// Package drupalsec monitors drupal.org security advisories (core,
// contributed projects and public service announcements) from the combined
// security RSS feed and turns them into releasewatch candidates. It is fully
// deterministic: no LLM is involved.
package drupalsec

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/mmcdole/gofeed"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/olegiv/it-digest-bot/internal/httpx"
)

const (
	// PackageKey is the releases_seen.package value shared by every advisory.
	// The advisory identifier (SA-CONTRIB-2026-191, PSA-2026-09-21, …) is the
	// version, so (PackageKey, id) is the de-duplication key.
	PackageKey = "drupal-security"
	// DefaultFeedURL combines core, contrib and PSA advisories in one feed.
	DefaultFeedURL = "https://www.drupal.org/security/all/rss.xml"
	// SecurityPageURL is the human landing page for the advisories.
	SecurityPageURL = "https://www.drupal.org/security"

	// advisoryHost is the only host an item link may point to. The link is
	// rendered as a live Telegram link, so a feed-controlled value must not
	// be able to send readers anywhere else.
	advisoryHost = "drupal.org"

	maxFeedBody = 5 << 20
)

// Kind classifies an advisory by the part of the Drupal ecosystem it covers.
type Kind string

const (
	KindCore    Kind = "core"    // SA-CORE-*
	KindContrib Kind = "contrib" // SA-CONTRIB-*
	KindPSA     Kind = "psa"     // PSA-*
	KindOther   Kind = "other"   // unrecognised identifier
)

// Advisory is one parsed security advisory or public service announcement.
type Advisory struct {
	GUID               string    // raw RSS guid, e.g. "3623987 at https://www.drupal.org"
	ID                 string    // "SA-CONTRIB-2026-191"; empty when unrecognised
	Kind               Kind      // derived from ID
	Title              string    // RSS title
	Link               string    // advisory page
	Published          time.Time // RSS pubDate, else updated; zero when both are missing or unparsable
	ProjectName        string    // "Drupal core", "Webform"; empty for PSAs
	ProjectMachineName string    // "drupal", "webform"; empty for PSAs
	RiskLabel          string    // "Moderately critical"; empty for PSAs
	RiskScore          string    // "12/25"
	RiskVector         string    // "AC:Basic/A:User/CI:Some/II:Some/E:Theoretical/TD:Uncommon"
	Vulnerability      string    // "Cross Site Scripting (XSS)"
	AffectedVersions   string    // "<3.0.2"
	CVEs               []string  // may be empty
	Description        string    // plain text, one paragraph per line
	Solution           string    // plain text, "• " bullets for list items
}

// Version is the releases_seen version key: the advisory identifier, or a
// stable fallback derived from the guid for items without a recognisable
// identifier.
func (a *Advisory) Version() string {
	if a.ID != "" {
		return a.ID
	}
	if nid := leadingDigits(a.GUID); nid != "" {
		return "nid-" + nid
	}
	return strings.TrimSpace(a.GUID)
}

// Client fetches and parses the drupal.org security feed.
type Client struct {
	feedURL string
	http    *httpx.Client
}

// NewClient returns a client for the combined drupal.org security feed.
func NewClient(h *httpx.Client) *Client {
	if h == nil {
		h = httpx.New()
	}
	return &Client{feedURL: DefaultFeedURL, http: h}
}

// SetFeedURL overrides the feed URL in place (config override or tests) and
// returns the receiver for chaining. An empty value keeps the current URL.
// Validation (https, no credentials or query) is the caller's job; config
// does it at load time.
func (c *Client) SetFeedURL(u string) *Client {
	if strings.TrimSpace(u) != "" {
		c.feedURL = u
	}
	return c
}

// FeedURL returns the feed this client polls.
func (c *Client) FeedURL() string { return c.feedURL }

