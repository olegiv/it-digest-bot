package distrosec

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode"

	"github.com/olegiv/it-digest-bot/internal/httpx"
	"github.com/olegiv/it-digest-bot/internal/releasewatch"
	"github.com/olegiv/it-digest-bot/internal/store"
	"github.com/olegiv/it-digest-bot/internal/telegram"
)

func nopSleep(context.Context, time.Duration) error { return nil }

func testHTTP() *httpx.Client {
	return httpx.New(httpx.WithSleep(nopSleep), httpx.WithMaxRetries(0))
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return b
}

func serve(t *testing.T, body []byte, seen *[]string) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if seen != nil {
			mu.Lock()
			*seen = append(*seen, r.URL.RawQuery)
			mu.Unlock()
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestAssess(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		text    string
		remote  bool
		impacts string
	}{
		{"A remote attacker could possibly use this issue to cause nginx\nto crash, resulting in a denial of service.", true, "denial of service"},
		{"which could result in remote code\nexecution, denial of service, information disclosure or bypassing\nverification of client certificates.", true, "code execution,authentication bypass,information disclosure,denial of service"},
		{"A local user can take advantage of this flaw for local privilege escalation to root.", false, "privilege escalation"},
		{"A local attacker could use this. A remote attacker could also crash it.", true, "denial of service"},
		{"Several security issues were discovered in the Linux kernel. An attacker could possibly use these to compromise the system. Subsystems: Network drivers; TLS protocol;", false, ""},
		{"If a user or automated system were tricked into opening a specially crafted file, an attacker could execute arbitrary code.", true, "code execution"},
		{"could result in HTTP request smuggling or cross-site scripting", true, "cross-site scripting,HTTP request smuggling"},
		{"An attacker could inject arbitrary web script (XSS).", true, "cross-site scripting"},
	} {
		a := Assess(tc.text)
		if a.Remote != tc.remote || strings.Join(a.Impacts, ",") != tc.impacts {
			t.Errorf("Assess(%.60q) = remote %v impacts %q; want %v %q",
				tc.text, a.Remote, strings.Join(a.Impacts, ","), tc.remote, tc.impacts)
		}
	}
	// A summary may add remote cues and impacts the description lacks.
	if a := Assess("A local user could crash it.", "GStreamer could be made to crash if it received specially crafted input."); !a.Remote {
		t.Errorf("remote cue in summary: %+v", a)
	}
	if a := Assess("A local attacker could possibly use these issues to escalate privileges.", "needrestart vulnerability", "needrestart could be made to run programs as an administrator."); a.Remote ||
		strings.Join(a.Impacts, ",") != "code execution,privilege escalation" {
		t.Errorf("needrestart: %+v", a)
	}
}

// ubuntuFixture holds six real noble notices (trimmed): nginx x2 (remote),
// PyJWT, generic and FIPS kernels, and needrestart (local issue).
func TestParseUbuntuNotices(t *testing.T) {
	t.Parallel()
	notices, warnings, total, err := ParseUbuntuNotices(fixture(t, "ubuntu-notices-noble.json"), "noble")
	if err != nil || len(warnings) != 0 || total != 1701 {
		t.Fatalf("ParseUbuntuNotices: %v warnings=%v total=%d", err, warnings, total)
	}
	if len(notices) != 6 {
		t.Fatalf("notices = %d, want 6", len(notices))
	}
	byID := map[string]Notice{}
	for _, n := range notices {
		n.Analyze()
		byID[n.ID] = n
		if n.Distro != "Ubuntu 24.04 LTS" || n.Link != UbuntuNoticePageURL+n.ID || n.Published.IsZero() {
			t.Errorf("%s: distro %q link %q published %v", n.ID, n.Distro, n.Link, n.Published)
		}
	}
	nginx := byID["USN-9182-1"]
	if !nginx.Remote || len(nginx.CVEs) != 3 ||
		strings.Join(nginx.Fixed, ",") != "nginx 1.24.0-2ubuntu7.21" || strings.Join(nginx.Packages, ",") != "nginx" {
		t.Errorf("nginx notice = %+v", nginx)
	}
	if !byID["USN-9183-1"].ProOnly || byID["USN-9173-1"].ProOnly {
		t.Error("FIPS kernel fix is Pro-only; the generic kernel is not")
	}
	if strings.Join(byID["USN-9173-1"].Packages, ",") != "linux,linux-aws,linux-aws-fips" {
		t.Errorf("kernel source packages = %v", byID["USN-9173-1"].Packages)
	}

	// Another release: none of these notices list jammy packages here.
	other, _, _, err := ParseUbuntuNotices(fixture(t, "ubuntu-notices-noble.json"), "jammy")
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range other {
		if n.Distro == "Ubuntu 24.04 LTS" {
			t.Errorf("jammy parse labelled %s as noble", n.ID)
		}
	}
}

