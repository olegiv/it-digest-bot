// Package applerelease monitors stable iOS and macOS releases announced on
// the Apple Developer releases RSS feed and turns them into releasewatch
// candidates. Betas, release candidates and other Apple platforms in the
// same feed are ignored. It is fully deterministic: no LLM is involved.
package applerelease

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/mmcdole/gofeed"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/olegiv/it-digest-bot/internal/httpx"
)

const (
	// DefaultFeedURL lists every Apple Developer release (all OSes, Xcode,
	// betas, RCs, App Store Connect, …); ParseFeed keeps only the stable
	// releases of the tracked platforms.
	DefaultFeedURL = "https://developer.apple.com/news/releases/rss/releases.rss"
	// ReleasesPageURL is the human landing page for the feed.
	ReleasesPageURL = "https://developer.apple.com/news/releases/"
	// SecurityReleasesURL is Apple's index of security content per release.
	SecurityReleasesURL = "https://support.apple.com/en-us/100100"

	// appleHost is the only host an item or release-notes link may point
	// to. Links are rendered as live Telegram links, so a feed-controlled
	// value must not be able to send readers anywhere else.
	appleHost = "developer.apple.com"
	// appleBase resolves the feed's relative release-notes links
	// ("/go/?id=ios-27_2-rn").
	appleBase = "https://" + appleHost + "/"

	// MaxLinkBytes caps an unescaped link (Apple's are ~60 bytes).
	MaxLinkBytes = 512

	maxFeedBody = 5 << 20
)

// Platform is a tracked Apple operating system, spelled as in feed titles.
type Platform string

const (
	PlatformIOS   Platform = "iOS"
	PlatformMacOS Platform = "macOS"
)

// Platforms lists every tracked platform. Adding one here (and to
// titleRe) is all it takes to track another OS.
var Platforms = []Platform{PlatformIOS, PlatformMacOS}

// PackageKey is the releases_seen.package value for the platform ("ios",
// "macos"); the marketing version ("27.0.1") is the version.
func (p Platform) PackageKey() string { return strings.ToLower(string(p)) }

// Release is one stable OS release parsed from the feed.
type Release struct {
	Platform  Platform
	Name      string    // marketing name ("Tahoe"); empty when the title has none
	Version   string    // "27.0.1"
	Build     string    // "24A446"
	Title     string    // RSS title
	Link      string    // developer.apple.com announcement
	NotesURL  string    // developer.apple.com release notes; empty when absent
	Published time.Time // RSS pubDate; zero when missing or unparsable
}

// Major returns the leading version component, or 0 if it is unparsable.
func (r *Release) Major() int {
	head, _, _ := strings.Cut(r.Version, ".")
	n, err := strconv.Atoi(head)
	if err != nil {
		return 0
	}
	return n
}

// DisplayName is "iOS" or "macOS Tahoe".
func (r *Release) DisplayName() string {
	if r.Name == "" {
		return string(r.Platform)
	}
	return string(r.Platform) + " " + r.Name
}

// titleRe matches stable releases only: "iOS 27.0.1 (24A446)" and
// "macOS Tahoe 26.1 (25B78)". It is anchored, so "iOS 27.2 RC (24B115)",
// "macOS 27.2 beta 3 (26B5062e)" and other platforms ("iPadOS …") never
// match. Every component is length-bounded so a malformed or hostile title
// cannot grow a post past telegram.MaxMessageBytes.
var titleRe = regexp.MustCompile(`^(iOS|macOS)(?:\s+([A-Z][A-Za-z]{1,31}))?\s+(\d{1,4}(?:\.\d{1,4}){0,3})\s+\(([0-9A-Za-z]{1,16})\)$`)

// Client fetches and parses the Apple Developer releases feed.
type Client struct {
	feedURL string
	http    *httpx.Client
}

// NewClient returns a client for the Apple Developer releases feed.
func NewClient(h *httpx.Client) *Client {
	if h == nil {
		h = httpx.New()
	}
	return &Client{feedURL: DefaultFeedURL, http: h}
}

// SetFeedURL overrides the feed URL in place (config override or tests) and
// returns the receiver for chaining. An empty value keeps the current URL.
// Validation is the caller's job; config does it at load time.
func (c *Client) SetFeedURL(u string) *Client {
	if strings.TrimSpace(u) != "" {
		c.feedURL = u
	}
	return c
}

// FeedURL returns the feed this client polls.
func (c *Client) FeedURL() string { return c.feedURL }

