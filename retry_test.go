package odoo

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func retryClient(t *testing.T, retries int, handler http.HandlerFunc) *Client {
	t.Helper()

	var (
		server *httptest.Server
		client *Client
	)

	server = httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client = NewClient(server.URL, "key", "")
	client.Retry = RetryPolicy{MaxRetries: retries, Backoff: time.Millisecond, MaxBackoff: 2 * time.Millisecond}
	return client
}

func TestCallReadRetriesTransientStatus(t *testing.T) {
	var (
		calls  atomic.Int32
		client *Client
		items  []int
		err    error
	)

	client = retryClient(t, 3, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		fmt.Fprint(w, "[1]")
	})
	err = client.CallRead(context.Background(), "m", "search_read", nil, &items)
	require.NoError(t, err)
	assert.Equal(t, []int{1}, items)
	assert.EqualValues(t, 3, calls.Load())
}

func TestCallReadRetriesTransportError(t *testing.T) {
	var (
		calls  atomic.Int32
		client *Client
		err    error
	)

	client = retryClient(t, 2, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		hijack(t, w)
	})
	err = client.CallRead(context.Background(), "m", "search_read", nil, nil)
	require.Error(t, err)
	assert.EqualValues(t, 3, calls.Load())
	assert.True(t, retryable(err))
}

func hijack(t *testing.T, w http.ResponseWriter) {
	t.Helper()

	var (
		conn net.Conn
		err  error
	)

	conn, _, err = w.(http.Hijacker).Hijack()
	require.NoError(t, err)
	conn.Close()
}

func TestCallReadDoesNotRetryPermanentErrors(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusInternalServerError} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var (
				calls  atomic.Int32
				client *Client
				err    error
			)

			client = retryClient(t, 3, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(status)
			})
			err = client.CallRead(context.Background(), "m", "search_read", nil, nil)
			require.Error(t, err)
			assert.EqualValues(t, 1, calls.Load())
		})
	}
}

func TestCallReadExhaustsRetriesAndReturnsLastError(t *testing.T) {
	var (
		calls   atomic.Int32
		client  *Client
		err     error
		httpErr *HTTPError
	)

	client = retryClient(t, 2, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
	})
	err = client.CallRead(context.Background(), "m", "search_read", nil, nil)
	require.ErrorAs(t, err, &httpErr)
	assert.Equal(t, http.StatusTooManyRequests, httpErr.StatusCode)
	assert.EqualValues(t, 3, calls.Load())
}

func TestCallNeverRetries(t *testing.T) {
	var (
		calls  atomic.Int32
		client *Client
		err    error
	)

	client = retryClient(t, 3, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	err = client.Call(context.Background(), "m", "create", nil, nil)
	require.Error(t, err)
	_, err = client.CreateSaleOrder(context.Background(), CreateSaleOrderRequest{
		PartnerID: 1, Lines: []CreateSaleOrderLine{{ProductID: 1, Quantity: 1}},
	})
	require.Error(t, err)
	assert.EqualValues(t, 2, calls.Load())
}

func TestSearchRetriesButStopsOnContextCancel(t *testing.T) {
	var (
		calls  atomic.Int32
		client *Client
		ctx    context.Context
		cancel context.CancelFunc
		err    error
	)

	client = retryClient(t, 5, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	})
	client.Retry.Backoff = time.Hour
	client.Retry.MaxBackoff = time.Hour
	ctx, cancel = context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err = client.SearchProducts(ctx, ProductQuery{})
	require.Error(t, err)
	assert.EqualValues(t, 1, calls.Load())
}

func TestRetryPolicyDelay(t *testing.T) {
	var p RetryPolicy

	assert.Equal(t, 200*time.Millisecond, p.delay(0))
	assert.Equal(t, 400*time.Millisecond, p.delay(1))
	assert.Equal(t, 5*time.Second, p.delay(10))
	p = RetryPolicy{Backoff: time.Second, MaxBackoff: 3 * time.Second}
	assert.Equal(t, 2*time.Second, p.delay(1))
	assert.Equal(t, 3*time.Second, p.delay(2))
}

func TestLoadClientMaxRetries(t *testing.T) {
	var (
		path   string
		client *Client
		err    error
	)

	path = filepath.Join(t.TempDir(), "local.yaml")
	require.NoError(t, os.WriteFile(path, []byte(retryConfig(3)), 0o600))
	client, err = LoadClient(path)
	require.NoError(t, err)
	assert.Equal(t, 3, client.Retry.MaxRetries)

	require.NoError(t, os.WriteFile(path, []byte(retryConfig(11)), 0o600))
	_, err = LoadClient(path)
	assert.ErrorIs(t, err, ErrInvalidConfig)
}

func retryConfig(retries int) string {
	return fmt.Sprintf("odoo:\n  base_url: http://x.test\n  api_key: k\n  max_retries: %d\n", retries)
}

func TestParseRetryAfter(t *testing.T) {
	var now time.Time

	now = time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	assert.Equal(t, 3*time.Second, parseRetryAfter("3", now))
	assert.Equal(t, 30*time.Second, parseRetryAfter(now.Add(30*time.Second).Format(http.TimeFormat), now))
	assert.Zero(t, parseRetryAfter(now.Add(-time.Minute).Format(http.TimeFormat), now))
	assert.Zero(t, parseRetryAfter("-5", now))
	assert.Zero(t, parseRetryAfter("soon", now))
	assert.Zero(t, parseRetryAfter("", now))
}

func TestRetryPolicyWaitJitterAndRetryAfter(t *testing.T) {
	var (
		p   RetryPolicy
		err error
		d   time.Duration
		i   int
	)

	p = RetryPolicy{Backoff: time.Second, MaxBackoff: 10 * time.Second}
	for i = 0; i < 50; i++ {
		d = p.wait(0, nil)
		assert.GreaterOrEqual(t, d, 800*time.Millisecond)
		assert.LessOrEqual(t, d, time.Second)
	}
	p.NoJitter = true
	assert.Equal(t, time.Second, p.wait(0, nil))

	err = &HTTPError{StatusCode: 429, RetryAfter: 4 * time.Second}
	assert.Equal(t, 4*time.Second, p.wait(0, err))
	err = &HTTPError{StatusCode: 429, RetryAfter: time.Hour}
	assert.Equal(t, 10*time.Second, p.wait(0, err))
	err = &HTTPError{StatusCode: 429, RetryAfter: time.Millisecond}
	assert.Equal(t, time.Second, p.wait(0, err))
}

func TestCallRecordsRetryAfterHeader(t *testing.T) {
	var (
		client  *Client
		err     error
		httpErr *HTTPError
	)

	client = retryClient(t, 0, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	err = client.Call(context.Background(), "m", "n", nil, nil)
	require.ErrorAs(t, err, &httpErr)
	assert.Equal(t, 7*time.Second, httpErr.RetryAfter)
}
