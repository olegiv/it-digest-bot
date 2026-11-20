package phprelease

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/olegiv/it-digest-bot/internal/httpx"
)

func nopSleep(context.Context, time.Duration) error { return nil }

func testHTTP() *httpx.Client {
	return httpx.New(httpx.WithSleep(nopSleep), httpx.WithMaxRetries(0))
}

// activeJSON mirrors https://www.php.net/releases/active.php as rendered by
// php/web-php (include/branches.inc get_active_branches) on 2026-11-20.
// Source entries are shortened to one file each.
const activeJSON = `{"8":{
"8.2":{"announcement":true,"tags":["security"],"date":"19 Nov 2026","source":[{"filename":"php-8.2.36.tar.gz","name":"PHP 8.2.36 (tar.gz)","sha256":"c871dbdc","date":"19 Nov 2026"}],"version":"8.2.36"},
"8.3":{"announcement":true,"tags":["security"],"date":"19 Nov 2026","source":[{"filename":"php-8.3.37.tar.gz","name":"PHP 8.3.37 (tar.gz)","sha256":"cdd2ed86","date":"19 Nov 2026"}],"version":"8.3.37"},
"8.4":{"announcement":true,"tags":["security"],"date":"19 Nov 2026","source":[{"filename":"php-8.4.28.tar.gz","name":"PHP 8.4.28 (tar.gz)","sha256":"deffc88c","date":"19 Nov 2026"}],"version":"8.4.28"},
"8.5":{"announcement":true,"tags":["security"],"date":"19 Nov 2026","source":[{"filename":"php-8.5.13.tar.gz","name":"PHP 8.5.13 (tar.gz)","sha256":"3aeb20a7","date":"19 Nov 2026"}],"version":"8.5.13"},
"8.6":{"announcement":true,"tags":[],"date":"19 Nov 2026","source":[{"filename":"php-8.6.0.tar.gz","name":"PHP 8.6.0 (tar.gz)","sha256":"62337dcc","date":"19 Nov 2026"}],"version":"8.6.0"}
}}`

func TestParseActive(t *testing.T) {
	t.Parallel()
	rels, warnings, err := ParseActive([]byte(activeJSON))
	if err != nil {
		t.Fatalf("ParseActive: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v", warnings)
	}
	want := []string{"8.2.36", "8.3.37", "8.4.28", "8.5.13", "8.6.0"}
	if len(rels) != len(want) {
		t.Fatalf("releases = %d, want %d", len(rels), len(want))
	}
	for i, r := range rels {
		if r.Version != want[i] {
			t.Errorf("rels[%d] = %s, want %s (ascending order)", i, r.Version, want[i])
		}
		if r.Branch != want[i][:3] {
			t.Errorf("%s branch = %q", r.Version, r.Branch)
		}
		if !r.Announced {
			t.Errorf("%s: announcement not detected", r.Version)
		}
		if got := r.Date.Format("2006-01-02"); got != "2026-11-19" {
			t.Errorf("%s date = %s", r.Version, got)
		}
		if wantSec := r.Version != "8.6.0"; r.Security != wantSec {
			t.Errorf("%s security = %v, want %v", r.Version, r.Security, wantSec)
		}
	}
	if !rels[4].IsNewBranch() || rels[3].IsNewBranch() {
		t.Error("IsNewBranch should be true only for x.y.0")
	}
}

