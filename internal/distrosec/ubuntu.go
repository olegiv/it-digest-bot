package distrosec

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

	"github.com/olegiv/it-digest-bot/internal/httpx"
)

const (
	// DefaultUbuntuNoticesURL is the Ubuntu Security Notices API.
	DefaultUbuntuNoticesURL = "https://ubuntu.com/security/notices.json"
	// UbuntuNoticePageURL is the base of a notice's human page.
	UbuntuNoticePageURL = "https://ubuntu.com/security/notices/"
	// DefaultUbuntuRelease is Ubuntu 24.04 LTS.
	DefaultUbuntuRelease = "noble"

	// UbuntuPageSize is the API maximum per request.
	UbuntuPageSize = 20
	// UbuntuPages is how many pages (newest first) one run reads. Ubuntu
	// publishes about 45 notices a week per LTS release, so 40 cover
	// several days, far more than an hourly run needs, with room for
	// bursts and missed runs.
	UbuntuPages = 2

	// maxUbuntuBody bounds one decompressed page. Kernel notices embed
	// thousands of CVE references; a page is about 3 MB today.
	maxUbuntuBody = 32 << 20
)

var (
	usnIDRe         = regexp.MustCompile(`^USN-\d{1,6}-\d{1,3}$`)
	codenameRe      = regexp.MustCompile(`^[a-z]{3,20}$`)
	ubuntuVersionRe = regexp.MustCompile(`^\d{2}\.\d{2}$`)
)

// ValidUbuntuRelease reports whether s looks like an Ubuntu release codename
// ("noble").
func ValidUbuntuRelease(s string) bool { return codenameRe.MatchString(s) }

// UbuntuClient reads Ubuntu Security Notices for one release.
type UbuntuClient struct {
	url     string
	release string
	http    *httpx.Client
}

// NewUbuntuClient returns a client for release (empty = DefaultUbuntuRelease).
func NewUbuntuClient(h *httpx.Client, release string) *UbuntuClient {
	if h == nil {
		h = httpx.New()
	}
	if release == "" {
		release = DefaultUbuntuRelease
	}
	return &UbuntuClient{url: DefaultUbuntuNoticesURL, release: release, http: h}
}

// SetURL overrides the API endpoint (config override or tests). An empty
// value keeps the current URL; validation is the caller's job.
func (c *UbuntuClient) SetURL(u string) *UbuntuClient {
	if strings.TrimSpace(u) != "" {
		c.url = u
	}
	return c
}

// Release returns the release codename this client reads.
func (c *UbuntuClient) Release() string { return c.release }

// URL returns the endpoint this client polls.
func (c *UbuntuClient) URL() string { return c.url }

// Fetch returns the newest notices for the release (UbuntuPages pages),
// oldest first, plus warnings for entries that had to be skipped.
func (c *UbuntuClient) Fetch(ctx context.Context) ([]Notice, []string, error) {
	var (
		all      []Notice
		warnings []string
		seen     = map[string]bool{}
	)
	for page := range UbuntuPages {
		body, err := c.fetchPage(ctx, page*UbuntuPageSize)
		if err != nil {
			return nil, nil, err
		}
		notices, warns, total, err := ParseUbuntuNotices(body, c.release)
		if err != nil {
			return nil, nil, err
		}
		warnings = append(warnings, warns...)
		for _, n := range notices {
			if !seen[n.ID] {
				seen[n.ID] = true
				all = append(all, n)
			}
		}
		if total == 0 && len(notices) > 0 {
			warnings = append(warnings, "notices API returned no total_results; only the first page was read")
			break
		}
		if (page+1)*UbuntuPageSize >= total {
			break
		}
	}
	sortOldestFirst(all)
	return all, warnings, nil
}

