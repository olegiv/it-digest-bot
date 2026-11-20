package distrosec

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/mmcdole/gofeed"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/olegiv/it-digest-bot/internal/httpx"
)

const (
	// DefaultDebianFeedURL lists the most recent Debian Security Advisories
	// (about 30, two weeks or so) with their descriptions.
	DefaultDebianFeedURL = "https://www.debian.org/security/dsa-long.rdf"
	// DebianTrackerURL is the base of an advisory's security-tracker page,
	// which lists its CVEs and fixed versions per release.
	DebianTrackerURL = "https://security-tracker.debian.org/tracker/"
	// DebianDistro labels Debian posts. DSAs cover the supported stable
	// releases; the watched package names (php8.4, postgresql-17, …)
	// select the release that matters.
	DebianDistro = "Debian"

	maxDebianBody = 4 << 20
)

// dsaTitleRe matches "DSA-6642-1 nginx - security update" and
// "DSA-6637-2 needrestart - regression update".
var dsaTitleRe = regexp.MustCompile(`^(DSA-\d{1,6}-\d{1,3})\s+([a-z0-9][a-z0-9+.\-]{0,63})\s+-\s+([A-Za-z ]{1,40})$`)

// DebianClient reads the Debian Security Advisory feed.
type DebianClient struct {
	url  string
	http *httpx.Client
}

// NewDebianClient returns a client for the official DSA feed.
func NewDebianClient(h *httpx.Client) *DebianClient {
	if h == nil {
		h = httpx.New()
	}
	return &DebianClient{url: DefaultDebianFeedURL, http: h}
}

// SetURL overrides the feed URL (config override or tests). An empty value
// keeps the current URL; validation is the caller's job.
func (c *DebianClient) SetURL(u string) *DebianClient {
	if strings.TrimSpace(u) != "" {
		c.url = u
	}
	return c
}

// URL returns the feed this client polls.
func (c *DebianClient) URL() string { return c.url }

// Fetch downloads the feed and returns its advisories oldest first, plus
// warnings for items that had to be skipped.
func (c *DebianClient) Fetch(ctx context.Context) ([]Notice, []string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("build debian security feed request: %w", err)
	}
	req.Header.Set("Accept", "application/rss+xml, application/rdf+xml, application/xml;q=0.9, */*;q=0.1")
	resp, err := c.http.Do(ctx, req)
	if err != nil {
		return nil, nil, fmt.Errorf("fetch debian security feed: %w", err)
	}
	if resp == nil {
		return nil, nil, fmt.Errorf("fetch debian security feed: nil response")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		return nil, nil, fmt.Errorf("debian security feed: http %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxDebianBody+1))
	if err != nil {
		return nil, nil, fmt.Errorf("read debian security feed: %w", err)
	}
	if len(body) > maxDebianBody {
		return nil, nil, fmt.Errorf("debian security feed: response exceeds %d bytes", maxDebianBody)
	}
	return ParseDebianFeed(body)
}

// ParseDebianFeed parses the DSA RSS feed. Items whose title is not a DSA
// title are skipped with a warning; an empty or unparsable feed is an
// error, since debian.org always lists recent advisories.
func ParseDebianFeed(body []byte) ([]Notice, []string, error) {
	feed, err := gofeed.NewParser().Parse(bytes.NewReader(body))
	if err != nil {
		return nil, nil, fmt.Errorf("parse debian security feed: %w", err)
	}
	var (
		out      []Notice
		warnings []string
		seen     = map[string]bool{}
	)
	for i, item := range feed.Items {
		if item == nil {
			continue
		}
		title := cleanLine(item.Title, 200)
		m := dsaTitleRe.FindStringSubmatch(title)
		if m == nil {
			warnings = append(warnings, fmt.Sprintf("item %d: not a DSA title %.80q", i, title))
			continue
		}
		id, pkg, kind := m[1], m[2], strings.ToLower(strings.TrimSpace(m[3]))
		if seen[id] {
			continue
		}
		seen[id] = true
		n := Notice{
			ID:         id,
			Distro:     DebianDistro,
			Title:      pkg + " - " + kind,
			Packages:   []string{pkg},
			rawDesc:    item.Description,
			rawHTML:    true,
			rawContext: []string{title},
			Link:       DebianTrackerURL + id,
			Regression: strings.Contains(kind, "regression"),
		}
		if item.PublishedParsed != nil {
			n.Published = item.PublishedParsed.UTC()
		} else if item.UpdatedParsed != nil {
			n.Published = item.UpdatedParsed.UTC()
		}
		out = append(out, n)
	}
	if len(out) == 0 {
		return nil, warnings, fmt.Errorf("debian security feed: no advisories")
	}
	sortOldestFirst(out)
	return out, warnings, nil
}

// htmlParagraphs converts a DSA description (HTML) to plain text with a
// blank line between paragraphs. Links are dropped: the feed only links the
// advisory's tracker page, which the post links itself.
func htmlParagraphs(fragment string) string {
	nodes, err := html.ParseFragment(strings.NewReader(fragment), &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div})
	if err != nil {
		return fragment
	}
	var sb strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		switch n.Type {
		case html.TextNode:
			sb.WriteString(n.Data)
			return
		case html.ElementNode:
			switch n.DataAtom {
			case atom.A, atom.Script, atom.Style:
				return
			case atom.P, atom.Br, atom.Div, atom.Li:
				sb.WriteString("\n\n")
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	for _, n := range nodes {
		walk(n)
	}
	return sb.String()
}
