package httpx

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
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

// TestDoSanitizesNetworkErrors: net/http's *url.Error embeds the raw request
// URL; Do must run it through the sanitizer on both the retry-exhausted and
// the context paths, without hiding the cause from errors.Is.
func TestDoSanitizesNetworkErrors(t *testing.T) {
	t.Parallel()
	mask := WithURLSanitizer(func(u *url.URL) string {
		return strings.ReplaceAll(u.String(), "SECRET", "***")
	})

	c := New(WithSleep(nopSleep), WithMaxRetries(1), mask)
	req, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1:1/botSECRET/getMe", nil)
	_, err := c.Do(context.Background(), req)
	if err == nil || strings.Contains(err.Error(), "SECRET") || !strings.Contains(err.Error(), "/bot***/getMe") {
		t.Errorf("retry-exhausted error leaks or lacks the masked URL: %v", err)
	}
	if _, ok := errors.AsType[*url.Error](err); !ok {
		t.Errorf("cause lost: %v", err)
	}

	hang := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer hang.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	c = New(WithSleep(nopSleep), mask)
	req, _ = http.NewRequest(http.MethodGet, hang.URL+"/botSECRET/getMe", nil)
	_, err = c.Do(ctx, req)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want DeadlineExceeded", err)
	}
	if strings.Contains(err.Error(), "SECRET") {
		t.Errorf("context error leaks the URL: %v", err)
	}
}

// TestRetryAfterHonouredWithinLimit: a Retry-After at the limit replaces
// the backoff and the request is retried.
func TestRetryAfterHonouredWithinLimit(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", strconv.Itoa(int(MaxRetryAfter/time.Second)))
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	var slept []time.Duration
	c := New(WithSleep(func(_ context.Context, d time.Duration) error {
		slept = append(slept, d)
		return nil
	}))
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	resp, err := c.Do(context.Background(), req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	_ = resp.Body.Close()
	if len(slept) != 1 || slept[0] != MaxRetryAfter {
		t.Errorf("slept %v, want [%v]", slept, MaxRetryAfter)
	}
}

// TestRetryAfterBeyondLimitStopsRetrying: an upstream asking for a pause
// longer than MaxRetryAfter must not stall the one-shot run; Do gives up at
// once without sleeping.
func TestRetryAfterBeyondLimitStopsRetrying(t *testing.T) {
	t.Parallel()
	for _, header := range []string{
		"3600",
		"999999999999",
		time.Now().Add(2 * time.Hour).UTC().Format(http.TimeFormat),
	} {
		var calls atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			calls.Add(1)
			w.Header().Set("Retry-After", header)
			w.WriteHeader(http.StatusServiceUnavailable)
		}))

		slept := 0
		c := New(WithSleep(func(context.Context, time.Duration) error {
			slept++
			return nil
		}))
		req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
		_, err := c.Do(context.Background(), req)
		srv.Close()
		if err == nil {
			t.Fatalf("Retry-After %q: want error", header)
		}
		if calls.Load() != 1 || slept != 0 {
			t.Errorf("Retry-After %q: %d calls, %d sleeps; want 1 call, no sleep", header, calls.Load(), slept)
		}
		for _, want := range []string{"giving up after 1 attempts", "http 503", "exceeds the 1m0s limit"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("Retry-After %q: error %q missing %q", header, err, want)
			}
		}
	}
}

func TestRetryAfterHugeValueDoesNotOverflow(t *testing.T) {
	t.Parallel()
	h := http.Header{}
	h.Set("Retry-After", "9223372036854775807")
	if got := retryAfter(h); got <= MaxRetryAfter {
		t.Errorf("retryAfter = %v, want a positive value above the limit", got)
	}
}

