// Package phprelease monitors official PHP releases. It polls php.net's
// active-branches JSON, which lists the latest release of every supported
// branch, and turns each branch tip into a releasewatch candidate. Security
// releases are flagged from the release's "security" tag. It is fully
// deterministic: no LLM is involved.
package phprelease

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/olegiv/it-digest-bot/internal/httpx"
)

const (
	// PackageKey is the releases_seen.package value; the full version
	// ("8.5.13") is the version.
	PackageKey = "php"

	// DefaultActiveURL returns the latest release of every active branch
	// (security-supported branches plus those that reached end of life in
	// the last 28 days) as JSON.
	DefaultActiveURL = "https://www.php.net/releases/active.php"

	// SiteURL is the base for every link rendered in a post. Links are built
	// from the validated version, never taken from the response, so
	// php.net content can never send readers elsewhere.
	SiteURL = "https://www.php.net"
	// DownloadsURL is the php.net downloads page.
	DownloadsURL = SiteURL + "/downloads.php"
	// SupportedVersionsURL documents the support state of every branch.
	SupportedVersionsURL = SiteURL + "/supported-versions.php"

	maxBody = 1 << 20

	// MaxEntries bounds the branches ParseActive accepts. php.net lists
	// about five; a response with more is treated as broken rather than
	// turned into a burst of announcements.
	MaxEntries = 32
)

// versionRe accepts plain stable versions only ("8.5.13"), in canonical form:
// no leading zeros, so "08.5.1" can never become a second releases_seen key
// for "8.5.1". Pre-releases never appear in the active-branches list, and
// anything else would flow into URLs and posts, so it is skipped with a
// warning instead.
var versionRe = regexp.MustCompile(`^(0|[1-9]\d{0,2})\.(0|[1-9]\d{0,2})\.(0|[1-9]\d{0,3})$`)

// Release is the latest release of one active PHP branch.
type Release struct {
	Branch    string    // "8.5"
	Version   string    // "8.5.13"
	Date      time.Time // release date (UTC midnight); zero when unparsable
	Security  bool      // tagged as a security release
	Announced bool      // php.net has an announcement page for it
}

// Major returns the major version component.
func (r *Release) Major() int { return r.part(0) }

// Minor returns the minor version component.
func (r *Release) Minor() int { return r.part(1) }

// Patch returns the patch version component.
func (r *Release) Patch() int { return r.part(2) }

func (r *Release) part(i int) int {
	parts := strings.Split(r.Version, ".")
	if i >= len(parts) {
		return 0
	}
	n, err := strconv.Atoi(parts[i])
	if err != nil {
		return 0
	}
	return n
}

// IsNewBranch reports whether this is the first release of a branch (x.y.0).
func (r *Release) IsNewBranch() bool { return r.Patch() == 0 }

// ChangeLogURL is the release's anchor in php.net's per-major ChangeLog.
func (r *Release) ChangeLogURL() string {
	return fmt.Sprintf("%s/ChangeLog-%d.php#%s", SiteURL, r.Major(), r.Version)
}

// AnnouncementURL is php.net's announcement page, or "" when the release
// has none.
func (r *Release) AnnouncementURL() string {
	if !r.Announced {
		return ""
	}
	return SiteURL + "/releases/" + strings.ReplaceAll(r.Version, ".", "_") + ".php"
}

// Client fetches the php.net active-branches JSON.
type Client struct {
	activeURL string
	http      *httpx.Client
}

// NewClient returns a client for the official php.net endpoint.
func NewClient(h *httpx.Client) *Client {
	if h == nil {
		h = httpx.New()
	}
	return &Client{activeURL: DefaultActiveURL, http: h}
}

// SetActiveURL overrides the endpoint in place (config override or tests)
// and returns the receiver for chaining. An empty value keeps the current
// URL. Validation is the caller's job; config does it at load time.
func (c *Client) SetActiveURL(u string) *Client {
	if strings.TrimSpace(u) != "" {
		c.activeURL = u
	}
	return c
}

// ActiveURL returns the endpoint this client polls.
func (c *Client) ActiveURL() string { return c.activeURL }