func TestParseUbuntuNoticesRejectsBadInput(t *testing.T) {
	t.Parallel()
	if _, _, _, err := ParseUbuntuNotices([]byte("<html>"), "noble"); err == nil {
		t.Error("want decode error")
	}
	body := `{"notices":[
	 {"id":"USN-1-1\u202e","title":"x","release_packages":{"noble":[]}},
	 {"id":"USN-2-1","is_hidden":"True","release_packages":{"noble":[]}},
	 {"id":"USN-3-1","title":"t\u001b[2J","description":"A remote attacker [x](https://evil) could.","published":"bogus",
	  "cves_ids":["CVE-2026-1234","bad","CVE-2026-1234"],
	  "release_packages":{"noble":[{"name":"Bad Name","is_source":true},{"name":"ok","version":"1.0;rm -rf","is_source":true}]}}],
	 "total_results":3}`
	notices, warnings, _, err := ParseUbuntuNotices([]byte(body), "noble")
	if err != nil || len(warnings) != 1 || len(notices) != 1 {
		t.Fatalf("notices %+v warnings %v err %v", notices, warnings, err)
	}
	n := notices[0]
	if n.Title != "t [2J" || len(n.CVEs) != 1 || strings.Join(n.Packages, ",") != "ok" || len(n.Fixed) != 0 || !n.Published.IsZero() {
		t.Errorf("sanitized notice = %+v", n)
	}
}

func TestUbuntuClientPagesAndQuery(t *testing.T) {
	t.Parallel()
	var queries []string
	srv := serve(t, fixture(t, "ubuntu-notices-noble.json"), &queries)
	c := NewUbuntuClient(testHTTP(), "").SetURL(srv.URL)
	if c.Release() != DefaultUbuntuRelease || c.URL() != srv.URL {
		t.Fatalf("client = %s %s", c.Release(), c.URL())
	}
	notices, _, err := c.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(queries) != UbuntuPages {
		t.Errorf("requests = %d, want %d", len(queries), UbuntuPages)
	}
	if queries[0] != "limit=20&offset=0&release=noble" || queries[1] != "limit=20&offset=20&release=noble" {
		t.Errorf("queries = %v", queries)
	}
	if len(notices) != 6 {
		t.Errorf("duplicates across pages must collapse: %d", len(notices))
	}
	for i := 1; i < len(notices); i++ {
		if notices[i].Published.Before(notices[i-1].Published) {
			t.Fatal("notices must be oldest first")
		}
	}
	if NewUbuntuClient(nil, "jammy").SetURL(" ").URL() != DefaultUbuntuNoticesURL {
		t.Error("blank override must keep the default")
	}
}

func TestUbuntuClientStopsAtTotal(t *testing.T) {
	t.Parallel()
	var queries []string
	srv := serve(t, []byte(`{"notices":[],"total_results":5}`), &queries)
	if _, _, err := NewUbuntuClient(testHTTP(), "noble").SetURL(srv.URL).Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(queries) != 1 {
		t.Errorf("requests = %d, want 1 when the first page holds every result", len(queries))
	}
}

func TestClientErrors(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		handler http.HandlerFunc
		want    string
	}{
		{"status", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) }, "http 403"},
		{"bad body", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("<html>")) }, ""},
	} {
		srv := httptest.NewServer(tc.handler)
		_, _, uerr := NewUbuntuClient(testHTTP(), "noble").SetURL(srv.URL).Fetch(context.Background())
		_, _, derr := NewDebianClient(testHTTP()).SetURL(srv.URL).Fetch(context.Background())
		srv.Close()
		for _, err := range []error{uerr, derr} {
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("%s: err = %v, want %q", tc.name, err, tc.want)
			}
		}
	}
	big := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat(" ", maxDebianBody+1)))
	}))
	defer big.Close()
	if _, _, err := NewDebianClient(testHTTP()).SetURL(big.URL).Fetch(context.Background()); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Errorf("oversize debian feed: %v", err)
	}
}

