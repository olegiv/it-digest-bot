// Package gorelease monitors official Go toolchain releases.
package gorelease

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"golang.org/x/net/html"

	"github.com/olegiv/it-digest-bot/internal/httpx"
)

const (
	PackageKey               = "go"
	DefaultDownloadsURL      = "https://go.dev/dl/?mode=json"
	DefaultReleaseHistoryURL = "https://go.dev/doc/devel/release"
	maxAPIBody               = 4 << 20
)

// Client fetches Go release metadata from go.dev.
type Client struct {
	downloadsURL      string
	releaseHistoryURL string
	http              *httpx.Client
}

// NewClient returns a client pointed at the official Go release endpoints.
func NewClient(h *httpx.Client) *Client {
	if h == nil {
		h = httpx.New()
	}
	return &Client{
		downloadsURL:      DefaultDownloadsURL,
		releaseHistoryURL: DefaultReleaseHistoryURL,
		http:              h,
	}
}

// WithURLs overrides the go.dev URLs for tests.
func (c *Client) WithURLs(downloadsURL, releaseHistoryURL string) *Client {
	c.downloadsURL = downloadsURL
	c.releaseHistoryURL = releaseHistoryURL
	return c
}

// DownloadRelease is the subset of go.dev/dl/?mode=json used by the watcher.
type DownloadRelease struct {
	Version string         `json:"version"`
	Stable  bool           `json:"stable"`
	Files   []DownloadFile `json:"files"`
}

// DownloadFile is included for JSON compatibility with the official endpoint.
type DownloadFile struct {
	Filename string `json:"filename"`
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	Version  string `json:"version"`
	SHA256   string `json:"sha256"`
	Size     int64  `json:"size"`
	Kind     string `json:"kind"`
}

// FetchStable returns official stable Go releases, newest first.
func (c *Client) FetchStable(ctx context.Context) ([]DownloadRelease, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.downloadsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build go downloads request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	body, err := c.fetch(ctx, req, "go downloads")
	if err != nil {
		return nil, err
	}

	var payload []DownloadRelease
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode go downloads response: %w", err)
	}

	stable := make([]DownloadRelease, 0, len(payload))
	for _, rel := range payload {
		rel.Version = strings.TrimSpace(rel.Version)
		if !rel.Stable || rel.Version == "" {
			continue
		}
		stable = append(stable, rel)
	}
	if len(stable) == 0 {
		return nil, fmt.Errorf("go downloads: no stable releases")
	}
	return stable, nil
}

// FetchReleaseSummary returns short release text from Go Release History.
func (c *Client) FetchReleaseSummary(ctx context.Context, version string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.releaseHistoryURL, nil)
	if err != nil {
		return "", fmt.Errorf("build go release history request: %w", err)
	}
	req.Header.Set("Accept", "text/html")

	body, err := c.fetch(ctx, req, "go release history")
	if err != nil {
		return "", err
	}
	summary, err := ExtractReleaseSummary(body, version)
	if err != nil {
		return "", err
	}
	return summary, nil
}

func (c *Client) fetch(ctx context.Context, req *http.Request, label string) ([]byte, error) {
	resp, err := c.http.Do(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", label, err)
	}
	if resp == nil {
		return nil, fmt.Errorf("fetch %s: nil response", label)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, fmt.Errorf("%s: http %d", label, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxAPIBody+1))
	if err != nil {
		return nil, fmt.Errorf("read %s response: %w", label, err)
	}
	if int64(len(body)) > maxAPIBody {
		return nil, fmt.Errorf("%s: response exceeds %d bytes", label, maxAPIBody)
	}
	return body, nil
}

// ExtractReleaseSummary parses the Go Release History page and returns the
// human-readable text for a release anchor such as "go1.26.2".
func ExtractReleaseSummary(page []byte, version string) (string, error) {
	root, err := html.Parse(bytes.NewReader(page))
	if err != nil {
		return "", fmt.Errorf("parse go release history: %w", err)
	}

	target := findByID(root, version)
	if target == nil {
		return "", fmt.Errorf("go release history: %s not found", version)
	}

	text := textContent(target)
	if target.Data == "h2" {
		if next := nextElement(target, "p"); next != nil {
			text += " " + textContent(next)
		}
	}
	text = normalizeText(text)
	if text == "" {
		return "", fmt.Errorf("go release history: %s has empty text", version)
	}
	return text, nil
}

func findByID(n *html.Node, id string) *html.Node {
	if n == nil {
		return nil
	}
	for _, attr := range n.Attr {
		if attr.Key == "id" && attr.Val == id {
			return n
		}
	}
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		if found := findByID(child, id); found != nil {
			return found
		}
	}
	return nil
}

func nextElement(n *html.Node, name string) *html.Node {
	for next := n.NextSibling; next != nil; next = next.NextSibling {
		if next.Type == html.ElementNode && next.Data == name {
			return next
		}
	}
	return nil
}

func textContent(n *html.Node) string {
	if n == nil {
		return ""
	}
	if n.Type == html.TextNode {
		return n.Data
	}
	var b strings.Builder
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		b.WriteString(" ")
		b.WriteString(textContent(child))
	}
	return b.String()
}

func normalizeText(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
