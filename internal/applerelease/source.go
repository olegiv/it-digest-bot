package applerelease

import (
	"context"
	"log/slog"
	"sort"

	"github.com/olegiv/it-digest-bot/internal/httpx"
	"github.com/olegiv/it-digest-bot/internal/releasewatch"
)

// Source turns the stable releases of one platform into releasewatch
// candidates. Use one Source per platform: each implements
// releasewatch.Seeder under its own package key, and the feed lists a window
// of recent releases, so the first run records them as history and posts a
// single notice instead of one announcement each.
//
// Source is not safe for concurrent use: Candidates fills a per-run cache
// that SeedNotice reads, relying on releasewatch.Runner calling them in that
// order within one Run.
type Source struct {
	Platform Platform
	Client   *Client
	Logger   *slog.Logger

	// byVersion keeps the releases of the latest Candidates call so
	// SeedNotice can describe them without a second fetch.
	byVersion map[string]*Release
}

// NewSource returns a source for platform using the supplied HTTP client.
func NewSource(h *httpx.Client, platform Platform) *Source {
	return &Source{Platform: platform, Client: NewClient(h)}
}

var (
	_ releasewatch.Source = (*Source)(nil)
	_ releasewatch.Seeder = (*Source)(nil)
)

func (s *Source) Name() string { return s.Platform.PackageKey() }

// SeedPackage implements releasewatch.Seeder.
func (s *Source) SeedPackage() string { return s.Platform.PackageKey() }

// Candidates fetches the feed and returns one candidate per stable release of
// s.Platform, oldest first so that a batch is posted in release order. A
// release whose major version is below the newest major in the feed is
// flagged as an older-branch update.
func (s *Source) Candidates(ctx context.Context) ([]releasewatch.Candidate, error) {
	all, warnings, err := s.client().Fetch(ctx)
	if err != nil {
		return nil, err
	}
	for _, w := range warnings {
		s.logger().Warn("apple releases feed item skipped", "platform", s.Platform, "reason", w)
	}

	// Feed order is newest first, so the first occurrence of a version is
	// the latest build; later duplicates (re-issued builds) are dropped.
	seen := map[string]bool{}
	releases := make([]*Release, 0, len(all))
	latestMajor := 0
	for i := range all {
		r := &all[i]
		if r.Platform != s.Platform || seen[r.Version] {
			continue
		}
		seen[r.Version] = true
		releases = append(releases, r)
		latestMajor = max(latestMajor, r.Major())
	}

	sort.SliceStable(releases, func(i, j int) bool {
		if !releases[i].Published.Equal(releases[j].Published) {
			return releases[i].Published.Before(releases[j].Published)
		}
		return compareVersions(releases[i].Version, releases[j].Version) < 0
	})

	pkg := s.Platform.PackageKey()
	s.byVersion = make(map[string]*Release, len(releases))
	out := make([]releasewatch.Candidate, 0, len(releases))
	for _, r := range releases {
		s.byVersion[r.Version] = r
		legacy := r.Major() < latestMajor
		out = append(out, releasewatch.Candidate{
			Source:  s.Name(),
			Package: pkg,
			Version: r.Version,
			URL:     r.Link,
			Render: func(context.Context) (*releasewatch.Announcement, error) {
				return announcement(r, legacy), nil
			},
		})
	}
	if len(out) == 0 {
		s.logger().Warn("apple releases feed lists no stable release for platform; Apple may have changed the title format",
			"platform", s.Platform, "feed_items_matched", len(all))
	} else {
		s.logger().Info("apple stable releases", "platform", s.Platform, "count", len(out), "skipped", len(warnings))
	}
	return out, nil
}

// SeedNotice implements releasewatch.Seeder. With no candidates it seeds
// silently: nothing is recorded either, so Runner would otherwise repeat a
// "0 releases" notice on every run until the feed lists a stable release for
// the platform again.
func (s *Source) SeedNotice(_ context.Context, cands []releasewatch.Candidate) (*releasewatch.Announcement, error) {
	if len(cands) == 0 {
		return nil, nil
	}
	var newest *Release
	for _, cand := range cands {
		r := s.byVersion[cand.Version]
		if r == nil {
			continue
		}
		if newest == nil || compareVersions(r.Version, newest.Version) > 0 {
			newest = r
		}
	}
	payload := map[string]any{
		"package": s.Platform.PackageKey(),
		"count":   len(cands),
		"url":     ReleasesPageURL,
	}
	if newest != nil {
		payload["newest"] = newest.Version
	}
	return &releasewatch.Announcement{
		Text:        FormatSeedNotice(s.Platform, len(cands), newest),
		ReleaseURL:  ReleasesPageURL,
		DryRunTitle: string(s.Platform) + " releases baseline",
		Payload:     payload,
	}, nil
}

func announcement(r *Release, legacy bool) *releasewatch.Announcement {
	return &releasewatch.Announcement{
		Text:        FormatRelease(r, legacy),
		ReleaseURL:  r.Link,
		DryRunTitle: string(r.Platform) + " " + r.Version,
		Payload: map[string]any{
			"package":   r.Platform.PackageKey(),
			"version":   r.Version,
			"build":     r.Build,
			"url":       r.Link,
			"notes_url": r.NotesURL,
			"legacy":    legacy,
			"published": r.Published,
		},
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
