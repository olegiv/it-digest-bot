// Package releasewatch contains the shared release-announcement flow used by
// the hourly watch command.
package releasewatch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/olegiv/it-digest-bot/internal/store"
	"github.com/olegiv/it-digest-bot/internal/telegram"
)

// ErrDeferred marks a candidate that should not be posted yet, without
// failing the whole watcher run.
var ErrDeferred = errors.New("release deferred")

// DeferredError carries the concrete reason a source chose to defer posting.
type DeferredError struct {
	Reason string
}

func (e *DeferredError) Error() string {
	if e == nil || e.Reason == "" {
		return ErrDeferred.Error()
	}
	return e.Reason
}

func (e *DeferredError) Unwrap() error { return ErrDeferred }

// Deferf returns an error that Runner treats as a clean non-posting path.
func Deferf(format string, args ...any) error {
	return &DeferredError{Reason: fmt.Sprintf(format, args...)}
}

// Source fetches cheap release candidates. Expensive rendering and
// cross-checking belongs in Candidate.Render so Runner can skip already-seen
// versions before doing unnecessary upstream calls.
type Source interface {
	Name() string
	Candidates(context.Context) ([]Candidate, error)
}

// RenderFunc builds a Telegram-ready announcement for an unseen candidate.
type RenderFunc func(context.Context) (*Announcement, error)

// Candidate is a potential upstream release identified by a source.
type Candidate struct {
	Source  string
	Package string
	Version string
	Render  RenderFunc
}

// Announcement is the fully rendered output for an unseen candidate.
type Announcement struct {
	Text        string
	ReleaseURL  string
	Payload     map[string]any
	DryRunTitle string
}

// Sender is the telegram.Bot subset Runner needs.
type Sender interface {
	SendMessage(context.Context, string, string, telegram.ParseMode) (int64, error)
}

// Runner owns the common release workflow: fetch candidates, skip seen rows,
// render, dry-run or send, then record state.
type Runner struct {
	Sources []Source
	Channel string

	Bot      Sender
	Releases *store.Releases
	Posts    *store.Posts
	Logger   *slog.Logger

	DryRun bool
	DryOut io.Writer
}

// Result summarizes one Runner pass.
type Result struct {
	Items []ItemResult
}

// ItemResult records what happened to one candidate.
type ItemResult struct {
	Source    string
	Package   string
	Version   string
	Posted    bool
	Seen      bool
	Deferred  bool
	MessageID int64
}

// PostedCount returns the number of candidates posted in this run.
func (r *Result) PostedCount() int {
	if r == nil {
		return 0
	}
	n := 0
	for _, item := range r.Items {
		if item.Posted {
			n++
		}
	}
	return n
}

// Run executes one release-watcher pass.
func (r *Runner) Run(ctx context.Context) (*Result, error) {
	log := r.logger()
	res := &Result{}
	var errs []error

	for _, source := range r.Sources {
		if source == nil {
			continue
		}
		sourceName := source.Name()
		candidates, err := source.Candidates(ctx)
		if err != nil {
			errs = append(errs, fmt.Errorf("source %s candidates: %w", sourceName, err))
			continue
		}
		if len(candidates) == 0 {
			log.Info("no release candidates", "source", sourceName)
			continue
		}
		for _, cand := range candidates {
			if cand.Source == "" {
				cand.Source = sourceName
			}
			item, err := r.handleCandidate(ctx, cand, log)
			res.Items = append(res.Items, item)
			if err != nil {
				errs = append(errs, err)
				continue
			}
		}
	}

	return res, errors.Join(errs...)
}

func (r *Runner) handleCandidate(ctx context.Context, cand Candidate, log *slog.Logger) (ItemResult, error) {
	item := ItemResult{
		Source:  cand.Source,
		Package: cand.Package,
		Version: cand.Version,
	}
	if cand.Package == "" {
		return item, errors.New("release candidate package is required")
	}
	if cand.Version == "" {
		return item, fmt.Errorf("release candidate %s: version is required", cand.Package)
	}
	if cand.Render == nil {
		return item, fmt.Errorf("release candidate %s %s: render func is required", cand.Package, cand.Version)
	}

	seen, err := r.Releases.HasSeen(ctx, cand.Package, cand.Version)
	if err != nil {
		return item, fmt.Errorf("store lookup %s %s: %w", cand.Package, cand.Version, err)
	}
	if seen {
		item.Seen = true
		log.Info("no new release",
			"source", cand.Source,
			"package", cand.Package,
			"version", cand.Version)
		return item, nil
	}

	ann, err := cand.Render(ctx)
	if err != nil {
		if errors.Is(err, ErrDeferred) {
			item.Deferred = true
			log.Info("release deferred",
				"source", cand.Source,
				"package", cand.Package,
				"version", cand.Version,
				"reason", err.Error())
			return item, nil
		}
		return item, fmt.Errorf("render release %s %s: %w", cand.Package, cand.Version, err)
	}
	if ann == nil {
		return item, fmt.Errorf("render release %s %s: nil announcement", cand.Package, cand.Version)
	}
	if ann.Text == "" {
		return item, fmt.Errorf("render release %s %s: empty announcement", cand.Package, cand.Version)
	}

	if r.DryRun {
		r.printDryRun(cand, ann, log)
		return item, nil
	}

	msgID, err := r.Bot.SendMessage(ctx, r.Channel, ann.Text, telegram.ParseModeMarkdownV2)
	if err != nil {
		return item, fmt.Errorf("telegram send %s %s: %w", cand.Package, cand.Version, err)
	}
	item.Posted = true
	item.MessageID = msgID

	if err := r.Releases.RecordSeen(ctx, cand.Package, cand.Version, msgID, ann.ReleaseURL); err != nil {
		return item, fmt.Errorf("record release %s %s: %w", cand.Package, cand.Version, err)
	}

	payload, err := json.Marshal(r.payload(cand, ann))
	if err != nil {
		return item, fmt.Errorf("marshal release payload %s %s: %w", cand.Package, cand.Version, err)
	}
	if _, err := r.Posts.Record(ctx, store.KindRelease, string(payload), msgID); err != nil {
		log.Warn("record posts_log failed",
			"source", cand.Source,
			"package", cand.Package,
			"version", cand.Version,
			"err", err)
	}

	log.Info("posted release",
		"source", cand.Source,
		"package", cand.Package,
		"version", cand.Version,
		"message_id", msgID)
	return item, nil
}

func (r *Runner) payload(cand Candidate, ann *Announcement) map[string]any {
	if len(ann.Payload) > 0 {
		return ann.Payload
	}
	return map[string]any{
		"source":  cand.Source,
		"package": cand.Package,
		"version": cand.Version,
		"url":     ann.ReleaseURL,
	}
}

func (r *Runner) printDryRun(cand Candidate, ann *Announcement, log *slog.Logger) {
	out := r.DryOut
	if out == nil {
		out = os.Stdout
	}
	title := ann.DryRunTitle
	if title == "" {
		title = cand.Package + " " + cand.Version
	}
	log.Info("dry-run: rendering release message; no Telegram send, no DB writes",
		"source", cand.Source,
		"package", cand.Package,
		"version", cand.Version,
		"bytes", len(ann.Text))
	_, _ = fmt.Fprintf(out, "\n---- RELEASE %s - %d bytes ----\n%s\n---- END DRY-RUN ----\n",
		title, len(ann.Text), ann.Text)
}

func (r *Runner) logger() *slog.Logger {
	if r.Logger != nil {
		return r.Logger
	}
	return slog.Default()
}
