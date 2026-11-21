package phpsec

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/olegiv/it-digest-bot/internal/releasewatch"
	"github.com/olegiv/it-digest-bot/internal/store"
	"github.com/olegiv/it-digest-bot/internal/telegram"
)

func newTestSource(t *testing.T, body string) *Source {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	src := NewSource(testHTTP(), "")
	src.Client.SetAPIURL(srv.URL)
	return src
}

func TestSourceCandidatesOldestFirst(t *testing.T) {
	t.Parallel()
	src := newTestSource(t, advisoriesJSON)
	if src.Name() != PackageKey || src.SeedPackage() != PackageKey {
		t.Fatalf("Name/SeedPackage = %q/%q", src.Name(), src.SeedPackage())
	}
	cands, err := src.Candidates(context.Background())
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	if len(cands) != 2 || cands[0].Version != "GHSA-9pqp-7h25-4f32" || cands[1].Version != "GHSA-4w77-75f9-2c8w" {
		t.Fatalf("candidates = %+v, want oldest published first", cands)
	}
	ann, err := cands[1].Render(context.Background())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if ann.ReleaseURL != cands[1].URL || ann.DryRunTitle != "PHP GHSA-4w77-75f9-2c8w" ||
		ann.Payload["cve"] != "CVE-2026-12345" || ann.Payload["severity"] != SeverityHigh {
		t.Errorf("announcement = %+v", ann)
	}
}

func TestSourceSeedNotice(t *testing.T) {
	t.Parallel()
	src := newTestSource(t, advisoriesJSON)
	cands, err := src.Candidates(context.Background())
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	ann, err := src.SeedNotice(context.Background(), cands)
	if err != nil {
		t.Fatalf("SeedNotice: %v", err)
	}
	if ann.Payload["newest"] != "GHSA-4w77-75f9-2c8w" || ann.Payload["count"] != 2 || ann.ReleaseURL != AdvisoriesPageURL {
		t.Errorf("payload = %+v", ann.Payload)
	}
	for _, want := range []string{"2 advisories", "newest: GHSA\\-4w77\\-75f9\\-2c8w, 2026\\-11\\-19", `\#PHP \#Security`} {
		if !strings.Contains(ann.Text, want) {
			t.Errorf("seed text missing %q:\n%s", want, ann.Text)
		}
	}
	ann, err = src.SeedNotice(context.Background(), []releasewatch.Candidate{{Version: "unknown"}})
	if err != nil {
		t.Fatalf("SeedNotice(unknown): %v", err)
	}
	if _, ok := ann.Payload["newest"]; ok || !strings.Contains(ann.Text, "1 advisory published") {
		t.Errorf("unknown candidate: %+v\n%s", ann.Payload, ann.Text)
	}
}

