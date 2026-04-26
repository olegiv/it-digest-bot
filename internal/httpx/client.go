// Package httpx provides a shared HTTP client with a sane timeout, a
// project-wide User-Agent, and bounded retry/backoff on transient errors.
package httpx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/olegiv/it-digest-bot/internal/version"
)

const (
	DefaultTimeout    = 30 * time.Second
	DefaultMaxRetries = 3
	DefaultBaseDelay  = 500 * time.Millisecond
)

// Client wraps *http.Client with retry/backoff and default headers.
type Client struct {
	http         *http.Client
	userAgent    string
	maxRetries   int
	baseDelay    time.Duration
	sleep        func(context.Context, time.Duration) error
	urlSanitizer func(*url.URL) string
}

// Option configures the client at construction time.
type Option func(*Client)

// WithHTTPClient installs a custom *http.Client (e.g. from httptest).
func WithHTTPClient(c *http.Client) Option {
	return func(cl *Client) { cl.http = c }
}

// WithMaxRetries overrides the default retry budget.
func WithMaxRetries(n int) Option {
	return func(cl *Client) { cl.maxRetries = n }
}

// WithBaseDelay overrides the default backoff base.
func WithBaseDelay(d time.Duration) Option {
	return func(cl *Client) { cl.baseDelay = d }
}

// WithUserAgent overrides the default UA (useful in tests).
func WithUserAgent(ua string) Option {
	return func(cl *Client) { cl.userAgent = ua }
}

// WithSleep overrides the sleep implementation (tests can zero it out).
func WithSleep(fn func(context.Context, time.Duration) error) Option {
	return func(cl *Client) { cl.sleep = fn }
}

// WithURLSanitizer customizes how request URLs appear in error messages.
// The default masks userinfo credentials only (url.URL.Redacted); callers
// whose URLs embed secrets in the path — e.g. Telegram's /bot<TOKEN>/ —
// must install a sanitizer that also masks those segments.
func WithURLSanitizer(fn func(*url.URL) string) Option {
	return func(cl *Client) {
		if fn != nil {
			cl.urlSanitizer = fn
		}
	}
}

// SetURLSanitizer installs (or replaces) the sanitizer on an existing
// Client. Callers that own a subclient's security invariant (e.g.
// telegram.Bot, news.FeedFetcher) use this to guarantee their sanitizer
// is wired in even if the Client was constructed elsewhere without it.
// nil is ignored.
func (c *Client) SetURLSanitizer(fn func(*url.URL) string) {
	if fn != nil {
		c.urlSanitizer = fn
	}
}

func defaultURLSanitizer(u *url.URL) string {
	if u == nil {
		return ""
	}
	return u.Redacted()
}

// New returns a Client with sensible defaults.
func New(opts ...Option) *Client {
	c := &Client{
		http:         &http.Client{Timeout: DefaultTimeout},
		userAgent:    version.UserAgent(),
		maxRetries:   DefaultMaxRetries,
		baseDelay:    DefaultBaseDelay,
		sleep:        sleepCtx,
		urlSanitizer: defaultURLSanitizer,
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Do executes req with retry/backoff. The caller is responsible for closing
// the response body on success.
//
// Transient conditions: network errors, 429, 5xx. A Retry-After header (if
// present and parseable) overrides the jittered exponential backoff. On a
// retry, the request body is refreshed via req.GetBody (which net/http sets
// automatically for bytes.Buffer / bytes.Reader / strings.Reader bodies).
// If the request has no GetBody — e.g. a streaming body the caller built
// manually — retries are skipped on POST/PUT/PATCH and we return the last
// error to avoid sending a half-consumed body.
func (c *Client) Do(ctx context.Context, req *http.Request) (*http.Response, error) {
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", c.userAgent)
	}
	req = req.WithContext(ctx)

	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			if req.Body != nil {
				if req.GetBody == nil {
					// Cannot safely re-send a half-consumed body; bail out
					// with the previous error so the wrapped retry-exhaust
					// path still returns through urlSanitizer below.
					break
				}
				body, err := req.GetBody()
				if err != nil {
					return nil, fmt.Errorf("refresh request body for retry: %w", err)
				}
				req.Body = body
			}
			delay := backoff(attempt, c.baseDelay)
			if ra := retryAfter(lastResp(lastErr)); ra > 0 {
				delay = ra
			}
			if err := c.sleep(ctx, delay); err != nil {
				return nil, err
			}
		}

		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = err
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil, err
			}
			continue
		}
		if resp == nil {
			lastErr = errors.New("nil http response")
			continue
		}
		if !isRetryableStatus(resp.StatusCode) {
			return resp, nil
		}
		// retryable status — drain + close and loop.
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		lastErr = &statusError{status: resp.StatusCode, header: resp.Header}
	}
	return nil, fmt.Errorf("%s %s: giving up after %d attempts: %w",
		req.Method, c.urlSanitizer(req.URL), c.maxRetries+1, lastErr)
}

func isRetryableStatus(code int) bool {
	return code == http.StatusTooManyRequests || code >= 500
}

type statusError struct {
	status int
	header http.Header
}

func (e *statusError) Error() string {
	return fmt.Sprintf("http %d %s", e.status, http.StatusText(e.status))
}

func (e *statusError) Status() int         { return e.status }
func (e *statusError) Header() http.Header { return e.header }

// lastResp extracts the most recent http.Header from a retry error, if any.
func lastResp(err error) http.Header {
	if se, ok := errors.AsType[*statusError](err); ok {
		return se.header
	}
	return nil
}

func retryAfter(h http.Header) time.Duration {
	if h == nil {
		return 0
	}
	v := h.Get("Retry-After")
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

// backoff returns base * 2^(attempt-1), jittered by ±25%.
func backoff(attempt int, base time.Duration) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	d := base << (attempt - 1)
	jitter := time.Duration(rand.Int64N(int64(d)/2)) - d/4
	return d + jitter
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
