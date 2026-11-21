package phpsec

import (
	"context"
	"fmt"
	"log/slog"
	"sort"

	"github.com/olegiv/it-digest-bot/internal/httpx"
	"github.com/olegiv/it-digest-bot/internal/releasewatch"
)

// Source turns PHP security advisories into releasewatch candidates. It
// implements releasewatch.Seeder: the endpoint returns the advisory history,
// so the first run records it and posts a single notice instead of one
// announcement per historical advisory.
//
// Source is not safe for concurrent use: Candidates fills a per-run cache
// that SeedNotice reads, relying on releasewatch.Runner calling them in that
// order within one Run.
type Source struct {
	Client *Client
	Logger *slog.Logger

	byVersion map[string]*Advisory
}

// NewSource returns a PHP security advisory source. token is the optional
// GITHUB_TOKEN.
func NewSource(h *httpx.Client, token string) *Source {
	return &Source{Client: NewClient(h, token)}
}

var (
	_ releasewatch.Source = (*Source)(nil)
	_ releasewatch.Seeder = (*Source)(nil)
)

func (s *Source) Name() string { return PackageKey }

// SeedPackage implements releasewatch.Seeder.
func (s *Source) SeedPackage() string { return PackageKey }

// Candidates fetches the advisories and returns one candidate per advisory,
// oldest first so a batch is posted in publication order.
func (s *Source) Candidates(ctx context.Context) ([]releasewatch.Candidate, error) {
	advisories, warnings, err := s.client().Fetch(ctx)
	if err != nil {
		return nil, err
	}
	for _, w := range warnings {
		s.logger().Warn("php security advisory skipped", "reason", w)
	}
	// php-src has dozens of published advisories, so an empty list is an
	// upstream glitch, not a fact. Returning it would let the runner skip
	// this run, and the first non-empty run would then take a brand-new
	// advisory into the first-run baseline instead of posting it. Failing
	// keeps the baseline for a real list (and alerts via OnFailure).
	if len(advisories) == 0 {
		return nil, fmt.Errorf("php advisories: no published advisories returned; php/php-src always has some, so the endpoint or its response changed")
	}

	sort.SliceStable(advisories, func(i, j int) bool {
		if !advisories[i].Published.Equal(advisories[j].Published) {
			return advisories[i].Published.Before(advisories[j].Published)
		}
		return advisories[i].ID < advisories[j].ID
	})

	s.byVersion = make(map[string]*Advisory, len(advisories))
	out := make([]releasewatch.Candidate, 0, len(advisories))
	for i := range advisories {
		a := &advisories[i]
		s.byVersion[a.ID] = a
		out = append(out, releasewatch.Candidate{
			Source:  s.Name(),
			Package: PackageKey,
			Version: a.ID,
			URL:     a.Link,
			Render: func(context.Context) (*releasewatch.Announcement, error) {
				return announcement(a), nil
			},
		})
	}
	if len(advisories) == PerPage {
		s.logger().Warn("php security advisories filled a whole page; older ones are outside the window",
			"per_page", PerPage)
	}
	s.logger().Info("php security advisories", "count", len(out), "skipped", len(warnings))
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
		"url":     AdvisoriesPageURL,
	}
	if newest != nil {
		payload["newest"] = newest.ID
	}
	return &releasewatch.Announcement{
		Text:        FormatSeedNotice(len(cands), newest),
		ReleaseURL:  AdvisoriesPageURL,
		DryRunTitle: "PHP security baseline",
		Payload:     payload,
	}, nil
}

func announcement(a *Advisory) *releasewatch.Announcement {
	return &releasewatch.Announcement{
		Text:        FormatAdvisory(a),
		ReleaseURL:  a.Link,
		DryRunTitle: "PHP " + a.ID,
		Payload: map[string]any{
			"package":   PackageKey,
			"version":   a.ID,
			"url":       a.Link,
			"cve":       a.CVE,
			"summary":   a.Summary,
			"severity":  a.Severity,
			"score":     a.Score,
			"cwes":      a.CWEs,
			"published": a.Published,
		},
	}
}

func (s *Source) client() *Client {
	if s.Client == nil {
		s.Client = NewClient(nil, "")
	}
	return s.Client
}

func (s *Source) logger() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.Default()
}
