package drupalsec

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/olegiv/it-digest-bot/internal/httpx"
)

type retryTransport func(*http.Request) (*http.Response, error)

func (f retryTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

type retryBody struct {
	io.Reader
	closed bool
}

func (b *retryBody) Close() error {
	b.closed = true
	return nil
}

func TestClientFetchNotFoundRetries(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		statuses []int
		waits    []time.Duration
		wantErr  string
		body     string
	}{
		{name: "immediate success", statuses: []int{200}},
		{name: "one 404", statuses: []int{404, 200}, waits: []time.Duration{10 * time.Second}},
		{name: "two 404s", statuses: []int{404, 404, 200}, waits: []time.Duration{10 * time.Second, 30 * time.Second}},
		{name: "persistent 404", statuses: []int{404, 404, 404}, waits: []time.Duration{10 * time.Second, 30 * time.Second}, wantErr: "http 404 after 3 attempts"},
		{name: "unauthorized", statuses: []int{401}, wantErr: "http 401"},
		{name: "forbidden", statuses: []int{403}, wantErr: "http 403"},
		{name: "404 then forbidden", statuses: []int{404, 403}, waits: []time.Duration{10 * time.Second}, wantErr: "http 403"},
		{name: "gone", statuses: []int{410}, wantErr: "http 410"},
		{name: "malformed feed", statuses: []int{200}, body: "<rss><channel><item>", wantErr: "parse"},
		{name: "404 then malformed feed", statuses: []int{404, 200}, waits: []time.Duration{10 * time.Second}, body: "<rss><channel><item>", wantErr: "parse"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var calls int
			var bodies []*retryBody
			h := httpx.New(httpx.WithHTTPClient(&http.Client{Transport: retryTransport(func(req *http.Request) (*http.Response, error) {
				if calls >= len(tc.statuses) {
					t.Fatal("unexpected extra request")
				}
				if req.Method != http.MethodGet || !strings.Contains(req.Header.Get("Accept"), "application/rss+xml") || !strings.Contains(req.UserAgent(), "it-digest-bot/") {
					t.Errorf("retry lost request method or headers: %+v", req)
				}
				status := tc.statuses[calls]
				calls++
				body := liveFeedXML
				if tc.body != "" {
					body = tc.body
				}
				b := &retryBody{Reader: strings.NewReader(body)}
				bodies = append(bodies, b)
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: b}, nil
			})}))
			c := NewClient(h)
			var logs bytes.Buffer
			c.logger = slog.New(slog.NewJSONHandler(&logs, nil))
			var waits []time.Duration
			c.sleep = func(_ context.Context, delay time.Duration) error {
				for _, b := range bodies {
					if !b.closed {
						t.Error("response body still open during retry wait")
					}
				}
				waits = append(waits, delay)
				return nil
			}
			advisories, warnings, err := c.Fetch(context.Background())
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("Fetch error = %v, want %q", err, tc.wantErr)
				}
				if len(advisories) != 0 {
					t.Errorf("failed fetch returned %d advisories", len(advisories))
				}
			} else if err != nil || len(advisories) != 4 || len(warnings) != 0 {
				t.Fatalf("Fetch returned %d advisories, %v warnings, %v error", len(advisories), warnings, err)
			}
			if calls != len(tc.statuses) || !slices.Equal(waits, tc.waits) {
				t.Errorf("calls/waits = %d/%v, want %d/%v", calls, waits, len(tc.statuses), tc.waits)
			}
			for i, b := range bodies {
				if !b.closed {
					t.Errorf("response %d not closed after fetch", i+1)
				}
			}
			decoder := json.NewDecoder(&logs)
			for i, delay := range tc.waits {
				var entry struct {
					Level   string
					Source  string
					Status  int
					Attempt int
					Delay   string
				}
				if err := decoder.Decode(&entry); err != nil {
					t.Fatalf("retry log %d: %v", i+1, err)
				}
				if entry.Level != "WARN" || entry.Source != PackageKey || entry.Status != 404 || entry.Attempt != i+1 || entry.Delay != delay.String() {
					t.Errorf("retry log %d = %+v", i+1, entry)
				}
			}
			var extra json.RawMessage
			if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
				t.Errorf("unexpected extra retry log: %s, %v", extra, err)
			}
		})
	}
}

func TestClientFetchCancelDuringNotFoundWait(t *testing.T) {
	t.Parallel()
	for _, cancelAt := range []int{1, 2} {
		t.Run(fmt.Sprintf("wait %d", cancelAt), func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls, waits := 0, 0
			h := httpx.New(httpx.WithHTTPClient(&http.Client{Transport: retryTransport(func(*http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader("not found"))}, nil
			})}))
			c := NewClient(h)
			c.sleep = func(ctx context.Context, delay time.Duration) error {
				waits++
				if waits == cancelAt {
					cancel()
					return waitForRetry(ctx, delay)
				}
				return nil
			}
			_, _, err := c.Fetch(ctx)
			if !errors.Is(err, context.Canceled) || calls != cancelAt || waits != cancelAt {
				t.Errorf("calls/waits/error = %d/%d/%v, want %d/%d/context.Canceled", calls, waits, err, cancelAt, cancelAt)
			}
		})
	}
}

func TestWaitForRetry(t *testing.T) {
	t.Parallel()
	if err := waitForRetry(context.Background(), 0); err != nil {
		t.Fatalf("zero delay: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := waitForRetry(ctx, time.Hour); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("interrupted wait: %v, want context.DeadlineExceeded", err)
	}
}

func TestClientFetchKeepsHTTPRetries(t *testing.T) {
	t.Parallel()
	for _, initial404 := range []bool{false, true} {
		t.Run(fmt.Sprintf("initial404=%t", initial404), func(t *testing.T) {
			t.Parallel()
			calls, transientWaits, notFoundWaits := 0, 0, 0
			h := httpx.New(
				httpx.WithSleep(func(context.Context, time.Duration) error {
					transientWaits++
					return nil
				}),
				httpx.WithHTTPClient(&http.Client{Transport: retryTransport(func(*http.Request) (*http.Response, error) {
					calls++
					step := calls
					if initial404 {
						step--
					}
					if step == 1 {
						return nil, errors.New("temporary network failure")
					}
					status := http.StatusOK
					switch step {
					case 0:
						status = http.StatusNotFound
					case 2:
						status = http.StatusTooManyRequests
					case 3:
						status = http.StatusServiceUnavailable
					default:
						if step != 4 {
							t.Fatal("unexpected extra request")
						}
					}
					return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(liveFeedXML))}, nil
				})}),
			)
			c := NewClient(h)
			c.sleep = func(context.Context, time.Duration) error {
				notFoundWaits++
				return nil
			}
			advisories, _, err := c.Fetch(context.Background())
			wantCalls, wantNotFoundWaits := 4, 0
			if initial404 {
				wantCalls, wantNotFoundWaits = 5, 1
			}
			if err != nil || len(advisories) != 4 || calls != wantCalls || transientWaits != 3 || notFoundWaits != wantNotFoundWaits {
				t.Fatalf("advisories/error/calls/transient waits/404 waits = %d/%v/%d/%d/%d", len(advisories), err, calls, transientWaits, notFoundWaits)
			}
		})
	}
}
