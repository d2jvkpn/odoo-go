package odoo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCallRequest(t *testing.T) {
	var (
		server   *httptest.Server
		client   *Client
		err      error
		products []struct {
			ID   int
			Name string
		}
	)

	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var (
			params struct {
				Fields []string
				Limit  int
			}
			err error
		)

		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/odoo/json/2/product.product/search_read", r.URL.Path)
		assert.Equal(t, "bearer test-key", r.Header.Get("Authorization"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Equal(t, "test-db", r.Header.Get("X-Odoo-Database"))

		err = json.NewDecoder(r.Body).Decode(&params)
		assert.NoError(t, err)
		assert.Equal(t, 100, params.Limit)
		assert.Equal(t, []string{"id", "name"}, params.Fields)
		fmt.Fprint(w, `[{"id":7,"name":"Product"}]`)
	}))
	defer server.Close()

	client = NewClient(server.URL+"/odoo///", "test-key", "test-db")
	err = client.Call(context.Background(), "product.product", "search_read",
		map[string]any{"fields": []string{"id", "name"}, "limit": 100}, &products)
	require.NoError(t, err)
	require.Len(t, products, 1)
	assert.Equal(t, 7, products[0].ID)
	assert.Equal(t, "Product", products[0].Name)
}

func TestCallWithoutParameters(t *testing.T) {
	var (
		server *httptest.Server
		result int
		err    error
	)

	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body []byte

		body, _ = io.ReadAll(r.Body)
		assert.Equal(t, "{}", string(body))
		assert.Empty(t, r.Header.Get("X-Odoo-Database"), "unexpected database header")
		fmt.Fprint(w, "42")
	}))
	defer server.Close()

	err = NewClient(server.URL, "key", "").Call(context.Background(), "res.partner", "search_count", nil, &result)
	require.NoError(t, err)
	assert.Equal(t, 42, result)
}

func TestCallIgnoredResult(t *testing.T) {
	for _, body := range []string{"true", ""} {
		t.Run(body, func(t *testing.T) {
			var (
				server *httptest.Server
				err    error
			)

			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, body)
			}))
			defer server.Close()

			err = NewClient(server.URL, "key", "").Call(
				context.Background(), "res.partner", "write", map[string]any{}, nil,
			)
			require.NoError(t, err)
		})
	}
}

func TestCallResponseErrors(t *testing.T) {
	for _, tc := range []struct {
		name       string
		status     int
		body, want string
	}{
		{"unauthorized", 401, `{"name":"odoo.exceptions.AccessDenied","message":"Access Denied"}`, "Access Denied"},
		{"proxy failure", 502, "bad gateway", "bad gateway"},
		{"invalid JSON", 200, "invalid", "decode"},
		{"missing result", 200, "", "decode"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var (
				server *httptest.Server
				result any
				err    error
			)

			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()

			err = NewClient(server.URL, "key", "").Call(context.Background(), "res.partner", "read", nil, &result)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
			if tc.status >= 400 {
				assert.Contains(t, err.Error(), fmt.Sprint(tc.status), "missing status")
			}
		})
	}
}

func TestCallBoundsErrorBody(t *testing.T) {
	var (
		server *httptest.Server
		err    error
	)

	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		fmt.Fprint(w, strings.Repeat("x", 8192)+"END")
	}))
	defer server.Close()

	err = NewClient(server.URL, "key", "").Call(context.Background(), "res.partner", "read", nil, nil)
	require.Error(t, err)
	assert.LessOrEqual(t, len(err.Error()), 4200)
	assert.NotContains(t, err.Error(), "END")
}

func TestCallCanceledContext(t *testing.T) {
	var (
		ctx    context.Context
		cancel context.CancelFunc
		err    error
	)

	ctx, cancel = context.WithCancel(context.Background())
	cancel()

	err = NewClient("http://127.0.0.1:1", "key", "").Call(ctx, "res.partner", "read", nil, nil)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestCallInvalidInput(t *testing.T) {
	for _, tc := range []struct {
		name, baseURL string
		params        any
	}{
		{"invalid URL", "://invalid", nil},
		{"unserializable parameters", "http://127.0.0.1:1", make(chan int)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var err error

			err = NewClient(tc.baseURL, "key", "").Call(context.Background(), "res.partner", "read", tc.params, nil)
			assert.Error(t, err)
		})
	}
}

func TestCallNilHTTPClient(t *testing.T) {
	var (
		server *httptest.Server
		client *Client
		result bool
		err    error
	)

	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "true")
	}))
	defer server.Close()

	client = NewClient(server.URL, "key", "")
	client.HTTP = nil

	err = client.Call(context.Background(), "res.partner", "write", nil, &result)
	require.NoError(t, err)
	assert.True(t, result)
}

func TestCallRequiresParameterObject(t *testing.T) {
	var (
		server *httptest.Server
		err    error
	)

	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Fail(t, "invalid parameters reached server")
		fmt.Fprint(w, "true")
	}))
	defer server.Close()

	for _, params := range []any{[]int{1}, "value", 42, true} {
		err = NewClient(server.URL, "key", "").Call(context.Background(), "res.partner", "read", params, nil)
		assert.Error(t, err, "accepted invalid parameters: %#v", params)
	}
}
