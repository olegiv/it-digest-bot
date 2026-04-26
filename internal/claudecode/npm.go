// Package claudecode implements the Claude Code release source: poll npm
// for the latest version of @anthropic-ai/claude-code, cross-check against
// GitHub's "Latest release" marker, fetch release notes from GitHub (with a
// CHANGELOG fallback), and format a MarkdownV2 announcement for the shared
// releasewatch runner.
package claudecode

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/olegiv/it-digest-bot/internal/httpx"
)

// NPMClient fetches package metadata from the npm registry.
type NPMClient struct {
	baseURL string
	http    *httpx.Client
}

// NewNPMClient returns a client pointed at the public registry.
func NewNPMClient(h *httpx.Client) *NPMClient {
	return &NPMClient{
		baseURL: "https://registry.npmjs.org",
		http:    h,
	}
}

// WithBaseURL overrides the registry URL for tests.
func (c *NPMClient) WithBaseURL(u string) *NPMClient {
	c.baseURL = u
	return c
}

// PackageInfo is the slim subset of the npm registry payload we use.
type PackageInfo struct {
	LatestVersion string
	PublishedAt   time.Time
}

type npmPayload struct {
	DistTags map[string]string    `json:"dist-tags"`
	Time     map[string]time.Time `json:"time"`
}

// FetchLatest returns the latest version and its publish time.
func (c *NPMClient) FetchLatest(ctx context.Context, pkg string) (*PackageInfo, error) {
	u := fmt.Sprintf("%s/%s", c.baseURL, url.PathEscape(pkg))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("build npm request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("fetch npm %s: %w", pkg, err)
	}
	if resp == nil {
		return nil, fmt.Errorf("fetch npm %s: nil response", pkg)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, fmt.Errorf("npm %s: http %d", pkg, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxAPIBody+1))
	if err != nil {
		return nil, fmt.Errorf("read npm response: %w", err)
	}
	if int64(len(body)) > maxAPIBody {
		return nil, fmt.Errorf("npm %s: response exceeds %d bytes", pkg, maxAPIBody)
	}
	var payload npmPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode npm response: %w", err)
	}

	latest, ok := payload.DistTags["latest"]
	if !ok || latest == "" {
		return nil, fmt.Errorf("npm %s: no dist-tags.latest", pkg)
	}
	info := &PackageInfo{LatestVersion: latest}
	if t, ok := payload.Time[latest]; ok {
		info.PublishedAt = t
	}
	return info, nil
}