func TestParseDebianFeed(t *testing.T) {
	t.Parallel()
	notices, warnings, err := ParseDebianFeed(fixture(t, "debian-dsa-long.rdf"))
	if err != nil || len(warnings) != 0 || len(notices) != 30 {
		t.Fatalf("ParseDebianFeed: %d notices, %v, %v", len(notices), warnings, err)
	}
	byID := map[string]Notice{}
	for _, n := range notices {
		n.Analyze()
		byID[n.ID] = n
	}
	nginx := byID["DSA-6642-1"]
	if nginx.Title != "nginx - security update" || nginx.Link != DebianTrackerURL+"DSA-6642-1" || !nginx.Remote ||
		nginx.Published.Format("2006-01-02") != "2026-11-19" || strings.Join(nginx.Packages, ",") != "nginx" {
		t.Errorf("nginx DSA = %+v", nginx)
	}
	if strings.Join(nginx.Impacts, ",") != "code execution,authentication bypass,information disclosure,denial of service" {
		t.Errorf("nginx impacts = %v", nginx.Impacts)
	}
	for _, p := range nginx.Paragraphs {
		if strings.Contains(p, "security-tracker") {
			t.Errorf("tracker link text must be dropped: %q", p)
		}
	}
	if byID["DSA-6637-1"].Regression || !byID["DSA-6637-2"].Regression {
		t.Error("needrestart: -1 is a security update, -2 is a regression fix")
	}
	if !notices[0].Published.Before(notices[len(notices)-1].Published) {
		t.Error("advisories must be oldest first")
	}
}

func TestParseDebianFeedErrors(t *testing.T) {
	t.Parallel()
	empty := `<?xml version="1.0"?><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#" xmlns="http://purl.org/rss/1.0/"><channel><title>x</title></channel>
<item><title>Not a DSA</title><description>x</description></item></rdf:RDF>`
	_, warnings, err := ParseDebianFeed([]byte(empty))
	if err == nil || len(warnings) != 1 {
		t.Errorf("feed without advisories: err %v warnings %v", err, warnings)
	}
}

func TestSourceFiltersWatched(t *testing.T) {
	t.Parallel()
	srv := serve(t, fixture(t, "ubuntu-notices-noble.json"), nil)
	src := NewUbuntuSource(NewUbuntuClient(testHTTP(), "noble").SetURL(srv.URL), []string{"nginx", "linux", "needrestart", " "})
	src.Now = fixtureNow
	if src.Name() != "ubuntu-security" || src.PackageKey() != "usn-noble" {
		t.Fatalf("source = %s %s", src.Name(), src.PackageKey())
	}
	cands, err := src.Candidates(context.Background())
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	var ids []string
	for _, c := range cands {
		ids = append(ids, c.Version)
		if c.Package != "usn-noble" || c.Source != "ubuntu-security" {
			t.Errorf("candidate = %+v", c)
		}
	}
	// Local issues are posted too (needrestart USN-9151-1); PyJWT and
	// linux-fips are not watched.
	if strings.Join(ids, ",") != "USN-9151-1,USN-9159-1,USN-9173-1,USN-9182-1" {
		t.Fatalf("candidates = %v", ids)
	}
	ann, err := cands[2].Render(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ann.Text, "Packages: `linux`\n") || !strings.Contains(ann.Text, "`linux 6.8.0-151.151`") ||
		strings.Contains(ann.Text, "linux-aws") {
		t.Errorf("kernel post must show only the watched package:\n%s", ann.Text)
	}
}

func TestDebianSource(t *testing.T) {
	t.Parallel()
	srv := serve(t, fixture(t, "debian-dsa-long.rdf"), nil)
	src := NewDebianSource(NewDebianClient(testHTTP()).SetURL(srv.URL), []string{"nginx", "php8.4", "apache2", "needrestart", "postgresql-17"})
	src.Now = debianNow
	cands, err := src.Candidates(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, c := range cands {
		ids = append(ids, c.Version)
	}
	// Local issues are posted too: needrestart DSA-6637-1 and its
	// regression follow-up DSA-6637-2.
	if strings.Join(ids, ",") != "DSA-6620-1,DSA-6625-1,DSA-6637-1,DSA-6637-2,DSA-6641-1,DSA-6642-1" {
		t.Errorf("candidates = %v", ids)
	}
}

type recordingSender struct {
	mu       sync.Mutex
	messages []string
}

func (s *recordingSender) SendMessage(_ context.Context, _, text string, _ telegram.ParseMode) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = append(s.messages, text)
	return int64(len(s.messages)), nil
}

