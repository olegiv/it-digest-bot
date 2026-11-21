package main

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/olegiv/it-digest-bot/internal/config"
	"github.com/olegiv/it-digest-bot/internal/httpx"
)

func fixtureServer(t *testing.T, name string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", "internal", "distrosec", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// TestDistroSecuritySources pins the wiring of the opt-in distro sources:
// each runs only with a package list, the release reaches the Ubuntu
// client, each URL override reaches its own client, and the logger is set.
func TestDistroSecuritySources(t *testing.T) {
	t.Parallel()
	usn := fixtureServer(t, "ubuntu-notices-noble.json")
	dsa := fixtureServer(t, "debian-dsa-long.rdf")
	ubuntu := func(release string) config.UbuntuSecurityConfig {
		return config.UbuntuSecurityConfig{Release: release, Packages: []string{"nginx"}, NoticesURL: usn}
	}
	debian := config.DebianSecurityConfig{Packages: []string{"nginx"}, FeedURL: dsa}

	for name, tc := range map[string]struct {
		cfg   config.Config
		want  string // name/package key of each source
		fetch bool   // run Candidates against the fixtures
	}{
		"no package lists":       {config.Config{UbuntuSecurity: config.UbuntuSecurityConfig{Release: "noble", NoticesURL: usn}, DebianSecurity: config.DebianSecurityConfig{FeedURL: dsa}}, "", false},
		"ubuntu only, release":   {config.Config{UbuntuSecurity: ubuntu("jammy")}, "ubuntu-security/usn-jammy", false},
		"debian only":            {config.Config{DebianSecurity: debian}, "debian-security/dsa", true},
		"both, urls not swapped": {config.Config{UbuntuSecurity: ubuntu("noble"), DebianSecurity: debian}, "ubuntu-security/usn-noble,debian-security/dsa", true},
	} {
		var logs strings.Builder
		log := slog.New(slog.NewTextHandler(&logs, nil))
		sources := distroSecuritySources(&tc.cfg, httpx.New(), log)
		var got []string
		for _, s := range sources {
			keyed, ok := s.(interface{ PackageKey() string })
			if !ok {
				t.Fatalf("%s: %T has no PackageKey", name, s)
			}
			got = append(got, s.Name()+"/"+keyed.PackageKey())
		}
		if strings.Join(got, ",") != tc.want {
			t.Errorf("%s: sources = %v, want %q", name, got, tc.want)
			continue
		}
		if !tc.fetch {
			continue
		}
		for _, s := range sources {
			// A swapped URL feeds the other distro's format to the parser
			// and fails; the summary line proves the logger is wired.
			if _, err := s.Candidates(context.Background()); err != nil {
				t.Errorf("%s: %s: %v", name, s.Name(), err)
			}
			if !strings.Contains(logs.String(), `msg="security advisories" source=`+s.Name()) {
				t.Errorf("%s: %s did not log through the given logger:\n%s", name, s.Name(), logs.String())
			}
		}
	}
}
