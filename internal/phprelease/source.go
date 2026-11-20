package phprelease

import (
	"context"
	"log/slog"

	"github.com/olegiv/it-digest-bot/internal/httpx"
	"github.com/olegiv/it-digest-bot/internal/releasewatch"
)

// Source turns the latest release of every active PHP branch into
// releasewatch candidates. It implements releasewatch.Seeder: on the first
// run the current branch tips are recorded as the baseline and one notice is
// posted instead of announcing releases that may be months old.
//
// Source is not safe for concurrent use: Candidates fills a per-run cache
// that SeedNotice reads, relying on releasewatch.Runner calling them in that
// order within one Run.
type Source struct {
	Client *Client
	Logger *slog.Logger

	byVersion map[string]*Release
}

// NewSource returns a PHP release source using the supplied HTTP client.
func NewSource(h *httpx.Client) *Source {
	return &Source{Client: NewClient(h)}
}

var (
	_ releasewatch.Source = (*Source)(nil)
	_ releasewatch.Seeder = (*Source)(nil)
)

func (s *Source) Name() string { return PackageKey }

// SeedPackage implements releasewatch.Seeder.
func (s *Source) SeedPackage() string { return PackageKey }

// Candidates returns one candidate per active branch, oldest branch first,
// so a coordinated security release across branches is posted in version
// order with the current branch last.
func (s *Source) Candidates(ctx context.Context) ([]releasewatch.Candidate, error) {
	releases, warnings, err := s.client().Fetch(ctx)
	for _, w := range warnings {
		s.logger().Warn("php releases entry skipped", "reason", w)
	}
	if err != nil {
		return nil, err
	}

	latestBranch := releases[len(releases)-1].Branch // sorted ascending
	s.byVersion = make(map[string]*Release, len(releases))
	out := make([]releasewatch.Candidate, 0, len(releases))
	for i := range releases {
		r := &releases[i]
		s.byVersion[r.Version] = r
		current := r.Branch == latestBranch
		out = append(out, releasewatch.Candidate{
			Source:  s.Name(),
			Package: PackageKey,
			Version: r.Version,
			URL:     r.ChangeLogURL(),
			Render: func(context.Context) (*releasewatch.Announcement, error) {
				return announcement(r, current), nil
			},
		})
	}
	s.logger().Info("php active branches", "count", len(out), "skipped", len(warnings))
	return out, nil
}

// SeedNotice implements releasewatch.Seeder.
func (s *Source) SeedNotice(_ context.Context, cands []releasewatch.Candidate) (*releasewatch.Announcement, error) {
	releases := make([]*Release, 0, len(cands))
	versions := make([]string, 0, len(cands))
	for _, cand := range cands {
		if r := s.byVersion[cand.Version]; r != nil {
			releases = append(releases, r)
			versions = append(versions, r.Version)
		}
	}
	return &releasewatch.Announcement{
		Text:        FormatSeedNotice(releases),
		ReleaseURL:  SupportedVersionsURL,
		DryRunTitle: "PHP releases baseline",
		Payload: map[string]any{
			"package":  PackageKey,
			"count":    len(cands),
			"versions": versions,
			"url":      SupportedVersionsURL,
		},
	}, nil
}

func announcement(r *Release, current bool) *releasewatch.Announcement {
	payload := map[string]any{
		"package":   PackageKey,
		"version":   r.Version,
		"branch":    r.Branch,
		"security":  r.Security,
		"current":   current,
		"url":       r.ChangeLogURL(),
		"downloads": DownloadsURL,
	}
	if u := r.AnnouncementURL(); u != "" {
		payload["announcement"] = u
	}
	if !r.Date.IsZero() {
		payload["date"] = r.Date.Format("2006-01-02")
	}
	return &releasewatch.Announcement{
		Text:        FormatRelease(r, current),
		ReleaseURL:  r.ChangeLogURL(),
		DryRunTitle: "PHP " + r.Version,
		Payload:     payload,
	}
}

func (s *Source) client() *Client {
	if s.Client == nil {
		s.Client = NewClient(nil)
	}
	return s.Client
}

func (s *Source) logger() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.Default()
}