func TestSourceFetchError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	src := NewSource(testHTTP(), "")
	src.Client.SetAPIURL(srv.URL)
	if _, err := src.Candidates(context.Background()); err == nil {
		t.Fatal("want error")
	}
	if (&Source{}).client() == nil || (&Source{}).logger() == nil {
		t.Fatal("zero Source must fall back to defaults")
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

// TestSourceThroughRunner runs the source through the real Runner and store:
// the first run records the history and posts one notice, a new advisory is
// then posted exactly once.
func TestSourceThroughRunner(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	body := advisoriesJSON
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	ctx := context.Background()
	st, err := store.Open(ctx, "file:"+filepath.Join(t.TempDir(), "phpsec.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	src := NewSource(testHTTP(), "")
	src.Client.SetAPIURL(srv.URL)
	bot := &recordingSender{}
	r := &releasewatch.Runner{Sources: []releasewatch.Source{src}, Channel: "@ch", Bot: bot, Releases: st.Releases, Posts: st.Posts}

	res, err := r.Run(ctx)
	if err != nil {
		t.Fatalf("first Run: %v", err)
	}
	if res.SeededCount() != 2 || res.PostedCount() != 0 || len(bot.messages) != 1 {
		t.Fatalf("first run seeded %d posted %d messages %d, want 2/0/1", res.SeededCount(), res.PostedCount(), len(bot.messages))
	}

	mu.Lock()
	body = `[{"ghsa_id":"GHSA-7c2m-q5vr-3f8h","cve_id":"CVE-2026-23456","summary":"Critical bug","severity":"critical","state":"published","published_at":"2026-11-20T08:00:00Z"},` + advisoriesJSON[1:]
	mu.Unlock()

	res, err = r.Run(ctx)
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if res.PostedCount() != 1 || len(bot.messages) != 2 {
		t.Fatalf("second run posted %d messages %d, want 1/2", res.PostedCount(), len(bot.messages))
	}
	if !strings.Contains(bot.messages[1], "🔴 *PHP security advisory* — GHSA\\-7c2m\\-q5vr\\-3f8h") {
		t.Errorf("second run post:\n%s", bot.messages[1])
	}

	if res, err = r.Run(ctx); err != nil || res.PostedCount() != 0 || len(bot.messages) != 2 {
		t.Errorf("third run: posted %d, messages %d, err %v", res.PostedCount(), len(bot.messages), err)
	}
}

func TestFormatAdvisory(t *testing.T) {
	t.Parallel()
	advs, _, err := ParseAdvisories([]byte(advisoriesJSON))
	if err != nil {
		t.Fatalf("ParseAdvisories: %v", err)
	}
	got := FormatAdvisory(&advs[0])
	for _, want := range []string{
		"🟠 *PHP security advisory* — GHSA\\-4w77\\-75f9\\-2c8w\n",
		"*Heap buffer overflow in mb\\_convert\\_encoding\\(\\)*\n",
		"Severity: *High* 8\\.1\n",
		"CVE: CVE\\-2026\\-12345\n",
		"CWE: CWE\\-122\n",
		"Published: 2026\\-11\\-19 14:30 UTC\n",
		"• php\\-src: affected `< 8.2.36, >= 8.3.0 < 8.3.37`, fixed in `8.2.36, 8.3.37, 8.4.28, 8.5.13`\n",
		"A heap buffer overflow in mb\\_convert\\_encoding\\(\\) allows remote code execution",
		"[Advisory](https://github.com/php/php-src/security/advisories/GHSA-4w77-75f9-2c8w)",
		`\#PHP \#Security`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("post missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Details") || strings.Contains(got, "**") {
		t.Errorf("excerpt must be the first paragraph without markdown:\n%s", got)
	}

	minimal := FormatAdvisory(&Advisory{ID: "GHSA-aaaa-bbbb-cccc", Link: AdvisoriesPageURL + "/GHSA-aaaa-bbbb-cccc"})
	if !strings.Contains(minimal, "🛡️ *PHP security advisory*") || !strings.Contains(minimal, "Severity: *Unknown*") {
		t.Errorf("minimal post:\n%s", minimal)
	}
	if strings.Contains(minimal, "CVE:") || strings.Contains(minimal, "Affected") {
		t.Errorf("minimal post must omit absent fields:\n%s", minimal)
	}
}

// TestFormatAdvisoryEscapesMarkup: advisory text is untrusted, so links and
// code spans in it must come out inert.
func TestFormatAdvisoryEscapesMarkup(t *testing.T) {
	t.Parallel()
	a := &Advisory{
		ID:          "GHSA-aaaa-bbbb-cccc",
		Summary:     "[click](https://evil.example) `x`",
		Description: "Visit [here](https://evil.example) *now*",
		Vulnerabilities: []Vulnerability{
			{Package: "p`kg", Vulnerable: "< 1 `", Patched: "1\\"},
		},
		Link: AdvisoriesPageURL + "/GHSA-aaaa-bbbb-cccc",
	}
	got := FormatAdvisory(a)
	for _, bad := range []string{"[click](", "[here](", "`x`"} {
		if strings.Contains(got, bad) {
			t.Errorf("live markup %q leaked:\n%s", bad, got)
		}
	}
	if !strings.Contains(got, "affected `< 1 \\``") || !strings.Contains(got, "fixed in `1\\\\`") {
		t.Errorf("code spans not escaped:\n%s", got)
	}
}

func TestFormatAdvisoryBoundsSize(t *testing.T) {
	t.Parallel()
	huge := strings.Repeat("_*[]()~`>#+-=|{}.!\\", 400)
	vulns := make([]Vulnerability, maxVulnerabilities)
	for i := range vulns {
		vulns[i] = Vulnerability{Package: huge[:64], Vulnerable: huge[:128], Patched: huge[:128]}
	}
	a := &Advisory{
		ID:              "GHSA-aaaa-bbbb-cccc",
		CVE:             "CVE-2026-1234567",
		Summary:         huge[:maxSummaryBytes],
		Description:     huge,
		Severity:        SeverityCritical,
		Score:           9.8,
		CWEs:            []string{"CWE-1", "CWE-2", "CWE-3", "CWE-4", "CWE-5"},
		Vulnerabilities: vulns,
		Published:       time.Now(),
		Link:            AdvisoriesPageURL + "/GHSA-aaaa-bbbb-cccc",
	}
	got := FormatAdvisory(a)
	if len(got) > telegram.MaxMessageBytes {
		t.Fatalf("post is %d bytes, limit %d", len(got), telegram.MaxMessageBytes)
	}
	if !strings.Contains(got, `\(12 more\)`) || strings.Contains(got, "CWE\\-5") {
		t.Errorf("list caps not applied:\n%s", got)
	}
	if noExcerpt := render(a, false); len(noExcerpt) > telegram.MaxMessageBytes-1024 {
		t.Errorf("header + list alone is %d bytes; keep at least 1 KiB of headroom", len(noExcerpt))
	}
}

func TestExcerpt(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ in, want string }{
		{"### Summary\r\n\r\nFirst **line**\nsecond `code`.\n\nNext para", "First line second code."},
		{"```\ncode block\n```\nAfter fence", "After fence"},
		{"Text before\n```\nfence\n```", "Text before"},
		{"# Only heading", ""},
		{"", ""},
	} {
		if got := Excerpt(tc.in); got != tc.want {
			t.Errorf("Excerpt(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestCapField(t *testing.T) {
	t.Parallel()
	// limit 6 leaves 3 bytes of content before the 3-byte ellipsis.
	if got := capField(`abc\.def`, 6); got != "abc…" {
		t.Errorf("capField = %q", got)
	}
	if got := capField(`ab\.cdef`, 6); got != "ab…" {
		t.Errorf("dangling backslash not trimmed: %q", got)
	}
	if got := capField("ééé", 6); got != "ééé" {
		t.Errorf("exact fit: %q", got)
	}
	if got := capField("éééé", 6); got != "é…" {
		t.Errorf("rune boundary: %q", got)
	}
	if got := capField("short", 10); got != "short" {
		t.Errorf("no-op cap: %q", got)
	}
}

// TestFormatAdvisoryDropsForeignLinks covers PHP-002: advisory text is
// posted as plain text, where Telegram clients would auto-link any URL.
func TestFormatAdvisoryDropsForeignLinks(t *testing.T) {
	t.Parallel()
	a := &Advisory{
		ID:      "GHSA-aaaa-bbbb-cccc",
		Summary: "Overflow, see https://evil.example/x and www.evil.example",
		Description: "A remote attacker could crash PHP. Details: https://evil.example/poc " +
			"tg://resolve?domain=evil, fixed in https://www.php.net/ChangeLog-8.php and " +
			"https://github.com/php/php-src/commit/abc but not https://github.com/evil/php.",
		Link: AdvisoriesPageURL + "/GHSA-aaaa-bbbb-cccc",
	}
	got := FormatAdvisory(a)
	for _, bad := range []string{"evil", "tg:"} {
		if strings.Contains(got, bad) {
			t.Errorf("foreign link %q survived:\n%s", bad, got)
		}
	}
	for _, want := range []string{
		"*Overflow, see and*",
		"https://www\\.php\\.net/ChangeLog\\-8\\.php",
		"https://github\\.com/php/php\\-src/commit/abc",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("post missing %q:\n%s", want, got)
		}
	}
}

func TestAllowedLink(t *testing.T) {
	t.Parallel()
	for raw, want := range map[string]bool{
		"https://www.php.net/x":            true,
		"https://php.net/x":                true,
		"https://bugs.php.net/1":           true,
		"https://github.com/php/php-src/x": true,
		"http://www.php.net/x":             false,
		"https://php.net.evil.example/":    false,
		"https://evilphp.net/":             false,
		"https://github.com/phpevil/x":     false,
		"https://github.com/evil/php":      false,
		"https://user@php.net/":            false,
		"https://php.net:8443/":            false,
		"http://www.php.net":               false,
		// Dot segments leave /php/ once a browser resolves them.
		"https://github.com/php/../evil/x":           false,
		"https://github.com/php/%2e%2e/evil/x":       false,
		"https://github.com/php/%2E%2E/evil":         false,
		"https://github.com/php%2f..%2fevil/x":       false,
		"https://github.com/php/./x":                 false,
		"https://github.com/php/..%5cevil":           false,
		"https://github.com/php/php-src/":            true,
		"https://github.com/php/php-src/..x/1.2.3..": true,
	} {
		if got := allowedLink(raw); got != want {
			t.Errorf("allowedLink(%q) = %v, want %v", raw, got, want)
		}
	}
	got := plainText("see https://github.com/php/../evil/payload and https://github.com/php/php-src/pull/1")
	if got != "see and https://github.com/php/php-src/pull/1" {
		t.Errorf("plainText = %q", got)
	}
}

// TestSourceRejectsEmptyList covers PHP-004: an empty advisory list must not
// let a later first non-empty run take a new advisory into the baseline.
func TestSourceRejectsEmptyList(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	body := `[]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	ctx := context.Background()
	st, err := store.Open(ctx, "file:"+filepath.Join(t.TempDir(), "empty.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	src := NewSource(testHTTP(), "")
	src.Client.SetAPIURL(srv.URL)
	bot := &recordingSender{}
	r := &releasewatch.Runner{Sources: []releasewatch.Source{src}, Channel: "@ch", Bot: bot, Releases: st.Releases, Posts: st.Posts}

	if _, err := r.Run(ctx); err == nil || !strings.Contains(err.Error(), "no published advisories") {
		t.Fatalf("empty list: err = %v, want an error", err)
	}
	if len(bot.messages) != 0 {
		t.Fatalf("empty list posted %d messages", len(bot.messages))
	}

	// The endpoint recovers with the full history: that run seeds (one
	// notice), and the next new advisory is then posted on its own.
	mu.Lock()
	body = advisoriesJSON
	mu.Unlock()
	res, err := r.Run(ctx)
	if err != nil || res.SeededCount() != 2 || len(bot.messages) != 1 {
		t.Fatalf("recovery run: seeded %d messages %d err %v", res.SeededCount(), len(bot.messages), err)
	}
	mu.Lock()
	body = `[{"ghsa_id":"GHSA-7c2m-q5vr-3f8h","summary":"New","severity":"high","state":"published","published_at":"2026-11-21T08:00:00Z"},` + advisoriesJSON[1:]
	mu.Unlock()
	if res, err = r.Run(ctx); err != nil || res.PostedCount() != 1 {
		t.Errorf("new advisory: posted %d err %v", res.PostedCount(), err)
	}
}
