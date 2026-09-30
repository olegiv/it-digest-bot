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
	// URL is optional. It is stored as release_url when a Seeder records
	// history without rendering; normal posts take the URL from Announcement.
	URL    string
	Render RenderFunc
}

// Seeder is an optional Source extension for upstreams whose listing is a
// window of history (an RSS feed with the last N items) rather than just the
// latest version. When releases_seen holds no row at all for SeedPackage(),
// Runner records every current candidate as seen and posts SeedNotice once,
// instead of announcing dozens of historical items on the first run.
// SeedNotice may return (nil, nil) to seed silently.
type Seeder interface {
	SeedPackage() string
	SeedNotice(ctx context.Context, cands []Candidate) (*Announcement, error)
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
	Seeded    bool // recorded as history on a source's first run, not posted
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

// SeededCount returns the number of candidates recorded as history by a
// Seeder source in this run.
func (r *Result) SeededCount() int {
	if r == nil {
		return 0
	}
	n := 0
	for _, item := range r.Items {
		if item.Seeded {
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
		for i := range candidates {
			if candidates[i].Source == "" {
				candidates[i].Source = sourceName
			}
		}

		if seeder, ok := source.(Seeder); ok {
			seeded, err := r.seedIfFirstRun(ctx, seeder, sourceName, candidates, res, log)
			if err != nil {
				errs = append(errs, fmt.Errorf("source %s seed: %w", sourceName, err))
				continue
			}
			if seeded {
				continue
			}
		}

		for _, cand := range candidates {
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

// seedIfFirstRun applies the Seeder contract. It returns true when the
// candidates were consumed as history (or would have been, in dry-run) and
// must not be handled individually.
func (r *Runner) seedIfFirstRun(ctx context.Context, seeder Seeder, sourceName string, cands []Candidate, res *Result, log *slog.Logger) (bool, error) {
	pkg := seeder.SeedPackage()
	if pkg == "" {
		return false, errors.New("seed package is required")
	}
	_, err := r.Releases.GetLatestSeen(ctx, pkg)
	if err == nil {
		return false, nil // history already recorded: normal per-candidate flow
	}
	if !errors.Is(err, store.ErrNotFound) {
		return false, fmt.Errorf("store lookup %s: %w", pkg, err)
	}

	ann, err := seeder.SeedNotice(ctx, cands)
	if err != nil {
		return false, fmt.Errorf("render seed notice: %w", err)
	}
	if ann != nil && ann.Text == "" {
		ann = nil
	}

	items := make([]ItemResult, 0, len(cands))
	for _, cand := range cands {
		items = append(items, ItemResult{Source: cand.Source, Package: cand.Package, Version: cand.Version, Seeded: true})
	}

	if r.DryRun {
		r.printDrySeed(sourceName, cands, ann, log)
		res.Items = append(res.Items, items...)
		return true, nil
	}

	var msgID int64
	if ann != nil {
		msgID, err = r.Bot.SendMessage(ctx, r.Channel, ann.Text, telegram.ParseModeMarkdownV2)
		if err != nil {
			return false, fmt.Errorf("telegram send seed notice: %w", err)
		}
	}

	// One transaction: a partially recorded history would make the next run
	// skip seeding and announce every leftover item individually.
	rows := make([]store.SeenRelease, 0, len(cands))
	for _, cand := range cands {
		rows = append(rows, store.SeenRelease{Package: cand.Package, Version: cand.Version, ReleaseURL: cand.URL})
	}
	if err := r.Releases.RecordSeenBatch(ctx, rows, msgID); err != nil {
		return false, fmt.Errorf("record seeded history %s: %w", pkg, err)
	}
	for i := range items {
		items[i].MessageID = msgID
	}
	res.Items = append(res.Items, items...)

	payload, err := json.Marshal(map[string]any{
		"source":            sourceName,
		"package":           pkg,
		"count":             len(cands),
		"notice_posted":     ann != nil,
		"notice_message_id": msgID,
	})
	if err != nil {
		return true, fmt.Errorf("marshal seed payload %s: %w", pkg, err)
	}
	if _, err := r.Posts.Record(ctx, store.KindSeed, string(payload), msgID); err != nil {
		log.Warn("record posts_log failed",
			"source", sourceName,
			"package", pkg,
			"kind", store.KindSeed,
			"err", err)
	}

	log.Info("seeded source history",
		"source", sourceName,
		"package", pkg,
		"count", len(cands),
		"notice_posted", ann != nil,
		"message_id", msgID)
	return true, nil
}

func (r *Runner) printDrySeed(sourceName string, cands []Candidate, ann *Announcement, log *slog.Logger) {
	out := r.DryOut
	if out == nil {
		out = os.Stdout
	}
	log.Info("dry-run: first run would seed source history; no Telegram send, no DB writes",
		"source", sourceName,
		"count", len(cands),
		"notice_posted", ann != nil)
	_, _ = fmt.Fprintf(out, "\n---- SEED %s - would record %d candidates as seen ----\n", sourceName, len(cands))
	for _, cand := range cands {
		_, _ = fmt.Fprintf(out, "  %s %s\n", cand.Package, cand.Version)
	}
	if ann != nil {
		_, _ = fmt.Fprintf(out, "---- SEED NOTICE - %d bytes ----\n%s\n", len(ann.Text), ann.Text)
	} else {
		_, _ = fmt.Fprint(out, "---- SEED NOTICE - none (silent seed) ----\n")
	}
	_, _ = fmt.Fprint(out, "---- END DRY-RUN ----\n")
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
