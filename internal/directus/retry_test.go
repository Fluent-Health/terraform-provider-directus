package directus

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func fastRetryTransport() *retryTransport {
	return &retryTransport{
		base:       http.DefaultTransport,
		maxRetries: 5,
		baseDelay:  time.Millisecond,
		maxDelay:   5 * time.Millisecond,
	}
}

func TestRetryTransport_RetriesThenSucceedsReplayingBody(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if string(b) != `{"name":"x"}` {
			t.Errorf("attempt body = %q", b)
		}
		if atomic.AddInt32(&calls, 1) < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := &http.Client{Transport: fastRetryTransport()}
	resp, err := c.Post(srv.URL, "application/json", strings.NewReader(`{"name":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || atomic.LoadInt32(&calls) != 3 {
		t.Fatalf("status = %d, calls = %d; want 200 after 3", resp.StatusCode, calls)
	}
}

func TestRetryTransport_DoesNotRetry4xx(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	c := &http.Client{Transport: fastRetryTransport()}
	resp, err := c.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestRetryTransport_GivesUp(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	rt := fastRetryTransport()
	resp, err := (&http.Client{Transport: rt}).Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable || atomic.LoadInt32(&calls) != int32(rt.maxRetries+1) {
		t.Fatalf("status = %d, calls = %d", resp.StatusCode, calls)
	}
}

func TestRetryTransport_DelayHonorsRetryAfterCapped(t *testing.T) {
	rt := &retryTransport{baseDelay: time.Second, maxDelay: 10 * time.Second}
	if d := rt.delay(0, "3"); d != 3*time.Second {
		t.Errorf("Retry-After 3 → %v", d)
	}
	if d := rt.delay(0, "600"); d != 10*time.Second {
		t.Errorf("Retry-After 600 → %v, want cap", d)
	}
	if d := rt.delay(2, ""); d != 4*time.Second {
		t.Errorf("attempt 2 backoff → %v", d)
	}
	if d := rt.delay(20, ""); d != 10*time.Second {
		t.Errorf("attempt 20 backoff → %v, want cap", d)
	}
}