func (c *UbuntuClient) fetchPage(ctx context.Context, offset int) ([]byte, error) {
	u, err := url.Parse(c.url)
	if err != nil {
		return nil, fmt.Errorf("parse ubuntu notices url: %w", err)
	}
	q := u.Query()
	q.Set("release", c.release)
	q.Set("limit", strconv.Itoa(UbuntuPageSize))
	q.Set("offset", strconv.Itoa(offset))
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("build ubuntu notices request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("fetch ubuntu notices: %w", err)
	}
	if resp == nil {
		return nil, fmt.Errorf("fetch ubuntu notices: nil response")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		return nil, fmt.Errorf("ubuntu notices: http %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxUbuntuBody+1))
	if err != nil {
		return nil, fmt.Errorf("read ubuntu notices response: %w", err)
	}
	if len(body) > maxUbuntuBody {
		return nil, fmt.Errorf("ubuntu notices: response exceeds %d bytes", maxUbuntuBody)
	}
	return body, nil
}

type usnPage struct {
	Notices      []usnJSON `json:"notices"`
	TotalResults int       `json:"total_results"`
}

type usnJSON struct {
	ID              string                     `json:"id"`
	Title           string                     `json:"title"`
	Summary         string                     `json:"summary"`
	Description     string                     `json:"description"`
	Published       string                     `json:"published"`
	IsHidden        any                        `json:"is_hidden"`
	CVEIDs          []string                   `json:"cves_ids"`
	Releases        []usnRelease               `json:"releases"`
	ReleasePackages map[string][]usnPackageRef `json:"release_packages"`
}

type usnRelease struct {
	Codename   string `json:"codename"`
	Version    string `json:"version"`
	SupportTag string `json:"support_tag"`
}

type usnPackageRef struct {
	Name     string `json:"name"`
	Version  string `json:"version"`
	IsSource bool   `json:"is_source"`
	Pocket   string `json:"pocket"`
}

// ParseUbuntuNotices decodes one notices.json page and returns the notices
// that list packages for release, plus the API's total result count.
// Hidden notices and notices with an invalid ID are skipped, the latter
// with a warning.
func ParseUbuntuNotices(body []byte, release string) ([]Notice, []string, int, error) {
	var page usnPage
	if err := json.Unmarshal(body, &page); err != nil {
		return nil, nil, 0, fmt.Errorf("decode ubuntu notices response: %w", err)
	}
	var (
		out      []Notice
		warnings []string
	)
	for i := range page.Notices {
		raw := &page.Notices[i]
		id := strings.TrimSpace(raw.ID)
		if !usnIDRe.MatchString(id) {
			warnings = append(warnings, fmt.Sprintf("notice %d: invalid id %.40q", i, id))
			continue
		}
		if hidden(raw.IsHidden) {
			continue
		}
		refs, ok := raw.ReleasePackages[release]
		if !ok {
			continue // not fixed for this release
		}
		out = append(out, ubuntuNotice(id, raw, release, refs))
	}
	return out, warnings, page.TotalResults, nil
}

func ubuntuNotice(id string, raw *usnJSON, release string, refs []usnPackageRef) Notice {
	n := Notice{
		ID:         id,
		Distro:     ubuntuLabel(raw.Releases, release),
		Title:      cleanLine(raw.Title, 200),
		CVEs:       validCVEs(raw.CVEIDs),
		Paragraphs: paragraphs(raw.Description),
		Published:  parseUbuntuTime(raw.Published),
		Link:       UbuntuNoticePageURL + id,
	}
	n.Regression = strings.Contains(strings.ToLower(n.Title), "regression")

	sources := map[string]string{}
	proOnly, binaries := true, 0
	for _, ref := range refs {
		name := strings.TrimSpace(ref.Name)
		if !ValidPackageName(name) {
			continue
		}
		if ref.IsSource {
			if !slices.Contains(n.Packages, name) {
				n.Packages = append(n.Packages, name)
			}
			if v := strings.TrimSpace(ref.Version); versionRe.MatchString(v) {
				sources[name] = v
			}
			continue
		}
		if ref.Pocket != "" {
			binaries++
			if !proPocket(ref.Pocket) {
				proOnly = false
			}
		}
	}
	n.ProOnly = binaries > 0 && proOnly
	for _, p := range n.Packages {
		if v, ok := sources[p]; ok {
			n.Fixed = append(n.Fixed, p+" "+v)
		}
	}
	n.Assessment = Assess(raw.Description, raw.Title, raw.Summary)
	return n
}

// ubuntuLabel is "Ubuntu 24.04 LTS" from the notice's release list, or
// "Ubuntu <codename>" when the release is not listed.
func ubuntuLabel(releases []usnRelease, codename string) string {
	for _, r := range releases {
		if r.Codename != codename || !ubuntuVersionRe.MatchString(r.Version) {
			continue
		}
		label := "Ubuntu " + r.Version
		if r.SupportTag == "LTS" {
			label += " LTS"
		}
		return label
	}
	return "Ubuntu " + codename
}

func parseUbuntuTime(s string) time.Time {
	s = strings.TrimSpace(s)
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999999", "2006-01-02T15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

// proPocket reports whether an archive pocket is available only with
// Ubuntu Pro: ESM (esm-infra, esm-apps), FIPS and the real-time kernel.
func proPocket(pocket string) bool {
	for _, prefix := range []string{"esm", "fips", "realtime"} {
		if strings.HasPrefix(pocket, prefix) {
			return true
		}
	}
	return false
}

func hidden(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return strings.EqualFold(strings.TrimSpace(x), "true")
	default:
		return false
	}
}