// Fetch downloads the feed and returns its advisories in feed order (newest
// first) plus warnings for items that had to be skipped.
func (c *Client) Fetch(ctx context.Context) ([]Advisory, []string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.feedURL, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("build drupal security feed request: %w", err)
	}
	req.Header.Set("Accept", "application/rss+xml, application/xml;q=0.9, text/xml;q=0.8, */*;q=0.5")

	resp, err := c.http.Do(ctx, req)
	if err != nil {
		return nil, nil, fmt.Errorf("fetch drupal security feed: %w", err)
	}
	if resp == nil {
		return nil, nil, fmt.Errorf("fetch drupal security feed: nil response")
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, nil, fmt.Errorf("drupal security feed: http %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxFeedBody+1))
	if err != nil {
		return nil, nil, fmt.Errorf("read drupal security feed: %w", err)
	}
	if int64(len(body)) > maxFeedBody {
		return nil, nil, fmt.Errorf("drupal security feed: response exceeds %d bytes", maxFeedBody)
	}
	return ParseFeed(body)
}

// ParseFeed decodes the RSS document. Items without a guid or title, or
// whose link is not an https drupal.org URL of at most MaxLinkBytes (see
// validAdvisoryLink), are skipped and reported in the returned warnings; a
// malformed document is an error.
func ParseFeed(data []byte) ([]Advisory, []string, error) {
	feed, err := gofeed.NewParser().Parse(bytes.NewReader(data))
	if err != nil {
		return nil, nil, fmt.Errorf("parse drupal security feed: %w", err)
	}

	advisories := make([]Advisory, 0, len(feed.Items))
	var warnings []string
	for i, item := range feed.Items {
		guid := strings.TrimSpace(item.GUID)
		link := strings.TrimSpace(item.Link)
		title := strings.TrimSpace(item.Title)
		switch {
		case guid == "":
			warnings = append(warnings, fmt.Sprintf("item %d (%q) skipped: missing guid", i, title))
			continue
		case link == "":
			warnings = append(warnings, fmt.Sprintf("item %d (%q) skipped: missing link", i, title))
			continue
		case !validAdvisoryLink(link):
			warnings = append(warnings, fmt.Sprintf("item %d (%q) skipped: link %.80q is not an https %s URL of at most %d bytes", i, title, link, advisoryHost, MaxLinkBytes))
			continue
		case title == "":
			warnings = append(warnings, fmt.Sprintf("item %d (%s) skipped: missing title", i, guid))
			continue
		}
		advisories = append(advisories, parseItem(item, guid, link, title))
	}
	return advisories, warnings, nil
}

// validAdvisoryLink reports whether link is safe to render as the advisory
// link: https, on drupal.org or a subdomain, at most MaxLinkBytes, without
// credentials, whitespace or control characters.
func validAdvisoryLink(link string) bool {
	if len(link) > MaxLinkBytes {
		return false
	}
	if strings.ContainsFunc(link, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) }) {
		return false
	}
	u, err := url.Parse(link)
	if err != nil || u.Scheme != "https" || u.User != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == advisoryHost || strings.HasSuffix(host, "."+advisoryHost)
}

// Description field keys as rendered by drupal.org: the token following
// `field field-name-` in the wrapping div's class attribute.
const (
	fieldProject          = "field-project"
	fieldMachineName      = "project-machine-name"
	fieldCriticality      = "field-sa-criticality"
	fieldType             = "field-sa-type"
	fieldAffectedVersions = "field-affected-versions"
	fieldCVE              = "field-sa-cve"
	fieldDescription      = "field-sa-description"
	fieldSolution         = "field-sa-solution"

	fieldDivMarker   = `<div class="field field-name-`
	fieldItemsMarker = `<div class="field-items">`
)

var (
	advisoryIDRe = regexp.MustCompile(`(?i)\b((?:SA-CORE|SA-CONTRIB|PSA)-\d{4}-\d+(?:-\d+)*)\b`)
	riskRe       = regexp.MustCompile(`^(.+?)\s+(\d+)\s*[/∕]\s*(\d+)\s+(\S+)$`)
	cveRe        = regexp.MustCompile(`\bCVE-\d{4}-\d{4,}\b`)
	projectHref  = regexp.MustCompile(`href="/project/([A-Za-z0-9_\-]+)"`)
	// \p{Zs} covers NBSP and the THIN SPACE drupal.org puts around the
	// risk score's division slash.
	spaceRunRe = regexp.MustCompile(`[\p{Zs}\t\r\f\v]+`)
	digitsRe   = regexp.MustCompile(`^\d+`)
)

func parseItem(item *gofeed.Item, guid, link, title string) Advisory {
	a := Advisory{GUID: guid, Link: link, Title: title}
	if item.PublishedParsed != nil {
		a.Published = item.PublishedParsed.UTC()
	} else if item.UpdatedParsed != nil {
		a.Published = item.UpdatedParsed.UTC()
	}
	a.ID = ParseAdvisoryID(link, title)
	a.Kind = KindFromID(a.ID)

	raw := item.Description
	if raw == "" {
		raw = item.Content
	}
	fields := parseDescription(raw)

	a.ProjectName = fields[fieldProject]
	a.ProjectMachineName = fields[fieldMachineName]
	if a.ProjectMachineName == "" {
		if m := projectHref.FindStringSubmatch(raw); m != nil {
			a.ProjectMachineName = m[1]
		}
	}
	a.RiskLabel, a.RiskScore, a.RiskVector = parseRisk(fields[fieldCriticality])
	a.Vulnerability = fields[fieldType]
	a.AffectedVersions = strings.Join(strings.Fields(fields[fieldAffectedVersions]), " ")
	a.CVEs = extractCVEs(fields[fieldCVE])
	a.Description = fields[fieldDescription]
	a.Solution = fields[fieldSolution]

	// Unstructured descriptions (a future markup change): keep the readable
	// text so the post is still useful.
	if len(fields) == 0 && strings.TrimSpace(raw) != "" {
		a.Description = htmlToText(raw, false)
	}
	return a
}