// TestSourcesThroughRunner: both sources through the real Runner and
// store. Relevant advisories are posted on the first run (no baseline: a
// filtered source must never swallow a real alert as one), then never again.
func TestSourcesThroughRunner(t *testing.T) {
	t.Parallel()
	usn := serve(t, fixture(t, "ubuntu-notices-noble.json"), nil)
	dsa := serve(t, fixture(t, "debian-dsa-long.rdf"), nil)
	ctx := context.Background()
	st, err := store.Open(ctx, "file:"+filepath.Join(t.TempDir(), "sec.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	bot := &recordingSender{}
	ubuntu := NewUbuntuSource(NewUbuntuClient(testHTTP(), "noble").SetURL(usn.URL), []string{"nginx"})
	debian := NewDebianSource(NewDebianClient(testHTTP()).SetURL(dsa.URL), []string{"nginx", "apache2"})
	ubuntu.Now, debian.Now = fixtureNow, debianNow
	r := &releasewatch.Runner{
		Sources: []releasewatch.Source{ubuntu, debian},
		Channel: "@ch", Bot: bot, Releases: st.Releases, Posts: st.Posts,
	}
	res, err := r.Run(ctx)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.PostedCount() != 4 || len(bot.messages) != 4 {
		t.Fatalf("first run posted %d (%d messages), want 4", res.PostedCount(), len(bot.messages))
	}
	for _, m := range bot.messages {
		if !telegram.WellFormedMarkdownV2(strings.ReplaceAll(m, "*", "")) {
			t.Errorf("post is not well formed:\n%s", m)
		}
	}
	if seen, _ := st.Releases.HasSeen(ctx, "usn-noble", "USN-9182-1"); !seen {
		t.Error("USN not recorded under usn-noble")
	}
	if seen, _ := st.Releases.HasSeen(ctx, "dsa", "DSA-6620-1"); !seen {
		t.Error("DSA not recorded under dsa")
	}
	res, err = r.Run(ctx)
	if err != nil || res.PostedCount() != 0 || len(bot.messages) != 4 {
		t.Errorf("second run posted %d, err %v", res.PostedCount(), err)
	}
}

func TestFormatNotice(t *testing.T) {
	t.Parallel()
	notices, _, _, _ := ParseUbuntuNotices(fixture(t, "ubuntu-notices-noble.json"), "noble")
	var nginx Notice
	for _, n := range notices {
		if n.ID == "USN-9159-1" {
			nginx = n
		}
	}
	nginx.Analyze()
	got := FormatNotice(&nginx)
	for _, want := range []string{
		"🔴 *Ubuntu 24\\.04 LTS security notice* — USN\\-9159\\-1\n",
		"*nginx vulnerabilities*\n",
		"Packages: `nginx`\n",
		"🌐 *Remotely exploitable* per the advisory text\n",
		"Impact: code execution, denial of service\n",
		"🛠 Fixed in: `nginx 1.24.0-2ubuntu7.20`\n",
		"Update: `sudo apt update && sudo apt upgrade`\n",
		"🔗 [USN\\-9159\\-1](https://ubuntu.com/security/notices/USN-9159-1)",
		"\\#Ubuntu \\#Security",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("post missing %q:\n%s", want, got)
		}
	}
	if !strings.Contains(got, "CVE: CVE") || strings.Contains(got, "more") {
		t.Errorf("all 8 CVEs fit without a +N more tail:\n%s", got)
	}
}

func TestFormatNoticeEscapesAndBounds(t *testing.T) {
	t.Parallel()
	huge := strings.Repeat("_*[]()~`>#+-=|{}.!\\ ", 600)
	n := &Notice{
		ID:         "USN-1-1",
		Distro:     "Ubuntu 24.04 LTS",
		Title:      huge,
		Packages:   []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"},
		Fixed:      []string{huge, huge, huge, huge, huge, huge, huge, huge},
		Paragraphs: []string{huge, huge, "[click](https://evil.example) `x`"},
		Link:       UbuntuNoticePageURL + "USN-1-1",
		ProOnly:    true,
		Assessment: Assessment{Remote: true, Impacts: []string{"code execution"}},
	}
	for range 400 {
		n.CVEs = append(n.CVEs, "CVE-2026-1234567")
	}
	got := FormatNotice(n)
	if len(got) > telegram.MaxMessageBytes {
		t.Fatalf("post is %d bytes", len(got))
	}
	if strings.Contains(got, "[click](") || !strings.Contains(got, `\+2 more`) || !strings.Contains(got, `\(\+392 more\)`) {
		t.Errorf("escaping/list caps:\n%s", got)
	}
	for _, r := range got {
		if r < 0x20 && r != '\n' || unicode.Is(unicode.Cf, r) {
			t.Fatalf("control rune %U in post", r)
		}
	}
	reg := FormatNotice(&Notice{ID: "DSA-1-2", Distro: DebianDistro, Regression: true, Link: DebianTrackerURL + "DSA-1-2"})
	if !strings.HasPrefix(reg, "♻️ *Debian security regression fix*") || !strings.Contains(reg, "`apt update && apt upgrade`") {
		t.Errorf("regression post:\n%s", reg)
	}
}

func TestFixedFor(t *testing.T) {
	t.Parallel()
	got := fixedFor([]string{"linux 1", "linux-aws 2", "nginx 3"}, []string{"linux", "nginx"})
	if strings.Join(got, ",") != "linux 1,nginx 3" {
		t.Errorf("fixedFor = %v", got)
	}
}

func TestSourceFetchErrorAndLogger(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()
	src := NewDebianSource(NewDebianClient(testHTTP()).SetURL(srv.URL), []string{"nginx"})
	if _, err := src.Candidates(context.Background()); err == nil {
		t.Error("want error")
	}
	if src.logger() == nil {
		t.Error("nil logger must fall back to the default")
	}
}

// fixtureNow is just after the newest Ubuntu fixture notice (2026-11-20).
func fixtureNow() time.Time { return time.Date(2026, 11, 21, 0, 0, 0, 0, time.UTC) }

// debianNow keeps every Debian fixture advisory from 2026-11-12 on within
// MaxAdvisoryAge.
func debianNow() time.Time { return time.Date(2026, 11, 19, 0, 0, 0, 0, time.UTC) }

func TestSourceDropsOldAdvisories(t *testing.T) {
	t.Parallel()
	srv := serve(t, fixture(t, "debian-dsa-long.rdf"), nil)
	src := NewDebianSource(NewDebianClient(testHTTP()).SetURL(srv.URL), []string{"nginx", "apache2", "linux"})
	// Cutoff 2026-11-13: DSA-6616-1 (linux, 11-08) and DSA-6620-1 (apache2,
	// 11-12) are too old; linux DSA-6628-1 (11-13) and nginx stay.
	src.Now = func() time.Time { return time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC) }
	cands, err := src.Candidates(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, c := range cands {
		ids = append(ids, c.Version)
	}
	if strings.Join(ids, ",") != "DSA-6628-1,DSA-6642-1" {
		t.Errorf("candidates = %v", ids)
	}
}

// TestAssessKernelSubsystemList covers DST-001: kernel notices list
// affected subsystems, which must not count as remote cues.
func TestAssessKernelSubsystemList(t *testing.T) {
	t.Parallel()
	// Real USN-9183-1 text. "Remote Processor subsystem" is a subsystem
	// list item, not a remote attacker.
	kernel := "Several security issues were discovered in the Linux kernel.\nAn attacker could possibly use these to compromise the system.\n" +
		"This update corrects flaws in the following subsystems:\n  - USB Gadget drivers;\n  - Voltage and Current Regulator drivers;\n" +
		"  - Remote Processor subsystem;\n  - RPMSG subsystem;\n  - Real Time Clock drivers;"
	if a := Assess(kernel, "Linux kernel (FIPS) vulnerabilities", "Several security issues were fixed in the Linux kernel."); a.Remote {
		t.Errorf("kernel: %+v; want not remote", a)
	}
	// A list item that does describe an issue still counts.
	if a := Assess("Fixes:\n  - A remote attacker could crash the server;"); !a.Remote {
		t.Errorf("issue list item: %+v; want remote", a)
	}
}

func TestProPocket(t *testing.T) {
	t.Parallel()
	for pocket, want := range map[string]bool{
		"esm-infra": true, "esm-apps": true, "fips-updates": true, "realtime": true,
		"security": false, "updates": false, "": false,
	} {
		if got := proPocket(pocket); got != want {
			t.Errorf("proPocket(%q) = %v, want %v", pocket, got, want)
		}
	}
}

// TestUbuntuClientWarnsWithoutTotal covers DST-005.
func TestUbuntuClientWarnsWithoutTotal(t *testing.T) {
	t.Parallel()
	var queries []string
	body := []byte(`{"notices":[{"id":"USN-1-1","title":"x","published":"2026-11-20T00:00:00","release_packages":{"noble":[{"name":"nginx","is_source":true}]}}]}`)
	srv := serve(t, body, &queries)
	notices, warnings, err := NewUbuntuClient(testHTTP(), "noble").SetURL(srv.URL).Fetch(context.Background())
	if err != nil || len(notices) != 1 || len(queries) != 1 {
		t.Fatalf("notices %d queries %d err %v", len(notices), len(queries), err)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "no total_results") {
		t.Errorf("warnings = %v", warnings)
	}
}

// TestUndatedNoticesSortLast covers DST-006.
func TestUndatedNoticesSortLast(t *testing.T) {
	t.Parallel()
	d := func(day int) time.Time { return time.Date(2026, 11, day, 0, 0, 0, 0, time.UTC) }
	ns := []Notice{{ID: "B"}, {ID: "C", Published: d(20)}, {ID: "A"}, {ID: "D", Published: d(18)}}
	sortOldestFirst(ns)
	var ids []string
	for _, n := range ns {
		ids = append(ids, n.ID)
	}
	if strings.Join(ids, ",") != "D,C,A,B" {
		t.Errorf("order = %v", ids)
	}
}

// TestParsingDefersAnalysis covers DST-007: parsers only decode; the costly
// text work runs in Analyze, which sources call after the package filter.
func TestParsingDefersAnalysis(t *testing.T) {
	t.Parallel()
	notices, _, _, err := ParseUbuntuNotices(fixture(t, "ubuntu-notices-noble.json"), "noble")
	if err != nil {
		t.Fatal(err)
	}
	dsas, _, err := ParseDebianFeed(fixture(t, "debian-dsa-long.rdf"))
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range append(notices, dsas...) {
		if n.analyzed || len(n.Paragraphs) > 0 || n.Remote || len(n.Impacts) > 0 {
			t.Fatalf("%s analyzed during parsing", n.ID)
		}
	}
	n := dsas[len(dsas)-1]
	n.Analyze()
	first := n.Paragraphs
	n.rawDesc = "A remote attacker could"
	n.Analyze() // idempotent: the second call changes nothing
	if len(first) == 0 || len(n.Paragraphs) != len(first) || n.Remote {
		t.Errorf("Analyze not idempotent: %+v", n)
	}
	// A hand-built Notice (no raw text) keeps what the caller set.
	manual := Notice{Paragraphs: []string{"kept"}, Assessment: Assessment{Remote: true}}
	manual.Analyze()
	if len(manual.Paragraphs) != 1 || !manual.Remote {
		t.Errorf("Analyze clobbered a hand-built Notice: %+v", manual)
	}
}

// TestDebianTitlePackageValidated: the DSA title's package token follows
// the same rule as the config's package lists (DST-009).
func TestDebianTitlePackageValidated(t *testing.T) {
	t.Parallel()
	feed := `<?xml version="1.0"?><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#" xmlns="http://purl.org/rss/1.0/"><channel><title>x</title></channel>
<item><title>DSA-1-1 NGINX - security update</title><description>x</description></item>
<item><title>DSA-2-1 php8.4 - security update</title><description>x</description></item></rdf:RDF>`
	notices, warnings, err := ParseDebianFeed([]byte(feed))
	if err != nil || len(notices) != 1 || notices[0].ID != "DSA-2-1" || len(warnings) != 1 {
		t.Errorf("notices %+v warnings %v err %v", notices, warnings, err)
	}
	if ValidPackageName("NGINX") || !ValidUbuntuRelease("noble") {
		t.Error("distrosec validators must match pkgname")
	}
}
