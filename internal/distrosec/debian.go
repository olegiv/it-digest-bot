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
	// DebianDistro labels Debian posts. DSAs cover every supported stable
	// release and are not filtered by release: a versioned watched name
	// (php8.4, postgresql-17) selects one, an unversioned one (nginx)
	// matches DSAs for all of them.
	DebianDistro = "Debian"

	maxDebianBody = 4 << 20
)

// dsaTitleRe matches "DSA-6642-1 nginx - security update" and
// "DSA-6637-2 needrestart - regression update".
// The package token is checked with ValidPackageName (pkgname), the rule
// the config applies to watched package lists.
var dsaTitleRe = regexp.MustCompile(`^(DSA-\d{1,6}-\d{1,3})\s+(\S{1,64})\s+-\s+([A-Za-z ]{1,40})$`)

// dsaLooseTitleRe takes the ID and package from any other DSA title
// ("DSA-6650-1 nginx - security update (bookworm)"), so an unexpected
// suffix cannot hide an advisory for a watched package.
var dsaLooseTitleRe = regexp.MustCompile(`^(DSA-\d{1,6}-\d{1,3})\s+(\S{1,64})(?:\s+(.*))?$`)

// dsaTitle splits a DSA title into ID, package and kind ("security update").
// ok is false when no ID and valid package name can be read; strict is false
// when the title only matched the loose form, whose kind is cleaned and
// shortened rest-of-title text.
func dsaTitle(title string) (id, pkg, kind string, ok, strict bool) {
	if m := dsaTitleRe.FindStringSubmatch(title); m != nil && ValidPackageName(m[2]) {
		return m[1], m[2], strings.ToLower(strings.TrimSpace(m[3])), true, true
	}
	m := dsaLooseTitleRe.FindStringSubmatch(title)
	if m == nil || !ValidPackageName(m[2]) {
		return "", "", "", false, false
	}
	kind = strings.ToLower(cleanLine(plainText(strings.TrimLeft(m[3], "-– ")), 60))
	if kind == "" {
		kind = "security update"
	}
	return m[1], m[2], kind, true, false
}

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

// ParseDebianFeed parses the DSA RSS feed. An unusual DSA title is parsed
// loosely (dsaTitle) and reported in a warning; items without a DSA ID and
// valid package name are skipped with a warning; an empty or unparsable feed is an
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
		id, pkg, kind, ok, strict := dsaTitle(title)
		if !ok {
			warnings = append(warnings, fmt.Sprintf("item %d: not a DSA title %.80q", i, title))
			continue
		}
		if !strict {
			warnings = append(warnings, fmt.Sprintf("%s: unusual DSA title %.80q, posted as %s %q", id, title, pkg, kind))
		}
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
			// Block elements separate issues: the feed uses <p> today, but
			// a <dl> of CVE/description pairs or a table must not merge
			// two issues into one paragraph.
			case atom.P, atom.Br, atom.Div, atom.Li, atom.Ul, atom.Ol,
				atom.Dl, atom.Dt, atom.Dd, atom.Table, atom.Tr, atom.Blockquote, atom.Pre,
				atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
				sb.WriteString("\n\n")
			case atom.Td, atom.Th:
				sb.WriteString(" ")
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