// Fetch downloads the feed and returns the stable releases of all tracked
// platforms in feed order (newest first) plus warnings for matching items
// that had to be skipped.
func (c *Client) Fetch(ctx context.Context) ([]Release, []string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.feedURL, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("build apple releases feed request: %w", err)
	}
	req.Header.Set("Accept", "application/rss+xml, application/xml;q=0.9, text/xml;q=0.8, */*;q=0.5")

	resp, err := c.http.Do(ctx, req)
	if err != nil {
		return nil, nil, fmt.Errorf("fetch apple releases feed: %w", err)
	}
	if resp == nil {
		return nil, nil, fmt.Errorf("fetch apple releases feed: nil response")
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, nil, fmt.Errorf("apple releases feed: http %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxFeedBody+1))
	if err != nil {
		return nil, nil, fmt.Errorf("read apple releases feed: %w", err)
	}
	if int64(len(body)) > maxFeedBody {
		return nil, nil, fmt.Errorf("apple releases feed: response exceeds %d bytes", maxFeedBody)
	}
	return ParseFeed(body)
}

// ParseFeed decodes the RSS document and keeps items whose title is a stable
// release of a tracked platform. Non-matching items (betas, RCs, Xcode, other
// OSes) are dropped silently; matching items whose link is not an https
// developer.apple.com URL are skipped and reported in the warnings. A
// release-notes link that fails the same check is dropped, not the release.
// A malformed document is an error.
func ParseFeed(data []byte) ([]Release, []string, error) {
	feed, err := gofeed.NewParser().Parse(bytes.NewReader(data))
	if err != nil {
		return nil, nil, fmt.Errorf("parse apple releases feed: %w", err)
	}

	var (
		releases []Release
		warnings []string
	)
	for i, item := range feed.Items {
		title := strings.Join(strings.Fields(item.Title), " ")
		m := titleRe.FindStringSubmatch(title)
		if m == nil {
			continue
		}
		link := strings.TrimSpace(item.Link)
		if !validAppleLink(link) {
			warnings = append(warnings, fmt.Sprintf("item %d (%q) skipped: link %.80q is not an https %s URL of at most %d bytes", i, title, link, appleHost, MaxLinkBytes))
			continue
		}
		r := Release{
			Platform: Platform(m[1]),
			Name:     m[2],
			Version:  m[3],
			Build:    m[4],
			Title:    title,
			Link:     link,
		}
		if item.PublishedParsed != nil {
			r.Published = item.PublishedParsed.UTC()
		} else if item.UpdatedParsed != nil {
			r.Published = item.UpdatedParsed.UTC()
		}
		raw := item.Content
		if raw == "" {
			raw = item.Description
		}
		r.NotesURL = releaseNotesURL(raw)
		releases = append(releases, r)
	}
	return releases, warnings, nil
}

// releaseNotesURL returns the absolute target of the "View release notes"
// anchor in the item's HTML, or "" when there is none or it fails
// validAppleLink.
func releaseNotesURL(fragment string) string {
	doc, err := html.Parse(strings.NewReader(fragment))
	if err != nil {
		return ""
	}
	base, _ := url.Parse(appleBase)
	var found string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if found != "" {
			return
		}
		if n.Type == html.ElementNode && n.DataAtom == atom.A && strings.Contains(strings.ToLower(nodeText(n)), "release notes") {
			for _, attr := range n.Attr {
				if attr.Key != "href" {
					continue
				}
				ref, err := url.Parse(strings.TrimSpace(attr.Val))
				if err != nil {
					return
				}
				if abs := base.ResolveReference(ref).String(); validAppleLink(abs) {
					found = abs
				}
				return
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return found
}

func nodeText(n *html.Node) string {
	var sb strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			sb.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return sb.String()
}

// validAppleLink reports whether link is safe to render: https on exactly
// developer.apple.com, at most MaxLinkBytes, printable ASCII only (no
// whitespace, control or invisible/bidi characters such as U+202E that could
// disguise the target), without credentials.
func validAppleLink(link string) bool {
	if link == "" || len(link) > MaxLinkBytes {
		return false
	}
	if strings.ContainsFunc(link, func(r rune) bool { return r <= ' ' || r > '~' }) {
		return false
	}
	u, err := url.Parse(link)
	if err != nil || u.Scheme != "https" || u.User != nil {
		return false
	}
	return strings.EqualFold(u.Hostname(), appleHost)
}

// compareVersions orders dotted numeric versions ("26.10" > "26.9").
func compareVersions(a, b string) int {
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
