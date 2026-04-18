package claudecode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/olegiv/it-digest-bot/internal/httpx"
)

// GitHubClient fetches release notes and CHANGELOG.md from github.com.
type GitHubClient struct {
	apiBaseURL string
	rawBaseURL string
	http       *httpx.Client
	token      string
}

// NewGitHubClient returns a client pointed at the public GitHub API.
// If `token` is non-empty it's sent as Authorization: Bearer <token> on
// api.github.com requests to lift unauthenticated rate limits.
func NewGitHubClient(h *httpx.Client, token string) *GitHubClient {
	return &GitHubClient{
		apiBaseURL: "https://api.github.com",
		rawBaseURL: "https://raw.githubusercontent.com",
		http:       h,
		token:      token,
	}
}

// WithBaseURLs overrides both the API and raw hosts (for tests).
func (g *GitHubClient) WithBaseURLs(api, raw string) *GitHubClient {
	g.apiBaseURL = api
	g.rawBaseURL = raw
	return g
}

// ReleaseNotes is the rendered-release representation the watcher uses.
type ReleaseNotes struct {
	Body       string // markdown
	ReleaseURL string // html_url for the GitHub release page
}

// ErrReleaseNotFound is returned when a tag has no GitHub release AND no
// matching section exists in CHANGELOG.md.
var ErrReleaseNotFound = errors.New("release notes not found")

// FetchReleaseNotes returns the markdown body of the GitHub release for a
// given repo and tag (e.g. repo="anthropics/claude-code", tag="2.1.114").
// If the release API returns 404 it falls back to parsing CHANGELOG.md
// from the repo's default branch.
func (g *GitHubClient) FetchReleaseNotes(ctx context.Context, repo, tag string) (*ReleaseNotes, error) {
	notes, err := g.fetchRelease(ctx, repo, "v"+tag)
	if err == nil {
		return notes, nil
	}
	if !errors.Is(err, errGitHub404) {
		return nil, err
	}
	// also try tag without the "v" prefix — some repos tag plain "x.y.z".
	notes, err = g.fetchRelease(ctx, repo, tag)
	if err == nil {
		return notes, nil
	}
	if !errors.Is(err, errGitHub404) {
		return nil, err
	}
	// fallback: parse CHANGELOG.md
	return g.fetchChangelog(ctx, repo, tag)
}

var errGitHub404 = errors.New("github 404")

type ghRelease struct {
	HTMLURL string `json:"html_url"`
	Body    string `json:"body"`
	TagName string `json:"tag_name"`
}

func (g *GitHubClient) fetchRelease(ctx context.Context, repo, tag string) (*ReleaseNotes, error) {
	u := fmt.Sprintf("%s/repos/%s/releases/tags/%s", g.apiBaseURL, repo, tag)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("build github request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if g.token != "" {
		req.Header.Set("Authorization", "Bearer "+g.token)
	}

	resp, err := g.http.Do(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("fetch github release %s %s: %w", repo, tag, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, errGitHub404
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github release %s %s: http %d", repo, tag, resp.StatusCode)
	}

	var rel ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, fmt.Errorf("decode github release: %w", err)
	}
	return &ReleaseNotes{Body: rel.Body, ReleaseURL: rel.HTMLURL}, nil
}

func (g *GitHubClient) fetchChangelog(ctx context.Context, repo, version string) (*ReleaseNotes, error) {
	u := fmt.Sprintf("%s/%s/main/CHANGELOG.md", g.rawBaseURL, repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("build changelog request: %w", err)
	}
	resp, err := g.http.Do(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("fetch changelog %s: %w", repo, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrReleaseNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("changelog %s: http %d", repo, resp.StatusCode)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read changelog: %w", err)
	}
	body := extractChangelogSection(string(b), version)
	if body == "" {
		return nil, ErrReleaseNotFound
	}
	return &ReleaseNotes{
		Body:       body,
		ReleaseURL: fmt.Sprintf("https://github.com/%s/blob/main/CHANGELOG.md", repo),
	}, nil
}
