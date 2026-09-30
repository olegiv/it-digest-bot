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
	"math"
	"os"

	"github.com/olegiv/it-digest-bot/internal/store"
	"github.com/olegiv/it-digest-bot/internal/telegram"
)

// DefaultMaxPostsPerRun bounds how many announcements one Run may send when
// Runner.MaxPostsPerRun is zero. A source that suddenly lists many unseen
// items (a feed that was truncated on the previous run, a history that was
// never recorded) is throttled to this many posts; the rest stay unseen and
// are posted on later runs in the order the source returned them.
const DefaultMaxPostsPerRun = 10

// UnlimitedPosts disables the per-run cap when assigned to
// Runner.MaxPostsPerRun.
const UnlimitedPosts = -1

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
	Source  string // empty means the source's Name()
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
// Runner posts SeedNotice once and records every current candidate as seen
// in one transaction, instead of announcing dozens of historical items on
// the first run. Seeding ignores MaxPostsPerRun.
//
// Runner calls SeedNotice at most once per Run, immediately after
// Candidates, with the slice that call returned; every candidate must carry
// Package == SeedPackage(). SeedNotice may return (nil, nil) to seed
// silently; a non-nil Announcement must have Text.
//
// Failure semantics: if the notice send fails nothing is recorded and the
// next run seeds again. If the history write fails after the notice went
// out, nothing is recorded either and the next run repeats the notice, which
// is preferred over recording history with no notice or, worse, partial
// history that would turn the next run into a flood of individual posts.
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

	// MaxPostsPerRun caps the send attempts (or dry-run renders) per Run
	// across all sources. Zero means DefaultMaxPostsPerRun; a negative value
	// (UnlimitedPosts) disables the cap. A failed send still consumes a slot,
	// so a Telegram outage stops after this many attempts instead of trying
	// every unseen release. A Seeder's one-off notice is not counted. "Zero
	// posts" is not expressible; use DryRun for that.
	MaxPostsPerRun int
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
	Capped    bool // unseen, but left for a later run because MaxPostsPerRun was reached
	MessageID int64
}

// CappedCount returns the number of unseen candidates left for a later run
// because the per-run post limit was reached.
func (r *Result) CappedCount() int {
	if r == nil {
		return 0
	}
	n := 0
	for _, item := range r.Items {
		if item.Capped {
			n++
		}
	}
	return n
}

// postLimit resolves MaxPostsPerRun to an effective limit.
func (r *Runner) postLimit() int {
	switch {
	case r.MaxPostsPerRun < 0:
		return math.MaxInt
	case r.MaxPostsPerRun == 0:
		return DefaultMaxPostsPerRun
	default:
		return r.MaxPostsPerRun
	}
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
	limit := r.postLimit()
	attempts := 0

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
			item, attempted, err := r.handleCandidate(ctx, cand, attempts >= limit, log)
			res.Items = append(res.Items, item)
			if attempted {
				attempts++
			}
			if err != nil {
				errs = append(errs, err)
				continue
			}
		}
	}
	if n := res.CappedCount(); n > 0 {
		log.Warn("per-run send budget exhausted; remaining releases wait for the next run",
			"limit", limit,
			"attempted", attempts,
			"posted", res.PostedCount(),
			"capped", n)
	}

	return res, errors.Join(errs...)
}

// validateCandidate rejects candidates that cannot be posted or recorded.
// Both the per-candidate path and the seed path use it, so an empty version
// can never reach releases_seen (SQLite NOT NULL accepts "").
func validateCandidate(cand Candidate) error {
	if cand.Package == "" {
		return errors.New("release candidate package is required")
	}
	if cand.Version == "" {
		return fmt.Errorf("release candidate %s: version is required", cand.Package)
	}
	if cand.Render == nil {
		return fmt.Errorf("release candidate %s %s: render func is required", cand.Package, cand.Version)
	}
	return nil
}

