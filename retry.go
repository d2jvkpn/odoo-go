package odoo

import (
	"context"
	"errors"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	defaultBackoff    = 200 * time.Millisecond
	defaultMaxBackoff = 5 * time.Second
	jitterFraction    = 0.2
)

// RetryPolicy configures retries of read-only calls. MaxRetries is the number of additional
// attempts after the first; zero disables retries. The wait before retry n (starting at 0) is
// Backoff * 2^n, capped at MaxBackoff. Zero Backoff and MaxBackoff default to 200ms and 5s.
//
// Unless NoJitter is set, each wait is reduced by a random 0-20% so that concurrent clients do not
// retry in lockstep. A Retry-After header on the response (429, 503) raises the wait to the
// requested delay, still capped at MaxBackoff so a long request cannot stall the caller.
type RetryPolicy struct {
	MaxRetries int
	Backoff    time.Duration
	MaxBackoff time.Duration
	NoJitter   bool
}

// wait returns the sleep before the next retry, given the failed attempt and its error.
func (p RetryPolicy) wait(attempt int, err error) time.Duration {
	var (
		d, max  time.Duration
		httpErr *HTTPError
	)

	d, max = p.delay(attempt), p.MaxBackoff
	if max <= 0 {
		max = defaultMaxBackoff
	}
	if !p.NoJitter {
		d -= time.Duration(rand.Float64() * jitterFraction * float64(d))
	}
	if errors.As(err, &httpErr) && httpErr.RetryAfter > d {
		d = min(httpErr.RetryAfter, max)
	}
	return d
}

// delay returns the wait before the retry that follows the given zero-based failed attempt.
func (p RetryPolicy) delay(attempt int) time.Duration {
	var (
		d, max time.Duration
		i      int
	)

	d, max = p.Backoff, p.MaxBackoff
	if d <= 0 {
		d = defaultBackoff
	}
	if max <= 0 {
		max = defaultMaxBackoff
	}
	for i = 0; i < attempt && d < max; i++ {
		d *= 2
	}
	return min(d, max)
}

// transportError marks a failure to complete the HTTP exchange (connection refused or reset,
// timeout, DNS). The original error stays reachable through errors.Is / errors.As.
type transportError struct {
	Err error
}

func (e *transportError) Error() string { return "send request: " + e.Err.Error() }

func (e *transportError) Unwrap() error { return e.Err }

// retryable reports whether err is transient: a transport failure, or HTTP 429, 502, 503 or 504.
func retryable(err error) bool {
	var (
		transport *transportError
		httpErr   *HTTPError
	)

	if errors.As(err, &transport) {
		return true
	}
	if errors.As(err, &httpErr) {
		switch httpErr.StatusCode {
		case http.StatusTooManyRequests, http.StatusBadGateway,
			http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			return true
		}
	}
	return false
}

// sleepContext waits for d or until ctx is done, returning ctx's error in the latter case.
func sleepContext(ctx context.Context, d time.Duration) error {
	var timer *time.Timer

	timer = time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// parseRetryAfter reads a Retry-After value, either delay-seconds or an HTTP date. Invalid, empty
// and past values yield zero.
func parseRetryAfter(value string, now time.Time) time.Duration {
	var (
		seconds int
		when    time.Time
		err     error
	)

	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err = strconv.Atoi(value); err == nil {
		return time.Duration(max(seconds, 0)) * time.Second
	}
	if when, err = http.ParseTime(value); err == nil {
		return max(when.Sub(now), 0)
	}
	return 0
}
