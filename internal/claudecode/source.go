package claudecode

import (
	"context"
	"errors"
	"log/slog"

	"github.com/olegiv/it-digest-bot/internal/releasewatch"
)

// Source turns the Claude Code npm/GitHub release checks into generic
// releasewatch candidates.
type Source struct {
	Package    string
	GitHubRepo string

	NPM    *NPMClient
	GitHub *GitHubClient
	Logger *slog.Logger
}

func (s *Source) Name() string { return "claude-code" }

func (s *Source) Candidates(ctx context.Context) ([]releasewatch.Candidate, error) {
	log := s.logger()

	info, err := s.NPM.FetchLatest(ctx, s.Package)
	if err != nil {
		return nil, err
	}
	log.Info("npm latest", "package", s.Package, "version", info.LatestVersion)

	version := info.LatestVersion
	return []releasewatch.Candidate{{
		Source:  s.Name(),
		Package: s.Package,
		Version: version,
		Render: func(ctx context.Context) (*releasewatch.Announcement, error) {
			return s.render(ctx, version)
		},
	}}, nil
}

func (s *Source) render(ctx context.Context, version string) (*releasewatch.Announcement, error) {
	log := s.logger()

	// Cross-check against GitHub's "Latest release" marker before posting.
	// npm's dist-tags.latest can flip backward (yank, dist-tag retraction) —
	// requiring agreement with GitHub's deliberate maintainer toggle prevents
	// announcing a version that gets demoted minutes later.
	ghLatest, err := s.GitHub.FetchLatestRelease(ctx, s.GitHubRepo)
	if err != nil {
		if errors.Is(err, ErrReleaseNotFound) {
			return nil, releasewatch.Deferf("github has no published latest release")
		}
		return nil, err
	}
	log.Info("github latest", "repo", s.GitHubRepo, "version", ghLatest.Version)

	if ghLatest.Version != version {
		return nil, releasewatch.Deferf(
			"npm and github disagree on latest; npm=%s github=%s",
			version,
			ghLatest.Version,
		)
	}

	msg := FormatRelease(
		version,
		ghLatest.Body,
		ghLatest.ReleaseURL,
		NPMPackageURL(s.Package),
	)

	return &releasewatch.Announcement{
		Text:        msg,
		ReleaseURL:  ghLatest.ReleaseURL,
		DryRunTitle: version,
		Payload: map[string]any{
			"package": s.Package,
			"version": version,
			"url":     ghLatest.ReleaseURL,
		},
	}, nil
}

func (s *Source) logger() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.Default()
}
