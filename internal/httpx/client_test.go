package httpx

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func nopSleep(_ context.Context, _ time.Duration) error { return nil }

func TestDoSuccessFirstTry(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	c := New(WithSleep(nopSleep))
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	resp, err := c.Do(context.Background(), req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if string(b) != "ok" {
		t.Errorf("body = %q", b)
	}
}

func TestDoRetriesOn500ThenSucceeds(t *testing.T) {
	t.Parallel()
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n < 3 {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	c := New(WithSleep(nopSleep), WithMaxRetries(3))
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	resp, err := c.Do(context.Background(), req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	if atomic.LoadInt32(&calls) != 3 {
		t.Errorf("calls = %d, want 3", atomic.LoadInt32(&calls))
	}
}

func TestDoGivesUpAfterBudget(t *testing.T) {
	t.Parallel()
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.Error(w, "boom", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := New(WithSleep(nopSleep), WithMaxRetries(2))
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	_, err := c.Do(context.Background(), req)
	if err == nil {
		t.Fatal("expected error after retries exhausted")
	}
	if atomic.LoadInt32(&calls) != 3 {
		t.Errorf("calls = %d, want 3 (1 initial + 2 retries)", atomic.LoadInt32(&calls))
	}
	if !strings.Contains(err.Error(), "giving up") {
		t.Errorf("error = %v, want to contain 'giving up'", err)
	}
}

func TestDoNoRetryOn404(t *testing.T) {
	t.Parallel()
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer srv.Close()

	c := New(WithSleep(nopSleep))
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	resp, err := c.Do(context.Background(), req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d", resp.StatusCode)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Errorf("calls = %d, want 1 (404 is not retryable)", atomic.LoadInt32(&calls))
	}
}

func TestDoSetsUserAgent(t *testing.T) {
	t.Parallel()
	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	c := New(WithUserAgent("test-agent/1.0"), WithSleep(nopSleep))
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	resp, err := c.Do(context.Background(), req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	_ = resp.Body.Close()
	if gotUA != "test-agent/1.0" {
		t.Errorf("UA = %q", gotUA)
	}
}

func TestRetryAfterSeconds(t *testing.T) {
	t.Parallel()
	h := http.Header{}
	h.Set("Retry-After", "42")
	if got := retryAfter(h); got != 42*time.Second {
		t.Errorf("retryAfter = %v, want 42s", got)
	}
}

func TestRetryAfterHTTPDate(t *testing.T) {
	t.Parallel()
	future := time.Now().Add(10 * time.Second).UTC().Format(http.TimeFormat)
	h := http.Header{}
	h.Set("Retry-After", future)
	d := retryAfter(h)
	if d <= 0 || d > 11*time.Second {
		t.Errorf("retryAfter = %v, want ~10s", d)
	}
}

func TestURLSanitizerCalledOnGiveUp(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := New(
		WithSleep(nopSleep),
		WithMaxRetries(1),
		WithURLSanitizer(func(*url.URL) string { return "REDACTED" }),
	)
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/bot-secret-token/op", nil)
	_, err := c.Do(context.Background(), req)
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if strings.Contains(err.Error(), "bot-secret-token") {
		t.Errorf("error leaked original URL: %v", err)
	}
	if !strings.Contains(err.Error(), "REDACTED") {
		t.Errorf("expected sanitizer output in error, got %v", err)
	}
}

func TestContextCancelled(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c := New(WithSleep(nopSleep))
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	_, err := c.Do(ctx, req)
	if err == nil {
		t.Fatal("expected cancelled error")
	}
}
