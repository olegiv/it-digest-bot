package phpsec

import (
	"context"
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

// advisoriesJSON follows the "List repository security advisories" response
// schema of the GitHub REST API (2022-11-28), trimmed to the fields the
// client reads plus a few it must ignore. Newest published first, as
// requested with sort=published&direction=desc.
const advisoriesJSON = `[
{
  "ghsa_id": "GHSA-4w77-75f9-2c8w",
  "cve_id": "CVE-2026-12345",
  "url": "https://api.github.com/repos/php/php-src/security-advisories/GHSA-4w77-75f9-2c8w",
  "html_url": "https://github.com/php/php-src/security/advisories/GHSA-4w77-75f9-2c8w",
  "summary": "Heap buffer overflow in mb_convert_encoding()",
  "description": "### Summary\n\nA **heap buffer overflow** in ` + "`mb_convert_encoding()`" + ` allows\nremote code execution when converting attacker-controlled input.\n\n### Details\n\nMore text.",
  "severity": "high",
  "state": "published",
  "created_at": "2026-11-10T09:00:00Z",
  "updated_at": "2026-11-19T15:00:00Z",
  "published_at": "2026-11-19T14:30:00Z",
  "closed_at": null,
  "withdrawn_at": null,
  "vulnerabilities": [
    {"package": {"ecosystem": "other", "name": "php-src"}, "vulnerable_version_range": "< 8.2.36, >= 8.3.0 < 8.3.37", "patched_versions": "8.2.36, 8.3.37, 8.4.28, 8.5.13", "vulnerable_functions": []}
  ],
  "cvss": {"vector_string": "CVSS:3.1/AV:N/AC:H/PR:N/UI:N/S:U/C:H/I:H/A:H", "score": 8.1},
  "cvss_severities": {"cvss_v3": {"vector_string": "CVSS:3.1/AV:N/AC:H/PR:N/UI:N/S:U/C:H/I:H/A:H", "score": 8.1}, "cvss_v4": null},
  "cwes": [{"cwe_id": "CWE-122", "name": "Heap-based Buffer Overflow"}, {"cwe_id": "CWE-122", "name": "dup"}],
  "credits": []
},
{
  "ghsa_id": "GHSA-9pqp-7h25-4f32",
  "cve_id": null,
  "html_url": "https://evil.example/php/php-src/security/advisories/GHSA-9pqp-7h25-4f32",
  "summary": "Use-after-free in SplDoublyLinkedList",
  "description": null,
  "severity": "moderate",
  "state": "published",
  "published_at": "2026-06-05T12:00:00Z",
  "withdrawn_at": null,
  "vulnerabilities": [],
  "cvss": {"vector_string": null, "score": null},
  "cvss_severities": {"cvss_v3": null, "cvss_v4": {"vector_string": "CVSS:4.0/AV:L", "score": 5.4}},
  "cwes": []
},
{
  "ghsa_id": "GHSA-wpj3-hf5j-x4v4",
  "cve_id": "CVE-2025-1",
  "html_url": "https://github.com/php/php-src/security/advisories/GHSA-wpj3-hf5j-x4v4",
  "summary": "Withdrawn report",
  "severity": "low",
  "state": "published",
  "published_at": "2025-01-01T00:00:00Z",
  "withdrawn_at": "2025-02-01T00:00:00Z"
},
{
  "ghsa_id": "not-a-ghsa",
  "summary": "bad id",
  "state": "published"
},
{
  "ghsa_id": "GHSA-xxxx-yyyy-zzzz",
  "summary": "draft",
  "state": "draft"
}
]`

func TestParseAdvisories(t *testing.T) {
	t.Parallel()
	advs, warnings, err := ParseAdvisories([]byte(advisoriesJSON))
	if err != nil {
		t.Fatalf("ParseAdvisories: %v", err)
	}
	if len(advs) != 2 {
		t.Fatalf("advisories = %d, want 2 (withdrawn, invalid and draft skipped)", len(advs))
	}
	if len(warnings) != 2 {
		t.Errorf("warnings = %v, want withdrawn + invalid id", warnings)
	}

	a := advs[0]
	if a.ID != "GHSA-4w77-75f9-2c8w" || a.CVE != "CVE-2026-12345" || a.Severity != SeverityHigh || a.Score != 8.1 {
		t.Errorf("first advisory = %+v", a)
	}
	if a.Link != "https://github.com/php/php-src/security/advisories/GHSA-4w77-75f9-2c8w" {
		t.Errorf("link = %s", a.Link)
	}
	if len(a.CWEs) != 1 || a.CWEs[0] != "CWE-122" {
		t.Errorf("CWEs = %v, want deduplicated [CWE-122]", a.CWEs)
	}
	if len(a.Vulnerabilities) != 1 || a.Vulnerabilities[0].Package != "php-src" || !strings.HasPrefix(a.Vulnerabilities[0].Patched, "8.2.36") {
		t.Errorf("vulnerabilities = %+v", a.Vulnerabilities)
	}
	if !a.Published.Equal(time.Date(2026, 11, 19, 14, 30, 0, 0, time.UTC)) {
		t.Errorf("published = %v", a.Published)
	}

	b := advs[1]
	if b.CVE != "" || b.Severity != SeverityMedium || b.Score != 5.4 || b.Description != "" {
		t.Errorf("second advisory = %+v", b)
	}
	if b.Link != AdvisoriesPageURL+"/GHSA-9pqp-7h25-4f32" {
		t.Errorf("foreign html_url must be replaced by the canonical link, got %s", b.Link)
	}
}

func TestParseAdvisoriesErrors(t *testing.T) {
	t.Parallel()
	for _, body := range []string{``, `{}`, `{"message":"Not Found"}`, `not json`} {
		if _, _, err := ParseAdvisories([]byte(body)); err == nil {
			t.Errorf("ParseAdvisories(%q): want error", body)
		}
	}
	advs, _, err := ParseAdvisories([]byte(`[]`))
	if err != nil || len(advs) != 0 {
		t.Errorf("empty list: %v, %v", advs, err)
	}
	advs, _, err = ParseAdvisories([]byte(`[{"ghsa_id":"GHSA-aaaa-bbbb-cccc","state":"published"},{"ghsa_id":"GHSA-aaaa-bbbb-cccc","state":"published"}]`))
	if err != nil || len(advs) != 1 {
		t.Errorf("duplicates must collapse: %v, %v", advs, err)
	}
}

func TestAdvisoryLink(t *testing.T) {
	t.Parallel()
	const id = "GHSA-aaaa-bbbb-cccc"
	canonical := AdvisoriesPageURL + "/" + id
	for _, tc := range []struct{ in, want string }{
		{"https://github.com/php/php-src/security/advisories/" + id, "https://github.com/php/php-src/security/advisories/" + id},
		{"https://github.com/php/web-php/security/advisories/" + id, "https://github.com/php/web-php/security/advisories/" + id},
		{"http://github.com/php/php-src/security/advisories/" + id, canonical},
		{"https://github.com.evil/php/php-src/security/advisories/" + id, canonical},
		{"https://user@github.com/php/php-src/security/advisories/" + id, canonical},
		{"https://github.com/php/php-src/security/advisories/" + id + "?x=1", canonical},
		{"https://github.com/php/php-src/security/advisories/" + id + "#x", canonical},
		{"https://github.com/a/b/c/security/advisories/" + id, canonical},
		{"https://github.com/php/php-src/security/advisories/GHSA-other-id-zzzz", canonical},
		{"https://github.com/" + strings.Repeat("a", 300) + "/x/security/advisories/" + id, canonical},
		{"javascript:alert(1)", canonical},
		{"", canonical},
		{"%zz", canonical},
	} {
		if got := advisoryLink(id, tc.in); got != tc.want {
			t.Errorf("advisoryLink(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNormalizeSeverity(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ in, want string }{
		{"critical", SeverityCritical},
		{" HIGH ", SeverityHigh},
		{"medium", SeverityMedium},
		{"moderate", SeverityMedium},
		{"low", SeverityLow},
		{"", SeverityUnknown},
		{"bogus", SeverityUnknown},
	} {
		if got := normalizeSeverity(tc.in); got != tc.want {
			t.Errorf("normalizeSeverity(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestParseAdvisoriesClipsUntrustedFields(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("é", 2000)
	body := `[{"ghsa_id":"GHSA-aaaa-bbbb-cccc","state":"published","summary":"` + long + `",` +
		`"cvss":{"score":99},"cwes":[{"cwe_id":"CWE-79<script>"}],` +
		`"vulnerabilities":[` + strings.Repeat(`{"package":{"name":"p"},"patched_versions":"`+long+`"},`, 20) + `{}]}]`
	advs, _, err := ParseAdvisories([]byte(body))
	if err != nil {
		t.Fatalf("ParseAdvisories: %v", err)
	}
	a := advs[0]
	if len(a.Summary) > maxSummaryBytes || len(a.Vulnerabilities) != maxVulnerabilities {
		t.Errorf("summary %d bytes, %d vulnerabilities", len(a.Summary), len(a.Vulnerabilities))
	}
	if len(a.Vulnerabilities[0].Patched) > 128 || !strings.HasPrefix(a.Vulnerabilities[0].Patched, "é") {
		t.Errorf("patched clipped badly: %d bytes", len(a.Vulnerabilities[0].Patched))
	}
	if a.Score != 0 || len(a.CWEs) != 0 {
		t.Errorf("out-of-range score / invalid CWE must be dropped: %v %v", a.Score, a.CWEs)
	}
}

func TestClientFetch(t *testing.T) {
	t.Parallel()
	var gotQuery, gotAccept, gotAuth, gotVersion string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		gotAccept = r.Header.Get("Accept")
		gotAuth = r.Header.Get("Authorization")
		gotVersion = r.Header.Get("X-GitHub-Api-Version")
		_, _ = w.Write([]byte(advisoriesJSON))
	}))
	t.Cleanup(srv.Close)

	c := NewClient(testHTTP(), "secret-token").SetAPIURL(srv.URL)
	if c.APIURL() != srv.URL {
		t.Fatalf("APIURL = %s", c.APIURL())
	}
	advs, _, err := c.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(advs) != 2 {
		t.Errorf("advisories = %d", len(advs))
	}
	if gotQuery != "direction=desc&per_page=100&sort=published&state=published" {
		t.Errorf("query = %q", gotQuery)
	}
	if gotAccept != "application/vnd.github+json" || gotVersion != "2022-11-28" {
		t.Errorf("headers accept=%q version=%q", gotAccept, gotVersion)
	}
	if gotAuth != "" {
		t.Errorf("token must only be sent to https://api.github.com, got Authorization %q", gotAuth)
	}
	if NewClient(nil, "").SetAPIURL(" ").APIURL() != DefaultAPIURL {
		t.Error("blank override must keep the default URL")
	}
}

// TestClientSendsTokenToGitHubOnly checks the token gate with a transport
// that captures the request instead of reaching the network.
func TestClientSendsTokenToGitHubOnly(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		url      string
		wantAuth string
	}{
		{DefaultAPIURL, "Bearer secret-token"},
		{"https://API.GitHub.com/repos/php/php-src/security-advisories", "Bearer secret-token"},
		{"http://api.github.com/repos/php/php-src/security-advisories", ""},
		{"https://api.github.com.evil.example/repos/x", ""},
		{"https://ghe.example.com/api/v3/repos/php/php-src/security-advisories", ""},
	} {
		var gotAuth string
		rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
			gotAuth = r.Header.Get("Authorization")
			return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Header: http.Header{}, Request: r}, nil
		})
		h := httpx.New(httpx.WithHTTPClient(&http.Client{Transport: rt}), httpx.WithMaxRetries(0))
		_, _, _ = NewClient(h, "secret-token").SetAPIURL(tc.url).Fetch(context.Background())
		if gotAuth != tc.wantAuth {
			t.Errorf("%s: Authorization = %q, want %q", tc.url, gotAuth, tc.wantAuth)
		}
	}
	// No token configured: never send the header.
	var gotAuth string
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		gotAuth = r.Header.Get("Authorization")
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Header: http.Header{}, Request: r}, nil
	})
	h := httpx.New(httpx.WithHTTPClient(&http.Client{Transport: rt}), httpx.WithMaxRetries(0))
	_, _, _ = NewClient(h, "").Fetch(context.Background())
	if gotAuth != "" {
		t.Errorf("empty token sent Authorization %q", gotAuth)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestClientFetchErrors(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		handler http.HandlerFunc
		want    string
	}{
		{"not found", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) }, "http 404"},
		{"rate limit", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.WriteHeader(http.StatusForbidden)
		}, "rate limit exhausted"},
		{"forbidden", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) }, "http 403"},
		{"server error", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) }, "giving up"},
		{"oversize", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(strings.Repeat(" ", maxBody+1)))
		}, "exceeds"},
		{"bad json", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"message":"x"}`)) }, "decode"},
	} {
		srv := httptest.NewServer(tc.handler)
		_, _, err := NewClient(testHTTP(), "").SetAPIURL(srv.URL).Fetch(context.Background())
		srv.Close()
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", tc.name, err, tc.want)
		}
	}
	if _, _, err := NewClient(testHTTP(), "").SetAPIURL("http://[::1]:namedport").Fetch(context.Background()); err == nil {
		t.Error("invalid URL: want error")
	}
}
