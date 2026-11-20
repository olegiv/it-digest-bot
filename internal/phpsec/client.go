// Package phpsec monitors official PHP security advisories. The PHP project
// publishes them as GitHub repository security advisories on php/php-src;
// this package polls the public REST endpoint and turns every published,
// non-withdrawn advisory into a releasewatch candidate. It is fully
// deterministic: no LLM is involved.
package phpsec

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/olegiv/it-digest-bot/internal/httpx"
)

const (
	// PackageKey is the releases_seen.package value; the GHSA ID is the
	// version.
	PackageKey = "php-security"

	// DefaultAPIURL lists php/php-src repository security advisories.
	DefaultAPIURL = "https://api.github.com/repos/php/php-src/security-advisories"
	// AdvisoriesPageURL is the human landing page for the advisories.
	AdvisoriesPageURL = "https://github.com/php/php-src/security/advisories"

	// githubAPIHost is the only host GITHUB_TOKEN is ever sent to, so a
	// config override pointing elsewhere cannot capture the token.
	githubAPIHost = "api.github.com"

	// PerPage is the advisory window fetched per run, newest published
	// first (the API maximum). PHP publishes a handful per year, so one
	// page covers the whole history and every hourly delta.
	PerPage = 100

	maxBody = 4 << 20

	// Field caps on untrusted advisory content (unescaped bytes). The
	// formatter caps again after escaping; these bound memory and logs.
	maxSummaryBytes     = 512
	maxDescriptionBytes = 16 << 10
	maxVulnerabilities  = 16
	maxLinkBytes        = 256
)

var (
	ghsaRe = regexp.MustCompile(`^GHSA(?:-[0-9a-z]{4}){3}$`)
	cveRe  = regexp.MustCompile(`^CVE-\d{4}-\d{4,7}$`)
	cweRe  = regexp.MustCompile(`^CWE-\d{1,5}$`)
)

// Severity levels as reported by GitHub.
const (
	SeverityCritical = "critical"
	SeverityHigh     = "high"
	SeverityMedium   = "medium"
	SeverityLow      = "low"
	SeverityUnknown  = "unknown"
)

// Vulnerability is one affected package entry of an advisory.
type Vulnerability struct {
	Package    string // e.g. "php-src"; may be empty
	Vulnerable string // vulnerable version range, e.g. "< 8.3.8"
	Patched    string // patched versions, e.g. "8.1.29, 8.2.20, 8.3.8"
}

// Advisory is one published PHP security advisory.
type Advisory struct {
	ID              string // GHSA ID, the dedup key
	CVE             string // empty when none is assigned
	Summary         string
	Description     string // markdown, untrusted
	Severity        string // one of the Severity* constants
	Score           float64
	CWEs            []string // "CWE-122"
	Vulnerabilities []Vulnerability
	Published       time.Time
	Link            string // github.com advisory page
}

// Client fetches advisories from the GitHub REST API.
type Client struct {
	apiURL string
	token  string
	http   *httpx.Client
}

// NewClient returns a client for the php/php-src advisories endpoint.
// token (GITHUB_TOKEN) is optional and raises the rate limit; it is sent
// only to api.github.com over https.
func NewClient(h *httpx.Client, token string) *Client {
	if h == nil {
		h = httpx.New()
	}
	return &Client{apiURL: DefaultAPIURL, token: token, http: h}
}

// SetAPIURL overrides the endpoint in place (config override or tests) and
// returns the receiver for chaining. An empty value keeps the current URL.
// Validation is the caller's job; config does it at load time.
func (c *Client) SetAPIURL(u string) *Client {
	if strings.TrimSpace(u) != "" {
		c.apiURL = u
	}
	return c
}

// APIURL returns the endpoint this client polls.
func (c *Client) APIURL() string { return c.apiURL }

// Fetch returns published, non-withdrawn advisories in API order (newest
// published first) plus warnings for entries that had to be skipped.
func (c *Client) Fetch(ctx context.Context) ([]Advisory, []string, error) {
	u, err := url.Parse(c.apiURL)
	if err != nil {
		return nil, nil, fmt.Errorf("parse php advisories url: %w", err)
	}
	q := u.Query()
	q.Set("state", "published")
	q.Set("sort", "published")
	q.Set("direction", "desc")
	q.Set("per_page", strconv.Itoa(PerPage))
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, nil, fmt.Errorf("build php advisories request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if c.token != "" && u.Scheme == "https" && strings.EqualFold(u.Hostname(), githubAPIHost) {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.http.Do(ctx, req)
	if err != nil {
		return nil, nil, fmt.Errorf("fetch php advisories: %w", err)
	}
	if resp == nil {
		return nil, nil, fmt.Errorf("fetch php advisories: nil response")
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxBody))
		if resp.StatusCode == http.StatusForbidden && resp.Header.Get("X-RateLimit-Remaining") == "0" {
			return nil, nil, fmt.Errorf("php advisories: github rate limit exhausted (set GITHUB_TOKEN to raise it)")
		}
		return nil, nil, fmt.Errorf("php advisories: http %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return nil, nil, fmt.Errorf("read php advisories response: %w", err)
	}
	if len(body) > maxBody {
		return nil, nil, fmt.Errorf("php advisories: response exceeds %d bytes", maxBody)
	}
	return ParseAdvisories(body)
}