func TestParseActiveSkipsInvalidEntries(t *testing.T) {
	t.Parallel()
	body := `{"8":{
"8.4":{"version":"8.4.28","tags":["Security "],"date":"07 June 2024","announcement":{"English":"/releases/8_4_28.php"}},
"8.5":{"version":"8.5.13-dev","date":"x"},
"8.6":{"version":"8.5.99"},
"8.7":{"version":"<b>8.7.0</b>"}},
"9":{"8.3":{"version":"8.3.1"}}}`
	rels, warnings, err := ParseActive([]byte(body))
	if err != nil {
		t.Fatalf("ParseActive: %v", err)
	}
	if len(rels) != 1 || rels[0].Version != "8.4.28" {
		t.Fatalf("releases = %+v, want only 8.4.28", rels)
	}
	r := rels[0]
	if !r.Security || !r.Announced {
		t.Errorf("tag/announcement object not honoured: %+v", r)
	}
	if got := r.Date.Format("2006-01-02"); got != "2024-06-07" {
		t.Errorf("long month date = %s", got)
	}
	if len(warnings) != 4 {
		t.Errorf("warnings = %d (%v), want 4", len(warnings), warnings)
	}
	for _, w := range warnings {
		if strings.Contains(w, "<b>8.7.0</b>") && !strings.Contains(w, `"<b>8.7.0</b>"`) {
			t.Errorf("warning must quote untrusted input: %s", w)
		}
	}
}

func TestParseActiveAnnouncementVariants(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		raw  string
		want bool
	}{
		{`true`, true},
		{`false`, false},
		{`{}`, false},
		{`{"English":"/releases/8_5_1.php"}`, true},
		{`"yes"`, false},
		{`null`, false},
	} {
		body := `{"8":{"8.5":{"version":"8.5.1","announcement":` + tc.raw + `}}}`
		rels, _, err := ParseActive([]byte(body))
		if err != nil {
			t.Fatalf("%s: %v", tc.raw, err)
		}
		if rels[0].Announced != tc.want {
			t.Errorf("announcement %s = %v, want %v", tc.raw, rels[0].Announced, tc.want)
		}
	}
	// Missing field.
	rels, _, err := ParseActive([]byte(`{"8":{"8.5":{"version":"8.5.1"}}}`))
	if err != nil || rels[0].Announced || rels[0].AnnouncementURL() != "" {
		t.Errorf("missing announcement: %+v, %v", rels, err)
	}
}

func TestParseActiveErrors(t *testing.T) {
	t.Parallel()
	for _, body := range []string{``, `[]`, ` [] `, `{}`, `{"8":{}}`, `not json`, `[1,2]`, `{"8":{"8.5":{"version":"bad"}}}`} {
		if _, _, err := ParseActive([]byte(body)); err == nil {
			t.Errorf("ParseActive(%q): want error", body)
		}
	}
}

func TestReleaseURLs(t *testing.T) {
	t.Parallel()
	r := &Release{Branch: "8.5", Version: "8.5.13", Announced: true}
	if got, want := r.ChangeLogURL(), "https://www.php.net/ChangeLog-8.php#8.5.13"; got != want {
		t.Errorf("ChangeLogURL = %s, want %s", got, want)
	}
	if got, want := r.AnnouncementURL(), "https://www.php.net/releases/8_5_13.php"; got != want {
		t.Errorf("AnnouncementURL = %s, want %s", got, want)
	}
	if r.Major() != 8 || r.Minor() != 5 || r.Patch() != 13 {
		t.Errorf("parts = %d.%d.%d", r.Major(), r.Minor(), r.Patch())
	}
	if (&Release{Version: "x"}).Major() != 0 || (&Release{Version: "8"}).Patch() != 0 {
		t.Error("malformed version parts should be 0")
	}
}

func TestCompareVersions(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		a, b string
		want int
	}{
		{"8.10.0", "8.9.9", 1},
		{"8.4.1", "8.4.10", -1},
		{"8.4", "8.4.0", 0},
		{"9.0.0", "8.99.99", 1},
	} {
		if got := CompareVersions(tc.a, tc.b); got != tc.want {
			t.Errorf("CompareVersions(%s, %s) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestClientFetch(t *testing.T) {
	t.Parallel()
	var accept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		accept = r.Header.Get("Accept")
		_, _ = w.Write([]byte(activeJSON))
	}))
	t.Cleanup(srv.Close)

	c := NewClient(testHTTP()).SetActiveURL(srv.URL)
	if c.ActiveURL() != srv.URL {
		t.Fatalf("ActiveURL = %s", c.ActiveURL())
	}
	rels, _, err := c.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(rels) != 5 || accept != "application/json" {
		t.Errorf("releases = %d accept = %q", len(rels), accept)
	}
	if NewClient(nil).SetActiveURL("  ").ActiveURL() != DefaultActiveURL {
		t.Error("blank override must keep the default URL")
	}
}

