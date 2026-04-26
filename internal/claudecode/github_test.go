package claudecode

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGitHubFetchReleaseDirect(t *testing.T) {
	t.Parallel()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/tags/v2.1.114") {
			t.Errorf("path = %q", r.URL.Path)
		}
		fmt.Fprint(w, `{
            "html_url": "https://github.com/anthropics/claude-code/releases/tag/v2.1.114",
            "body": "## Changes\n- new feature",
            "tag_name": "v2.1.114"
        }`)
	}))
	defer api.Close()

	g := NewGitHubClient(testHTTP(), "").WithBaseURLs(api.URL, "unused")
	notes, err := g.FetchReleaseNotes(context.Background(), "anthropics/claude-code", "2.1.114")
	if err != nil {
		t.Fatalf("FetchReleaseNotes: %v", err)
	}
	if !strings.Contains(notes.Body, "new feature") {
		t.Errorf("body = %q", notes.Body)
	}
	if notes.ReleaseURL == "" {
		t.Error("release URL missing")
	}
}

func TestGitHubFetchReleaseFallsBackToChangelog(t *testing.T) {
	t.Parallel()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer api.Close()
	raw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `# Changelog

## 2.1.114 - 2025-04-18
- foo
- bar

## 2.1.113
- older
`)
	}))
	defer raw.Close()

	g := NewGitHubClient(testHTTP(), "").WithBaseURLs(api.URL, raw.URL)
	notes, err := g.FetchReleaseNotes(context.Background(), "anthropics/claude-code", "2.1.114")
	if err != nil {
		t.Fatalf("FetchReleaseNotes: %v", err)
	}
	if !strings.Contains(notes.Body, "foo") || strings.Contains(notes.Body, "older") {
		t.Errorf("body = %q", notes.Body)
	}
	if !strings.Contains(notes.ReleaseURL, "CHANGELOG.md") {
		t.Errorf("expected CHANGELOG.md URL in fallback, got %q", notes.ReleaseURL)
	}
}

func TestGitHubFetchReleaseNotFound(t *testing.T) {
	t.Parallel()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer api.Close()
	raw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer raw.Close()

	g := NewGitHubClient(testHTTP(), "").WithBaseURLs(api.URL, raw.URL)
	_, err := g.FetchReleaseNotes(context.Background(), "anthropics/claude-code", "9.9.9")
	if !errors.Is(err, ErrReleaseNotFound) {
		t.Errorf("expected ErrReleaseNotFound, got %v", err)
	}
}

func TestGitHubTagWithoutV(t *testing.T) {
	t.Parallel()
	var triedV, triedPlain bool
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/tags/v2.1.114"):
			triedV = true
			http.NotFound(w, r)
		case strings.HasSuffix(r.URL.Path, "/tags/2.1.114"):
			triedPlain = true
			fmt.Fprint(w, `{"html_url": "x", "body": "plain-tag", "tag_name": "2.1.114"}`)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer api.Close()

	g := NewGitHubClient(testHTTP(), "").WithBaseURLs(api.URL, "unused")
	notes, err := g.FetchReleaseNotes(context.Background(), "owner/repo", "2.1.114")
	if err != nil {
		t.Fatalf("FetchReleaseNotes: %v", err)
	}
	if !triedV || !triedPlain {
		t.Errorf("expected both 'v'-prefixed and plain lookups; triedV=%v triedPlain=%v",
			triedV, triedPlain)
	}
	if !strings.Contains(notes.Body, "plain-tag") {
		t.Errorf("body = %q", notes.Body)
	}
}

func TestFetchLatestRelease_Success(t *testing.T) {
	t.Parallel()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/releases/latest") {
			t.Errorf("path = %q, want suffix /releases/latest", r.URL.Path)
		}
		fmt.Fprint(w, `{
            "html_url": "https://github.com/anthropics/claude-code/releases/tag/v2.1.119",
            "body": "## Changes\n- thing",
            "tag_name": "v2.1.119"
        }`)
	}))
	defer api.Close()

	g := NewGitHubClient(testHTTP(), "").WithBaseURLs(api.URL, "unused")
	rel, err := g.FetchLatestRelease(context.Background(), "anthropics/claude-code")
	if err != nil {
		t.Fatalf("FetchLatestRelease: %v", err)
	}
	if rel.Version != "2.1.119" {
		t.Errorf("version = %q, want %q (leading 'v' must be stripped)", rel.Version, "2.1.119")
	}
	if !strings.Contains(rel.Body, "thing") {
		t.Errorf("body = %q", rel.Body)
	}
	if rel.ReleaseURL == "" {
		t.Error("release URL missing")
	}
}

func TestFetchLatestRelease_NotFound(t *testing.T) {
	t.Parallel()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer api.Close()

	g := NewGitHubClient(testHTTP(), "").WithBaseURLs(api.URL, "unused")
	_, err := g.FetchLatestRelease(context.Background(), "owner/repo")
	if !errors.Is(err, ErrReleaseNotFound) {
		t.Errorf("err = %v, want ErrReleaseNotFound", err)
	}
}

func TestFetchLatestRelease_RejectsPrerelease(t *testing.T) {
	t.Parallel()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"html_url":"x","body":"y","tag_name":"v3.0.0-rc1","draft":false,"prerelease":true}`)
	}))
	defer api.Close()

	g := NewGitHubClient(testHTTP(), "").WithBaseURLs(api.URL, "unused")
	_, err := g.FetchLatestRelease(context.Background(), "owner/repo")
	if !errors.Is(err, ErrReleaseNotFound) {
		t.Errorf("prerelease must map to ErrReleaseNotFound, got %v", err)
	}
}

func TestFetchLatestRelease_RejectsDraft(t *testing.T) {
	t.Parallel()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"html_url":"x","body":"y","tag_name":"v3.0.0","draft":true,"prerelease":false}`)
	}))
	defer api.Close()

	g := NewGitHubClient(testHTTP(), "").WithBaseURLs(api.URL, "unused")
	_, err := g.FetchLatestRelease(context.Background(), "owner/repo")
	if !errors.Is(err, ErrReleaseNotFound) {
		t.Errorf("draft must map to ErrReleaseNotFound, got %v", err)
	}
}

func TestFetchLatestRelease_HTTPError(t *testing.T) {
	t.Parallel()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer api.Close()

	g := NewGitHubClient(testHTTP(), "").WithBaseURLs(api.URL, "unused")
	_, err := g.FetchLatestRelease(context.Background(), "owner/repo")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if errors.Is(err, ErrReleaseNotFound) {
		t.Errorf("non-404 status must not map to ErrReleaseNotFound, got %v", err)
	}
}

func TestGitHubAuthHeader(t *testing.T) {
	t.Parallel()
	var gotAuth string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		fmt.Fprint(w, `{"html_url":"x","body":"y","tag_name":"v1.0.0"}`)
	}))
	defer api.Close()

	g := NewGitHubClient(testHTTP(), "secret-token").WithBaseURLs(api.URL, "unused")
	_, err := g.FetchReleaseNotes(context.Background(), "owner/repo", "1.0.0")
	if err != nil {
		t.Fatalf("FetchReleaseNotes: %v", err)
	}
	if gotAuth != "Bearer secret-token" {
		t.Errorf("auth header = %q", gotAuth)
	}
}
