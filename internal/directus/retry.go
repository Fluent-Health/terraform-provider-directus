package directus

import (
	"io"
	"net/http"
	"strconv"
	"time"
)

const (
	defaultMaxRetries = 8
	defaultBaseDelay  = 500 * time.Millisecond
	defaultMaxDelay   = 30 * time.Second
)

// retryTransport retries transient responses: HTTP 429 (Directus' rate
// limiter, RATE_LIMITER_ENABLED) and the 502/503/504 a load balancer returns
// while a pod rolls. A Terraform apply fans many operations out concurrently,
// so the provider rides out a throttle rather than failing the apply on the
// first 429. Retry-After is honored (capped); otherwise the wait doubles up to
// a cap.
type retryTransport struct {
	base       http.RoundTripper
	maxRetries int
	baseDelay  time.Duration
	maxDelay   time.Duration
}

func newRetryTransport(base http.RoundTripper) *retryTransport {
	return &retryTransport{
		base:       base,
		maxRetries: defaultMaxRetries,
		baseDelay:  defaultBaseDelay,
		maxDelay:   defaultMaxDelay,
	}
}

func (t *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	for attempt := 0; ; attempt++ {
		// Each retry runs on a fresh clone with the body replayed from
		// GetBody (set by net/http for the *bytes.Reader bodies this package
		// builds). A body without GetBody cannot be replayed: send it once.
		attemptReq := req
		if attempt > 0 {
			attemptReq = req.Clone(req.Context())
			if req.Body != nil {
				if req.GetBody == nil {
					return t.base.RoundTrip(req)
				}
				body, err := req.GetBody()
				if err != nil {
					return nil, err
				}
				attemptReq.Body = body
			}
		}

		resp, err := t.base.RoundTrip(attemptReq)
		if err != nil {
			return nil, err
		}
		if attempt >= t.maxRetries || !retryableStatus(resp.StatusCode) {
			return resp, nil
		}

		delay := t.delay(attempt, resp.Header.Get("Retry-After"))

		// Drain and close so the connection can be reused before we wait.
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()

		select {
		case <-req.Context().Done():
			return nil, req.Context().Err()
		case <-time.After(delay):
		}
	}
}

func retryableStatus(code int) bool {
	switch code {
	case http.StatusTooManyRequests, // 429
		http.StatusBadGateway,         // 502
		http.StatusServiceUnavailable, // 503
		http.StatusGatewayTimeout:     // 504
		return true
	default:
		return false
	}
}

// delay returns the wait before the next attempt: a valid Retry-After
// (delta-seconds or HTTP-date) capped at maxDelay, else baseDelay*2^attempt
// capped at maxDelay.
func (t *retryTransport) delay(attempt int, retryAfter string) time.Duration {
	if d, ok := parseRetryAfter(retryAfter); ok {
		switch {
		case d < 0:
			return 0
		case d > t.maxDelay:
			return t.maxDelay
		default:
			return d
		}
	}
	d := t.baseDelay << attempt
	if d <= 0 || d > t.maxDelay {
		return t.maxDelay
	}
	return d
}

func parseRetryAfter(v string) (time.Duration, bool) {
	if v == "" {
		return 0, false
	}
	if secs, err := strconv.Atoi(v); err == nil {
		return time.Duration(secs) * time.Second, true
	}
	if when, err := http.ParseTime(v); err == nil {
		return time.Until(when), true
	}
	return 0, false
}