func TestClientFetchErrors(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		handler http.HandlerFunc
		want    string
	}{
		{"status", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) }, "http 404"},
		{"server error", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) }, "giving up"},
		{"oversize", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(strings.Repeat(" ", maxBody+1)))
		}, "exceeds"},
		{"bad json", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("<html>")) }, "decode"},
	} {
		srv := httptest.NewServer(tc.handler)
		_, _, err := NewClient(testHTTP()).SetActiveURL(srv.URL).Fetch(context.Background())
		srv.Close()
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", tc.name, err, tc.want)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := NewClient(testHTTP()).SetActiveURL("http://127.0.0.1:1").Fetch(ctx); err == nil {
		t.Error("cancelled context: want error")
	}
	if _, _, err := NewClient(testHTTP()).SetActiveURL("http://[::1]:namedport").Fetch(context.Background()); err == nil {
		t.Error("invalid URL: want error")
	}
}

// TestParseActiveRejectsNonCanonicalVersions: leading zeros would create a
// second releases_seen key for the same release ("08.5.1" vs "8.5.1").
func TestParseActiveRejectsNonCanonicalVersions(t *testing.T) {
	t.Parallel()
	for _, v := range []string{"08.5.1", "8.05.1", "8.5.01", "8.5.00", "8.5.1.0", "8.5", "+8.5.1", "8.5.1 ", "８.5.1"} {
		body := `{"8":{"8.4":{"version":"8.4.1"},"8.5":{"version":"` + v + `"}}}`
		if strings.HasPrefix(v, "08") {
			body = `{"8":{"8.4":{"version":"8.4.1"}},"08":{"08.5":{"version":"` + v + `"}}}`
		}
		rels, warnings, err := ParseActive([]byte(body))
		if err != nil {
			t.Fatalf("%q: %v", v, err)
		}
		if strings.TrimSpace(v) == "8.5.1" {
			continue // surrounding whitespace is trimmed, not rejected
		}
		if len(rels) != 1 || rels[0].Version != "8.4.1" || len(warnings) != 1 {
			t.Errorf("%q: releases %+v warnings %v, want it skipped", v, rels, warnings)
		}
	}
	for _, tc := range []struct{ major, branch, version string }{
		{"8", "8.5", "8.5.0"},
		{"10", "10.0", "10.0.0"},
		{"8", "8.10", "8.10.100"},
	} {
		body := `{"` + tc.major + `":{"` + tc.branch + `":{"version":"` + tc.version + `"}}}`
		if rels, _, err := ParseActive([]byte(body)); err != nil || len(rels) != 1 {
			t.Errorf("%q must be accepted: %+v, %v", tc.version, rels, err)
		}
	}
}

func TestParseActiveRejectsTooManyEntries(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	sb.WriteString(`{"8":{`)
	for i := range MaxEntries + 1 {
		if i > 0 {
			sb.WriteString(",")
		}
		fmt.Fprintf(&sb, `"8.%d":{"version":"8.%d.1"}`, i, i)
	}
	sb.WriteString(`}}`)
	if _, _, err := ParseActive([]byte(sb.String())); err == nil || !strings.Contains(err.Error(), "more than the 32 accepted") {
		t.Errorf("err = %v, want entry-cap error", err)
	}

	// Exactly MaxEntries is still fine.
	at := strings.Replace(sb.String(), fmt.Sprintf(`,"8.%d":{"version":"8.%d.1"}`, MaxEntries, MaxEntries), "", 1)
	if rels, _, err := ParseActive([]byte(at)); err != nil || len(rels) != MaxEntries {
		t.Errorf("at cap: %d releases, %v", len(rels), err)
	}
}
