package distrosec

import (
	"context"
	"log/slog"
	"time"

	"github.com/olegiv/it-digest-bot/internal/releasewatch"
)

// Package keys in releases_seen; the advisory ID is the version.
const (
	UbuntuPackagePrefix = "usn-" // "usn-noble"
	DebianPackageKey    = "dsa"
)

// MaxAdvisoryAge drops advisories published longer ago than this. Hourly
// runs only need the newest ones; the bound keeps the first run, and a
// package newly added to the list, from posting weeks-old advisories.
const MaxAdvisoryAge = 7 * 24 * time.Hour

// fetcher is the part of UbuntuClient / DebianClient a Source needs.
type fetcher interface {
	Fetch(context.Context) ([]Notice, []string, error)
}

// Source turns advisories into releasewatch candidates: every one that
// touches a watched package, local issues included.
//
// It is deliberately not a releasewatch.Seeder. A filtered list is often
// empty, and a Seeder seeds on the first run that returns candidates, which
// would swallow the first real alert as a "now tracked" baseline. Instead
// the first run posts the relevant advisories of the last MaxAdvisoryAge,
// which is bounded and useful right after enabling the source.
type Source struct {
	name    string
	pkgKey  string
	client  fetcher
	watched map[string]bool
	Logger  *slog.Logger
	// Now returns the current time (tests); nil means time.Now.
	Now func() time.Time
}

var _ releasewatch.Source = (*Source)(nil)

// NewUbuntuSource watches Ubuntu Security Notices of client's release for
// the given source packages.
func NewUbuntuSource(client *UbuntuClient, packages []string) *Source {
	return &Source{
		name:    "ubuntu-security",
		pkgKey:  UbuntuPackagePrefix + client.Release(),
		client:  client,
		watched: watchSet(packages),
	}
}

// NewDebianSource watches Debian Security Advisories for the given source
// packages.
func NewDebianSource(client *DebianClient, packages []string) *Source {
	return &Source{
		name:    "debian-security",
		pkgKey:  DebianPackageKey,
		client:  client,
		watched: watchSet(packages),
	}
}

func (s *Source) Name() string { return s.name }

// PackageKey is the releases_seen package these advisories are stored under.
func (s *Source) PackageKey() string { return s.pkgKey }

// Candidates fetches the advisories and returns the relevant ones, oldest
// first.
func (s *Source) Candidates(ctx context.Context) ([]releasewatch.Candidate, error) {
	notices, warnings, err := s.client.Fetch(ctx)
	if err != nil {
		return nil, err
	}
	log := s.logger()
	for _, w := range warnings {
		log.Warn("security advisory skipped", "source", s.name, "reason", w)
	}

	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	cutoff := now().Add(-MaxAdvisoryAge)

	var oldest time.Time
	for i := range notices {
		if p := notices[i].Published; !p.IsZero() && (oldest.IsZero() || p.Before(oldest)) {
			oldest = p
		}
	}

	// Keep only advisories for watched packages, and analyze only those.
	watched := notices[:0:0]
	for _, n := range notices {
		if len(matchPackages(n.Packages, s.watched)) > 0 {
			n.Analyze()
			watched = append(watched, n)
		}
	}

	out := make([]releasewatch.Candidate, 0, len(watched))
	old := 0
	for i := range watched {
		n := &watched[i]
		if !n.Published.IsZero() && n.Published.Before(cutoff) {
			old++
			continue
		}
		matched := matchPackages(n.Packages, s.watched)
		n.Packages = matched
		n.Fixed = fixedFor(n.Fixed, matched)
		out = append(out, releasewatch.Candidate{
			Source:  s.name,
			Package: s.pkgKey,
			Version: n.ID,
			URL:     n.Link,
			Render: func(context.Context) (*releasewatch.Announcement, error) {
				return announcement(n, s.pkgKey), nil
			},
		})
	}
	log.Info("security advisories",
		"source", s.name,
		"fetched", len(notices),
		"watched", len(watched),
		"relevant", len(out),
		"oldest_fetched", oldest,
		"older_than_max_age", old,
		"skipped", len(warnings))
	return out, nil
}

// fixedFor keeps the "pkg version" entries of the matched packages.
func fixedFor(fixed, matched []string) []string {
	var out []string
	for _, f := range fixed {
		for _, m := range matched {
			if len(f) > len(m) && f[:len(m)] == m && f[len(m)] == ' ' {
				out = append(out, f)
				break
			}
		}
	}
	return out
}

func announcement(n *Notice, pkgKey string) *releasewatch.Announcement {
	payload := map[string]any{
		"package":  pkgKey,
		"version":  n.ID,
		"url":      n.Link,
		"distro":   n.Distro,
		"packages": n.Packages,
		"remote":   n.Remote,
		"impacts":  n.Impacts,
		"cves":     len(n.CVEs),
	}
	if !n.Published.IsZero() {
		payload["published"] = n.Published
	}
	return &releasewatch.Announcement{
		Text:        FormatNotice(n),
		ReleaseURL:  n.Link,
		DryRunTitle: n.Distro + " " + n.ID,
		Payload:     payload,
	}
}

func (s *Source) logger() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.Default()
}