type ghAdvisory struct {
	GHSAID      string     `json:"ghsa_id"`
	CVEID       *string    `json:"cve_id"`
	HTMLURL     string     `json:"html_url"`
	Summary     string     `json:"summary"`
	Description *string    `json:"description"`
	Severity    *string    `json:"severity"`
	State       string     `json:"state"`
	PublishedAt *time.Time `json:"published_at"`
	WithdrawnAt *time.Time `json:"withdrawn_at"`
	CVSS        *ghCVSS    `json:"cvss"`
	CVSSSevs    *struct {
		V3 *ghCVSS `json:"cvss_v3"`
		V4 *ghCVSS `json:"cvss_v4"`
	} `json:"cvss_severities"`
	CWEs []struct {
		CWEID string `json:"cwe_id"`
	} `json:"cwes"`
	Vulnerabilities []struct {
		Package *struct {
			Name string `json:"name"`
		} `json:"package"`
		VulnerableVersionRange *string `json:"vulnerable_version_range"`
		PatchedVersions        *string `json:"patched_versions"`
	} `json:"vulnerabilities"`
}

type ghCVSS struct {
	Score *float64 `json:"score"`
}

// ParseAdvisories decodes a GitHub repository security advisories response.
// Entries that are not published, are withdrawn, or carry an invalid GHSA ID
// are skipped; the latter two with a warning.
func ParseAdvisories(body []byte) ([]Advisory, []string, error) {
	var raw []ghAdvisory
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, nil, fmt.Errorf("decode php advisories response: %w", err)
	}
	out := make([]Advisory, 0, len(raw))
	var warnings []string
	seen := make(map[string]bool, len(raw))
	for i := range raw {
		g := &raw[i]
		id := strings.TrimSpace(g.GHSAID)
		if !ghsaRe.MatchString(id) {
			warnings = append(warnings, fmt.Sprintf("advisory %d: invalid ghsa_id %.40q", i, id))
			continue
		}
		if g.State != "" && g.State != "published" {
			continue
		}
		if g.WithdrawnAt != nil {
			warnings = append(warnings, "advisory "+id+": withdrawn")
			continue
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, toAdvisory(id, g))
	}
	return out, warnings, nil
}

func toAdvisory(id string, g *ghAdvisory) Advisory {
	a := Advisory{
		ID:       id,
		Summary:  clip(oneLine(g.Summary), maxSummaryBytes),
		Severity: normalizeSeverity(deref(g.Severity)),
		Score:    score(g),
		Link:     advisoryLink(id, g.HTMLURL),
	}
	if cve := strings.TrimSpace(deref(g.CVEID)); cveRe.MatchString(cve) {
		a.CVE = cve
	}
	a.Description = clip(deref(g.Description), maxDescriptionBytes)
	if g.PublishedAt != nil {
		a.Published = g.PublishedAt.UTC()
	}
	for _, cwe := range g.CWEs {
		if cweID := strings.TrimSpace(cwe.CWEID); cweRe.MatchString(cweID) && !slices.Contains(a.CWEs, cweID) {
			a.CWEs = append(a.CWEs, cweID)
		}
	}
	for _, v := range g.Vulnerabilities {
		if len(a.Vulnerabilities) == maxVulnerabilities {
			break
		}
		vuln := Vulnerability{
			Vulnerable: clip(oneLine(deref(v.VulnerableVersionRange)), 128),
			Patched:    clip(oneLine(deref(v.PatchedVersions)), 128),
		}
		if v.Package != nil {
			vuln.Package = clip(oneLine(v.Package.Name), 64)
		}
		if vuln != (Vulnerability{}) {
			a.Vulnerabilities = append(a.Vulnerabilities, vuln)
		}
	}
	return a
}

// advisoryLink returns the advisory's github.com page. The API value is used
// only when it is exactly the expected https github.com advisory URL for this
// ID; otherwise the canonical php/php-src URL is built, so a hostile or
// malformed html_url can never become a live link in the channel.
func advisoryLink(id, htmlURL string) string {
	canonical := AdvisoriesPageURL + "/" + id
	htmlURL = strings.TrimSpace(htmlURL)
	if len(htmlURL) > maxLinkBytes {
		return canonical
	}
	u, err := url.Parse(htmlURL)
	if err != nil || u.Scheme != "https" || u.Host != "github.com" || u.User != nil ||
		u.RawQuery != "" || u.Fragment != "" || !strings.HasSuffix(u.Path, "/security/advisories/"+id) {
		return canonical
	}
	if parts := strings.Split(strings.Trim(u.Path, "/"), "/"); len(parts) != 5 {
		return canonical
	}
	return u.String()
}

func score(g *ghAdvisory) float64 {
	candidates := []*ghCVSS{g.CVSS}
	if g.CVSSSevs != nil {
		candidates = append(candidates, g.CVSSSevs.V4, g.CVSSSevs.V3)
	}
	for _, c := range candidates {
		if c != nil && c.Score != nil && *c.Score > 0 && *c.Score <= 10 {
			return *c.Score
		}
	}
	return 0
}

func normalizeSeverity(s string) string {
	switch s = strings.ToLower(strings.TrimSpace(s)); s {
	case SeverityCritical, SeverityHigh, SeverityMedium, SeverityLow:
		return s
	case "moderate":
		return SeverityMedium
	default:
		return SeverityUnknown
	}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// clip cuts s to at most n bytes on a rune boundary.
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}
