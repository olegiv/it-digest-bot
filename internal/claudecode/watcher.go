package claudecode

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

// Watcher orchestrates the full phase-1 flow: check npm → diff against
// the store → if a new version appeared, fetch release notes, post to
// Telegram, and record.
type Watcher struct {
	Package    string
	GitHubRepo string
	Channel    string

	NPM      *NPMClient
	GitHub   *GitHubClient
	Bot      *telegram.Bot
	Releases *store.Releases
	Posts    *store.Posts
	Logger   *slog.Logger

	// DryRun, when true, prints the rendered message to DryOut instead
	// of sending to Telegram, and skips the releases_seen + posts_log
	// writes so the run can be repeated.
	DryRun bool
	DryOut io.Writer // defaults to os.Stdout
}

// Result summarises what Run did.
type Result struct {
	LatestVersion string
	Posted        bool
	MessageID     int64
}

// Run executes one pass. On success, returns a Result describing what
// happened. On any error, a partial Result may be returned along with the
// error so callers can log more detail.
func (w *Watcher) Run(ctx context.Context) (*Result, error) {
	log := w.logger()

	info, err := w.NPM.FetchLatest(ctx, w.Package)
	if err != nil {
		return nil, fmt.Errorf("npm fetch: %w", err)
	}
	res := &Result{LatestVersion: info.LatestVersion}
	log.Info("npm latest", "package", w.Package, "version", info.LatestVersion)

	seen, err := w.Releases.HasSeen(ctx, w.Package, info.LatestVersion)
	if err != nil {
		return res, fmt.Errorf("store lookup: %w", err)
	}
	if seen {
		log.Info("no new release",
			"package", w.Package,
			"version", info.LatestVersion)
		return res, nil
	}

	// Cross-check against GitHub's "Latest release" marker before posting.
	// npm's dist-tags.latest can flip backward (yank, dist-tag retraction) —
	// requiring agreement with GitHub's deliberate maintainer toggle prevents
	// announcing a version that gets demoted minutes later.
	ghLatest, err := w.GitHub.FetchLatestRelease(ctx, w.GitHubRepo)
	if err != nil {
		if errors.Is(err, ErrReleaseNotFound) {
			log.Info("github has no published latest release; deferring",
				"package", w.Package,
				"npm", info.LatestVersion)
			return res, nil
		}
		return res, fmt.Errorf("github latest: %w", err)
	}
	log.Info("github latest", "repo", w.GitHubRepo, "version", ghLatest.Version)

	if ghLatest.Version != info.LatestVersion {
		log.Info("npm and github disagree on latest; deferring post until they agree",
			"package", w.Package,
			"npm", info.LatestVersion,
			"github", ghLatest.Version)
		return res, nil
	}

	msg := FormatRelease(
		info.LatestVersion,
		ghLatest.Body,
		ghLatest.ReleaseURL,
		NPMPackageURL(w.Package),
	)

	if w.DryRun {
		w.printDryRun(info.LatestVersion, msg, log)
		return res, nil
	}

	msgID, err := w.Bot.SendMessage(ctx, w.Channel, msg, telegram.ParseModeMarkdownV2)
	if err != nil {
		return res, fmt.Errorf("telegram send: %w", err)
	}
	res.Posted = true
	res.MessageID = msgID

	if err := w.Releases.RecordSeen(ctx, w.Package, info.LatestVersion, msgID, ghLatest.ReleaseURL); err != nil {
		return res, fmt.Errorf("record release: %w", err)
	}

	payload, _ := json.Marshal(map[string]any{
		"package": w.Package,
		"version": info.LatestVersion,
		"url":     ghLatest.ReleaseURL,
	})
	if _, err := w.Posts.Record(ctx, store.KindRelease, string(payload), msgID); err != nil {
		log.Warn("record posts_log failed", "err", err)
	}

	log.Info("posted release",
		"package", w.Package,
		"version", info.LatestVersion,
		"message_id", msgID)
	return res, nil
}

func (w *Watcher) printDryRun(version, msg string, log *slog.Logger) {
	out := w.DryOut
	if out == nil {
		out = os.Stdout
	}
	log.Info("dry-run: rendering release message to stdout; no Telegram send, no DB writes",
		"version", version, "bytes", len(msg))
	_, _ = fmt.Fprintf(out, "\n---- RELEASE %s — %d bytes ----\n%s\n---- END DRY-RUN ----\n",
		version, len(msg), msg)
}

func (w *Watcher) logger() *slog.Logger {
	if w.Logger != nil {
		return w.Logger
	}
	return slog.Default()
}