// MissingFields lists the fields every well-formed drupal.org item carries
// but this advisory lacks. A non-empty result means the feed markup has
// drifted from what parseDescription and ParseAdvisoryID expect; the post
// still goes out with what was parsed, so callers should log it.
func (a *Advisory) MissingFields() []string {
	var missing []string
	if a.ID == "" {
		missing = append(missing, "id")
	}
	if a.Published.IsZero() {
		missing = append(missing, "published")
	}
	if a.Kind == KindPSA {
		return missing
	}
	for _, f := range []struct{ name, value string }{
		{"project", a.ProjectName},
		{"risk", a.RiskLabel},
		{"vulnerability", a.Vulnerability},
		{"affected_versions", a.AffectedVersions},
		{"solution", a.Solution},
	} {
		if f.value == "" {
			missing = append(missing, f.name)
		}
	}
	return missing
}

// ParseAdvisoryID extracts the advisory identifier, preferring the link slug
// ("/sa-contrib-2026-191") and falling back to the title, whose last " - "
// segment carries the identifier on drupal.org.
func ParseAdvisoryID(link, title string) string {
	if u, err := url.Parse(strings.TrimSpace(link)); err == nil && u.Path != "" {
		if id := advisoryIDRe.FindString(strings.ToUpper(path.Base(u.Path))); id != "" {
			return id
		}
	}
	if i := strings.LastIndex(title, " - "); i >= 0 {
		if id := advisoryIDRe.FindString(strings.ToUpper(strings.TrimSpace(title[i+3:]))); id != "" {
			return id
		}
	}
	return strings.ToUpper(advisoryIDRe.FindString(title))
}

// KindFromID classifies an advisory identifier by prefix.
func KindFromID(id string) Kind {
	upper := strings.ToUpper(id)
	switch {
	case strings.HasPrefix(upper, "SA-CORE-"):
		return KindCore
	case strings.HasPrefix(upper, "SA-CONTRIB-"):
		return KindContrib
	case strings.HasPrefix(upper, "PSA-"):
		return KindPSA
	default:
		return KindOther
	}
}

// parseRisk splits "Moderately critical 13 ∕ 25 AC:Basic/…" into label,
// "13/25" and vector. Unrecognised text is kept whole as the label.
func parseRisk(s string) (label, score, vector string) {
	s = strings.TrimSpace(s)
	if m := riskRe.FindStringSubmatch(s); m != nil {
		return m[1], m[2] + "/" + m[3], m[4]
	}
	return s, "", ""
}

// extractCVEs returns the distinct CVE identifiers in s, in order.
func extractCVEs(s string) []string {
	seen := map[string]bool{}
	var out []string
	for _, cve := range cveRe.FindAllString(s, -1) {
		if !seen[cve] {
			seen[cve] = true
			out = append(out, cve)
		}
	}
	return out
}

// parseDescription splits the drupal.org field rendering into a map keyed by
// field name (see the field* constants) with plain-text values.
func parseDescription(rawHTML string) map[string]string {
	fields := map[string]string{}
	chunks := strings.Split(rawHTML, fieldDivMarker)
	for _, chunk := range chunks[1:] {
		end := strings.IndexAny(chunk, `" `)
		if end <= 0 {
			continue
		}
		key := chunk[:end]
		body := chunk
		if _, after, found := strings.Cut(chunk, fieldItemsMarker); found {
			body = after
		}
		if text := htmlToText(body, key == fieldSolution); text != "" {
			fields[key] = text
		}
	}
	return fields
}

// blockElements end a line of text when they close.
var blockElements = map[atom.Atom]bool{
	atom.P: true, atom.Li: true, atom.Div: true, atom.Ul: true, atom.Ol: true,
	atom.Tr: true, atom.Blockquote: true,
	atom.H1: true, atom.H2: true, atom.H3: true, atom.H4: true, atom.H5: true, atom.H6: true,
}

// htmlToText converts an HTML fragment to plain text. It is built on
// html.Parse like internal/llm's stripHTML but keeps structure: entities
// decode exactly once, "&lt;3.0.2" survives as "<3.0.2", a stray "<" in prose
// is text, the contents of script, style and similar elements are dropped
// rather than leaking into the post, block elements end a line, and with
// bullets list items get "• ".
func htmlToText(fragment string, bullets bool) string {
	doc, err := html.Parse(strings.NewReader(fragment))
	if err != nil {
		// Unreachable with a strings.Reader; degrade like internal/llm does.
		return strings.Join(strings.Fields(fragment), " ")
	}
	var sb strings.Builder
	sb.Grow(len(fragment))
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		switch n.Type {
		case html.TextNode:
			sb.WriteString(n.Data)
			return
		case html.CommentNode, html.DoctypeNode:
			return
		case html.ElementNode:
			switch n.DataAtom {
			case atom.Script, atom.Style, atom.Noscript, atom.Template, atom.Iframe, atom.Svg:
				return
			case atom.Br:
				sb.WriteByte('\n')
				return
			case atom.Li:
				if bullets {
					sb.WriteString("\n• ")
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
		if n.Type == html.ElementNode && blockElements[n.DataAtom] {
			sb.WriteByte('\n')
		}
	}
	walk(doc)

	var out []string
	for line := range strings.SplitSeq(sb.String(), "\n") {
		line = strings.TrimSpace(spaceRunRe.ReplaceAllString(line, " "))
		if line == "" || (bullets && line == "•") {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func leadingDigits(s string) string {
	return digitsRe.FindString(strings.TrimSpace(s))
}
