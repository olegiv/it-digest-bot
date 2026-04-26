package claudecode

import (
	"context"
	"io"
	"log/slog"

	"github.com/olegiv/it-digest-bot/internal/releasewatch"
	"github.com/olegiv/it-digest-bot/internal/store"
	"github.com/olegiv/it-digest-bot/internal/telegram"
)

// Watcher runs the Claude Code release flow as a single-source release
// watcher pass. It is a thin convenience over releasewatch.Runner used by
// tests and any embedder that wants to invoke the Claude Code source on
// its own; production callers use releasewatch.Runner with multiple
// sources.
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
	source := &Source{
		Package:    w.Package,
		GitHubRepo: w.GitHubRepo,
		NPM:        w.NPM,
		GitHub:     w.GitHub,
		Logger:     w.Logger,
	}
	runner := &releasewatch.Runner{
		Sources:  []releasewatch.Source{source},
		Channel:  w.Channel,
		Bot:      w.Bot,
		Releases: w.Releases,
		Posts:    w.Posts,
		Logger:   w.Logger,
		DryRun:   w.DryRun,
		DryOut:   w.DryOut,
	}

	watchRes, err := runner.Run(ctx)
	return watcherResult(w.Package, watchRes), err
}

func watcherResult(pkg string, res *releasewatch.Result) *Result {
	out := &Result{}
	if res == nil {
		return out
	}
	for _, item := range res.Items {
		if item.Package != pkg {
			continue
		}
		out.LatestVersion = item.Version
		out.Posted = item.Posted
		out.MessageID = item.MessageID
		break
	}
	return out
}
