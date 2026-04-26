package claudecode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

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

// LatestRelease is what /repos/{repo}/releases/latest returns: the
// version GitHub considers "Latest" (excludes prereleases and drafts),
// plus its notes body and html URL. The watcher uses this as a
// deliberate "release is officially shipped" signal that's harder to
// flip than npm's dist-tags.latest.
type LatestRelease struct {
	Version    string // tag_name with leading "v" stripped
	Body       string
	ReleaseURL string
}

// ErrReleaseNotFound is returned when a tag has no GitHub release AND no
// matching section exists in CHANGELOG.md, or when /releases/latest
// returns 404 (no published non-prerelease release).
var ErrReleaseNotFound = errors.New("release notes not found")

// FetchLatestRelease returns the version + notes for the release GitHub
// has marked as "Latest" (the /releases/latest endpoint, which excludes
// prereleases and drafts). 404 is mapped to ErrReleaseNotFound so
// callers can distinguish "not yet shipped" from transport errors.
func (g *GitHubClient) FetchLatestRelease(ctx context.Context, repo string) (*LatestRelease, error) {
	u := fmt.Sprintf("%s/repos/%s/releases/latest", g.apiBaseURL, repo)
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
		return nil, fmt.Errorf("fetch github latest %s: %w", repo, err)
	}
	if resp == nil {
		return nil, fmt.Errorf("fetch github latest %s: nil response", repo)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		// A 404 here is ambiguous: either the repo is inaccessible
		// (typo in github_repo config, repo deleted/renamed, missing
		// or insufficiently scoped GITHUB_TOKEN) or the repo exists
		// but has no qualifying release. Only the latter is a
		// defer-cleanly path; the former is a misconfiguration that
		// must be surfaced. Probe /repos/{repo} to disambiguate.
		exists, probeErr := g.repoAccessible(ctx, repo)
		if probeErr != nil {
			return nil, fmt.Errorf("verify repo %s after /releases/latest 404: %w", repo, probeErr)
		}
		if !exists {
			return nil, fmt.Errorf("github repo %s not accessible (check [claudecode].github_repo and GITHUB_TOKEN scope)", repo)
		}
		return nil, ErrReleaseNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github latest %s: http %d", repo, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxAPIBody+1))
	if err != nil {
		return nil, fmt.Errorf("read github latest: %w", err)
	}
	if int64(len(body)) > maxAPIBody {
		return nil, fmt.Errorf("github latest %s: response exceeds %d bytes", repo, maxAPIBody)
	}
	var rel ghRelease
	if err := json.Unmarshal(body, &rel); err != nil {
		return nil, fmt.Errorf("decode github latest: %w", err)
	}
	if rel.TagName == "" {
		return nil, fmt.Errorf("github latest %s: empty tag_name", repo)
	}
	// Belt-and-braces: /releases/latest is documented to exclude drafts
	// and prereleases, but recheck explicitly so a future API regression
	// can't silently announce a prerelease.
	if rel.Draft || rel.Prerelease {
		return nil, ErrReleaseNotFound
	}
	return &LatestRelease{
		Version:    strings.TrimPrefix(rel.TagName, "v"),
		Body:       rel.Body,
		ReleaseURL: rel.HTMLURL,
	}, nil
}

// repoAccessible probes /repos/{repo} and reports whether GitHub returns
// 200 (accessible) or 404 (not found / inaccessible). Other status codes
// or transport failures are returned as errors so the caller can decide
// how to treat them. Used by FetchLatestRelease to disambiguate a
// /releases/latest 404 from a repo-level configuration failure.
func (g *GitHubClient) repoAccessible(ctx context.Context, repo string) (bool, error) {
	u := fmt.Sprintf("%s/repos/%s", g.apiBaseURL, repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return false, fmt.Errorf("build repo probe: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if g.token != "" {
		req.Header.Set("Authorization", "Bearer "+g.token)
	}

	resp, err := g.http.Do(ctx, req)
	if err != nil {
		return false, fmt.Errorf("probe repo %s: %w", repo, err)
	}
	if resp == nil {
		return false, fmt.Errorf("probe repo %s: nil response", repo)
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	default:
		return false, fmt.Errorf("probe repo %s: http %d", repo, resp.StatusCode)
	}
}

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
	HTMLURL    string `json:"html_url"`
	Body       string `json:"body"`
	TagName    string `json:"tag_name"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
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
	if resp == nil {
		return nil, fmt.Errorf("fetch github release %s %s: nil response", repo, tag)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, errGitHub404
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github release %s %s: http %d", repo, tag, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxAPIBody+1))
	if err != nil {
		return nil, fmt.Errorf("read github release: %w", err)
	}
	if int64(len(body)) > maxAPIBody {
		return nil, fmt.Errorf("github release %s %s: response exceeds %d bytes", repo, tag, maxAPIBody)
	}
	var rel ghRelease
	if err := json.Unmarshal(body, &rel); err != nil {
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
	if resp == nil {
		return nil, fmt.Errorf("fetch changelog %s: nil response", repo)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrReleaseNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("changelog %s: http %d", repo, resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxAPIBody+1))
	if err != nil {
		return nil, fmt.Errorf("read changelog: %w", err)
	}
	if int64(len(b)) > maxAPIBody {
		return nil, fmt.Errorf("changelog %s: response exceeds %d bytes", repo, maxAPIBody)
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