func TestRetryPolicies(t *testing.T) {
	t.Parallel()
	dial := &url.Error{Op: "Post", URL: "https://x", Err: &net.OpError{Op: "dial", Err: errors.New("connection refused")}}
	dns := &url.Error{Op: "Post", URL: "https://x", Err: &net.DNSError{Err: "no such host", Name: "x"}}
	reset := &url.Error{Op: "Post", URL: "https://x", Err: &net.OpError{Op: "read", Err: errors.New("connection reset")}}
	eof := &url.Error{Op: "Post", URL: "https://x", Err: io.ErrUnexpectedEOF}
	for _, tc := range []struct {
		name         string
		status       int
		err          error
		def, notSent bool
	}{
		{"dial", 0, dial, true, true},
		{"dns", 0, dns, true, true},
		{"reset after write", 0, reset, true, false},
		{"eof", 0, eof, true, false},
		{"429", 429, nil, true, true},
		{"500", 500, nil, true, false},
		{"503", 503, nil, true, false},
		{"404", 404, nil, false, false},
	} {
		if got := DefaultRetryable(tc.status, tc.err); got != tc.def {
			t.Errorf("DefaultRetryable(%s) = %v, want %v", tc.name, got, tc.def)
		}
		if got := NotSentRetryable(tc.status, tc.err); got != tc.notSent {
			t.Errorf("NotSentRetryable(%s) = %v, want %v", tc.name, got, tc.notSent)
		}
	}
}

func TestRetryPolicyStopsOnNonRetryableStatus(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(srv.Close)
	c := New(WithSleep(nopSleep), WithRetryPolicy(NotSentRetryable))
	req, _ := http.NewRequest(http.MethodPost, srv.URL, strings.NewReader("x"))
	resp, err := c.Do(context.Background(), req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway || calls.Load() != 1 {
		t.Errorf("status %d after %d calls, want the 502 returned after 1 call", resp.StatusCode, calls.Load())
	}

	c.SetRetryPolicy(nil) // back to DefaultRetryable
	calls.Store(0)
	req, _ = http.NewRequest(http.MethodPost, srv.URL, strings.NewReader("x"))
	if _, err := c.Do(context.Background(), req); err == nil || calls.Load() != DefaultMaxRetries+1 {
		t.Errorf("default policy: %d calls, err %v; want %d calls and an error", calls.Load(), err, DefaultMaxRetries+1)
	}
}

func TestRetryPolicyStopsOnNotRetryableError(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	c := New(WithSleep(nopSleep), WithRetryPolicy(func(int, error) bool { calls.Add(1); return false }),
		WithHTTPClient(&http.Client{Transport: roundTripErr{}}))
	req, _ := http.NewRequest(http.MethodGet, "https://example.invalid/", nil)
	_, err := c.Do(context.Background(), req)
	if err == nil || !strings.Contains(err.Error(), "giving up after 1 attempts") || calls.Load() != 1 {
		t.Errorf("err %v after %d policy calls; want one attempt", err, calls.Load())
	}
}

type roundTripErr struct{}

func (roundTripErr) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, io.ErrUnexpectedEOF
}

// TestRedirectDropsAPIKeyHeaderAcrossHosts: net/http forwards every header
// except Authorization and Cookie on a cross-host redirect, so the default
// client drops API-key headers itself.
func TestRedirectDropsAPIKeyHeaderAcrossHosts(t *testing.T) {
	t.Parallel()
	var gotKey atomic.Value
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey.Store(r.Header.Get("X-Api-Key") + "|" + r.Header.Get("X-Other"))
	}))
	t.Cleanup(other.Close)
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/same" {
			gotKey.Store(r.Header.Get("X-Api-Key"))
			return
		}
		target := other.URL
		if r.URL.Query().Get("to") == "same" {
			target = "/same"
		}
		http.Redirect(w, r, target, http.StatusFound)
	}))
	t.Cleanup(first.Close)

	c := New(WithSleep(nopSleep))
	req, _ := http.NewRequest(http.MethodGet, first.URL, nil)
	req.Header.Set("X-Api-Key", "secret")
	req.Header.Set("X-Other", "kept")
	resp, err := c.Do(context.Background(), req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	_ = resp.Body.Close()
	if got := gotKey.Load(); got != "|kept" {
		t.Errorf("cross-host redirect forwarded %q, want the key dropped and other headers kept", got)
	}

	req, _ = http.NewRequest(http.MethodGet, first.URL+"?to=same", nil)
	req.Header.Set("X-Api-Key", "secret")
	resp, err = c.Do(context.Background(), req)
	if err != nil {
		t.Fatalf("Do same-host: %v", err)
	}
	_ = resp.Body.Close()
	if got := gotKey.Load(); got != "secret" {
		t.Errorf("same-host redirect: key = %q, want it kept", got)
	}
}