// handleCandidate processes one candidate. attempted reports whether a send
// slot was consumed (a send was made, even if it failed, or would have been
// in dry-run) so Run can enforce MaxPostsPerRun.
func (r *Runner) handleCandidate(ctx context.Context, cand Candidate, limitReached bool, log *slog.Logger) (item ItemResult, attempted bool, err error) {
	item = ItemResult{
		Source:  cand.Source,
		Package: cand.Package,
		Version: cand.Version,
	}
	if err := validateCandidate(cand); err != nil {
		return item, false, err
	}

	seen, err := r.Releases.HasSeen(ctx, cand.Package, cand.Version)
	if err != nil {
		return item, false, fmt.Errorf("store lookup %s %s: %w", cand.Package, cand.Version, err)
	}
	if seen {
		item.Seen = true
		log.Info("no new release",
			"source", cand.Source,
			"package", cand.Package,
			"version", cand.Version)
		return item, false, nil
	}

	// Checked after the seen lookup so only genuinely new releases count,
	// and before Render so no upstream calls are spent on a capped item.
	if limitReached {
		item.Capped = true
		log.Info("release skipped: per-run send budget exhausted; will be posted on a later run",
			"source", cand.Source,
			"package", cand.Package,
			"version", cand.Version)
		return item, false, nil
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
			return item, false, nil
		}
		return item, false, fmt.Errorf("render release %s %s: %w", cand.Package, cand.Version, err)
	}
	if ann == nil {
		return item, false, fmt.Errorf("render release %s %s: nil announcement", cand.Package, cand.Version)
	}
	if ann.Text == "" {
		return item, false, fmt.Errorf("render release %s %s: empty announcement", cand.Package, cand.Version)
	}

	if r.DryRun {
		r.printDryRun(cand, ann, log)
		return item, true, nil
	}

	msgID, err := r.Bot.SendMessage(ctx, r.Channel, ann.Text, telegram.ParseModeMarkdownV2)
	if err != nil {
		return item, true, fmt.Errorf("telegram send %s %s: %w", cand.Package, cand.Version, err)
	}
	item.Posted = true
	item.MessageID = msgID

	if err := r.Releases.RecordSeen(ctx, cand.Package, cand.Version, msgID, ann.ReleaseURL); err != nil {
		return item, true, fmt.Errorf("record release %s %s: %w", cand.Package, cand.Version, err)
	}

	payload, err := json.Marshal(r.payload(cand, ann))
	if err != nil {
		return item, true, fmt.Errorf("marshal release payload %s %s: %w", cand.Package, cand.Version, err)
	}
	if _, err := r.Posts.Record(ctx, store.KindRelease, string(payload), msgID); err != nil {
		log.Error("record posts_log failed; release was posted and recorded but the audit row is missing",
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
	return item, true, nil
}

// seedIfFirstRun applies the Seeder contract. It returns true when the
// candidates were consumed as history (or would have been, in dry-run) and
// must not be handled individually.
func (r *Runner) seedIfFirstRun(ctx context.Context, seeder Seeder, sourceName string, cands []Candidate, res *Result, log *slog.Logger) (bool, error) {
	pkg := seeder.SeedPackage()
	if pkg == "" {
		return false, errors.New("seed package is required")
	}
	for _, cand := range cands {
		if err := validateCandidate(cand); err != nil {
			return false, err
		}
		if cand.Package != pkg {
			return false, fmt.Errorf("seed candidate %s %s: package does not match seed package %s", cand.Package, cand.Version, pkg)
		}
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
		return false, errors.New("render seed notice: empty announcement")
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

	// One transaction, after the notice: a partially recorded history would
	// make the next run skip seeding and announce every leftover item
	// individually. See Seeder for the failure semantics.
	rows := make([]store.SeenRelease, 0, len(cands))
	for _, cand := range cands {
		rows = append(rows, store.SeenRelease{Package: cand.Package, Version: cand.Version, ReleaseURL: cand.URL})
	}
	if err := r.Releases.RecordSeenBatch(ctx, rows, msgID); err != nil {
		if msgID != 0 {
			log.Error("seed notice was posted but history was not recorded; the notice will be repeated on the next run",
				"source", sourceName,
				"package", pkg,
				"message_id", msgID,
				"err", err)
			return false, fmt.Errorf("record seeded history %s (notice message_id=%d already posted): %w", pkg, msgID, err)
		}
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
		log.Error("record posts_log failed; history was recorded but the seed audit row is missing",
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
