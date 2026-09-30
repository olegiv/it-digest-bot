package drupalsec

import (
	"context"
	"log/slog"
	"sort"

	"github.com/olegiv/it-digest-bot/internal/httpx"
	"github.com/olegiv/it-digest-bot/internal/releasewatch"
)

// Source turns drupal.org security advisories into releasewatch candidates.
// It also implements releasewatch.Seeder: the feed lists a window of recent
// advisories (50 at the time of writing), so the first run records them as
// history and posts a single notice instead of one announcement each.
//
// Source is not safe for concurrent use: Candidates fills a per-run cache
// that SeedNotice reads, relying on releasewatch.Runner calling them in that
// order within one Run.
type Source struct {
	Client *Client
	Logger *slog.Logger

	// byVersion keeps the advisories of the latest Candidates call so
	// SeedNotice can describe them without a second fetch.
	byVersion map[string]*Advisory
}

// NewSource returns a Drupal security source using the supplied HTTP client.
func NewSource(h *httpx.Client) *Source {
	return &Source{Client: NewClient(h)}
}

var (
	_ releasewatch.Source = (*Source)(nil)
	_ releasewatch.Seeder = (*Source)(nil)
)

func (s *Source) Name() string { return "drupal-security" }

// SeedPackage implements releasewatch.Seeder.
func (s *Source) SeedPackage() string { return PackageKey }

// Candidates fetches the feed and returns one candidate per advisory, oldest
// first so that a batch of new advisories is posted in publication order.
func (s *Source) Candidates(ctx context.Context) ([]releasewatch.Candidate, error) {
	advisories, warnings, err := s.client().Fetch(ctx)
	if err != nil {
		return nil, err
	}
	for _, w := range warnings {
		s.logger().Warn("drupal security feed item skipped", "reason", w)
	}
	degraded := 0
	for i := range advisories {
		a := &advisories[i]
		if missing := a.MissingFields(); len(missing) > 0 {
			degraded++
			s.logger().Warn("drupal security advisory parsed with missing fields; drupal.org markup may have changed",
				"version", a.Version(),
				"link", a.Link,
				"missing", missing)
		}
	}

	sort.SliceStable(advisories, func(i, j int) bool {
		if !advisories[i].Published.Equal(advisories[j].Published) {
			return advisories[i].Published.Before(advisories[j].Published)
		}
		return advisories[i].Version() < advisories[j].Version()
	})

	s.byVersion = make(map[string]*Advisory, len(advisories))
	out := make([]releasewatch.Candidate, 0, len(advisories))
	for i := range advisories {
		a := &advisories[i]
		s.byVersion[a.Version()] = a
		out = append(out, releasewatch.Candidate{
			Source:  s.Name(),
			Package: PackageKey,
			Version: a.Version(),
			URL:     a.Link,
			Render: func(context.Context) (*releasewatch.Announcement, error) {
				return announcement(a), nil
			},
		})
	}
	s.logger().Info("drupal security advisories", "count", len(out), "skipped", len(warnings), "degraded", degraded)
	return out, nil
}

// SeedNotice implements releasewatch.Seeder.
func (s *Source) SeedNotice(_ context.Context, cands []releasewatch.Candidate) (*releasewatch.Announcement, error) {
	var newest *Advisory
	for _, cand := range cands {
		a := s.byVersion[cand.Version]
		if a == nil {
			continue
		}
		if newest == nil || a.Published.After(newest.Published) {
			newest = a
		}
	}
	payload := map[string]any{
		"package": PackageKey,
		"count":   len(cands),
		"url":     SecurityPageURL,
	}
	if newest != nil {
		payload["newest"] = newest.ID
	}
	return &releasewatch.Announcement{
		Text:        FormatSeedNotice(len(cands), newest),
		ReleaseURL:  SecurityPageURL,
		DryRunTitle: "Drupal security baseline",
		Payload:     payload,
	}, nil
}

func announcement(a *Advisory) *releasewatch.Announcement {
	title := "Drupal " + a.Version()
	return &releasewatch.Announcement{
		Text:        FormatAdvisory(a),
		ReleaseURL:  a.Link,
		DryRunTitle: title,
		Payload: map[string]any{
			"package":           PackageKey,
			"version":           a.Version(),
			"url":               a.Link,
			"kind":              string(a.Kind),
			"title":             a.Title,
			"project":           a.ProjectMachineName,
			"risk":              a.RiskLabel,
			"risk_score":        a.RiskScore,
			"vulnerability":     a.Vulnerability,
			"affected_versions": a.AffectedVersions,
			"cves":              a.CVEs,
			"published":         a.Published,
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
