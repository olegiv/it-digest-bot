package gorelease

import (
	"context"
	"log/slog"

	"github.com/olegiv/it-digest-bot/internal/httpx"
	"github.com/olegiv/it-digest-bot/internal/releasewatch"
)

// Source turns official Go stable releases into generic releasewatch
// candidates.
type Source struct {
	Client *Client
	Logger *slog.Logger
}

// NewSource returns a Go release source using the supplied HTTP client.
func NewSource(h *httpx.Client) *Source {
	return &Source{Client: NewClient(h)}
}

func (s *Source) Name() string { return "go" }

func (s *Source) Candidates(ctx context.Context) ([]releasewatch.Candidate, error) {
	releases, err := s.client().FetchStable(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]releasewatch.Candidate, 0, len(releases))
	for _, rel := range releases {
		version := rel.Version
		out = append(out, releasewatch.Candidate{
			Source:  s.Name(),
			Package: PackageKey,
			Version: version,
			Render: func(ctx context.Context) (*releasewatch.Announcement, error) {
				return s.render(ctx, version)
			},
		})
	}
	s.logger().Info("go stable releases", "count", len(out))
	return out, nil
}

func (s *Source) render(ctx context.Context, version string) (*releasewatch.Announcement, error) {
	summary, err := s.client().FetchReleaseSummary(ctx, version)
	if err != nil {
		s.logger().Warn("go release history unavailable; using fallback summary",
			"version", version,
			"err", err)
		summary = FallbackSummary(version)
	}

	downloadURL := DownloadURL(version)
	historyURL := ReleaseHistoryURL(version)
	return &releasewatch.Announcement{
		Text:        FormatRelease(version, summary, downloadURL, historyURL),
		ReleaseURL:  historyURL,
		DryRunTitle: "Go " + version,
		Payload: map[string]any{
			"package":      PackageKey,
			"version":      version,
			"url":          historyURL,
			"download_url": downloadURL,
		},
	}, nil
}

func (s *Source) client() *Client {
	if s.Client != nil {
		return s.Client
	}
	s.Client = NewClient(nil)
	return s.Client
}

func (s *Source) logger() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.Default()
}
