package httpx

import (
	"context"
	"errors"
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

func TestWithTimeoutOverridesHTTPClient(t *testing.T) {
	t.Parallel()
	c := New(WithTimeout(3 * time.Second))
	if c.http == nil || c.http.Timeout != 3*time.Second {
		t.Fatalf("timeout = %v, want 3s", c.http.Timeout)
	}
}

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
	if resp == nil {
		t.Fatal("Do returned nil response")
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
	if resp == nil {
		t.Fatal("Do returned nil response")
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
	if resp == nil {
		t.Fatal("Do returned nil response")
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
	if resp == nil {
		t.Fatal("Do returned nil response")
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

func TestDoRefusesHTTPSDowngradeRedirect(t *testing.T) {
	t.Parallel()
	var plainHits int32
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&plainHits, 1)
		_, _ = w.Write([]byte("forged"))
	}))
	defer plain.Close()
	var tlsHits int32
	secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&tlsHits, 1)
		http.Redirect(w, r, plain.URL+"/botSECRET/feed", http.StatusFound)
	}))
	defer secure.Close()

	c := New(WithSleep(nopSleep), WithURLSanitizer(func(u *url.URL) string {
		return strings.ReplaceAll(u.String(), "SECRET", "***")
	}))
	// Trust the test server's certificate but keep New's redirect policy.
	c.http.Transport = secure.Client().Transport

	req, _ := http.NewRequest(http.MethodGet, secure.URL+"/feed", nil)
	resp, err := c.Do(context.Background(), req)
	if resp != nil {
		_ = resp.Body.Close()
		t.Fatal("downgrade redirect must not return a response")
	}
	if !errors.Is(err, ErrInsecureRedirect) {
		t.Fatalf("err = %v, want ErrInsecureRedirect", err)
	}
	if strings.Contains(err.Error(), "SECRET") || !strings.Contains(err.Error(), "/bot***/feed") {
		t.Errorf("error must use the sanitizer for the redirect target: %v", err)
	}
	if n := atomic.LoadInt32(&plainHits); n != 0 {
		t.Errorf("plain-http target was contacted %d times", n)
	}
	if n := atomic.LoadInt32(&tlsHits); n != 1 {
		t.Errorf("https origin hit %d times, want 1 (no retry on a refused redirect)", n)
	}
}

func TestDoFollowsSafeRedirects(t *testing.T) {
	t.Parallel()
	final := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer final.Close()
	secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, final.URL+"/x", http.StatusMovedPermanently)
	}))
	defer secure.Close()
	plainFinal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer plainFinal.Close()
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, plainFinal.URL+"/x", http.StatusFound)
	}))
	defer plain.Close()

	c := New(WithSleep(nopSleep))
	// httptest TLS servers share one CA, so either client's transport
	// trusts both.
	c.http.Transport = secure.Client().Transport
	for _, start := range []string{secure.URL, plain.URL} {
		req, _ := http.NewRequest(http.MethodGet, start, nil)
		resp, err := c.Do(context.Background(), req)
		if err != nil {
			t.Fatalf("%s: Do: %v", start, err)
		}
		b, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if string(b) != "ok" {
			t.Errorf("%s: body = %q", start, b)
		}
	}
}

func TestCheckRedirectLimit(t *testing.T) {
	t.Parallel()
	req, _ := http.NewRequest(http.MethodGet, "https://example.org/", nil)
	via := make([]*http.Request, maxRedirects)
	for i := range via {
		via[i] = req
	}
	if err := checkRedirect(req, via); err == nil || errors.Is(err, ErrInsecureRedirect) {
		t.Errorf("checkRedirect at limit = %v, want redirect-limit error", err)
	}
	if err := checkRedirect(req, via[:1]); err != nil {
		t.Errorf("https → https: %v", err)
	}
}