// Fetch returns the latest release of every active branch, ordered by
// version ascending, plus warnings for entries that had to be skipped.
func (c *Client) Fetch(ctx context.Context) ([]Release, []string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.activeURL, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("build php releases request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(ctx, req)
	if err != nil {
		return nil, nil, fmt.Errorf("fetch php releases: %w", err)
	}
	if resp == nil {
		return nil, nil, fmt.Errorf("fetch php releases: nil response")
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxBody))
		return nil, nil, fmt.Errorf("php releases: http %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return nil, nil, fmt.Errorf("read php releases response: %w", err)
	}
	if len(body) > maxBody {
		return nil, nil, fmt.Errorf("php releases: response exceeds %d bytes", maxBody)
	}
	releases, warnings, err := ParseActive(body)
	if err != nil {
		return nil, warnings, err
	}
	return releases, warnings, nil
}

// activeEntry mirrors one branch entry of releases/active.php.
type activeEntry struct {
	Version      string          `json:"version"`
	Date         string          `json:"date"`
	Tags         []string        `json:"tags"`
	Announcement json.RawMessage `json:"announcement"`
}

// ParseActive decodes the active-branches JSON: an object keyed by major
// version ("8"), each an object keyed by branch ("8.5"). Entries with an
// invalid version, or whose version does not belong to the branch and
// major they are listed under, are skipped with a warning. An empty list
// is an error: php.net always lists at least one supported branch, so an
// empty answer means the endpoint changed.
func ParseActive(body []byte) ([]Release, []string, error) {
	var majors map[string]map[string]activeEntry
	// PHP's json_encode renders an empty array as [], not {}.
	if trimmed := bytes.TrimSpace(body); !bytes.Equal(trimmed, []byte("[]")) {
		if err := json.Unmarshal(trimmed, &majors); err != nil {
			return nil, nil, fmt.Errorf("decode php releases response: %w", err)
		}
	}

	entries := 0
	for _, branches := range majors {
		entries += len(branches)
	}
	if entries > MaxEntries {
		return nil, nil, fmt.Errorf("php releases: %d branches listed, more than the %d accepted", entries, MaxEntries)
	}

	var (
		out      []Release
		warnings []string
	)
	for major, branches := range majors {
		for branch, e := range branches {
			rel, err := toRelease(major, branch, e)
			if err != nil {
				warnings = append(warnings, err.Error())
				continue
			}
			out = append(out, rel)
		}
	}
	slices.SortFunc(out, func(a, b Release) int { return CompareVersions(a.Version, b.Version) })
	slices.Sort(warnings) // map iteration order is random; keep logs stable
	if len(out) == 0 {
		return nil, warnings, fmt.Errorf("php releases: no active branch listed")
	}
	return out, warnings, nil
}

func toRelease(major, branch string, e activeEntry) (Release, error) {
	version := strings.TrimSpace(e.Version)
	m := versionRe.FindStringSubmatch(version)
	if m == nil {
		return Release{}, fmt.Errorf("branch %.16q: invalid version %.32q", branch, version)
	}
	if m[1] != major || m[1]+"."+m[2] != branch {
		return Release{}, fmt.Errorf("branch %.16q: version %s does not belong to major %.8q", branch, version, major)
	}
	rel := Release{
		Branch:    branch,
		Version:   version,
		Date:      parseDate(e.Date),
		Security:  slices.ContainsFunc(e.Tags, func(t string) bool { return strings.EqualFold(strings.TrimSpace(t), "security") }),
		Announced: announced(e.Announcement),
	}
	return rel, nil
}

// announced interprets the "announcement" field: true, or a non-empty
// language → URL object, means php.net has an announcement page. The page
// URL itself is always rebuilt from the version (see AnnouncementURL).
func announced(raw json.RawMessage) bool {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return false
	}
	var b bool
	if err := json.Unmarshal(raw, &b); err == nil {
		return b
	}
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err == nil {
		return len(m) > 0
	}
	return false
}

// dateLayouts are the formats php.net has used for release dates
// ("19 Nov 2026", "07 June 2024").
var dateLayouts = []string{"2 Jan 2006", "2 January 2006", "2006-01-02"}

func parseDate(s string) time.Time {
	s = strings.TrimSpace(s)
	for _, layout := range dateLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// CompareVersions orders dotted numeric versions ("8.10.0" > "8.9.1").
func CompareVersions(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) || i < len(bs); i++ {
		var x, y int
		if i < len(as) {
			x, _ = strconv.Atoi(as[i])
		}
		if i < len(bs) {
			y, _ = strconv.Atoi(bs[i])
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}
