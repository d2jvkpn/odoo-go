package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pagedServer serves search_read pages of two records out of five and search_count, recording requests.
func pagedServer(t *testing.T, requests *[]map[string]any) *httptest.Server {
	t.Helper()

	var server *httptest.Server

	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var params map[string]any

		assert.NoError(t, json.NewDecoder(r.Body).Decode(&params))
		params["_path"] = r.URL.Path
		*requests = append(*requests, params)
		if r.URL.Path[len(r.URL.Path)-len("search_count"):] == "search_count" {
			fmt.Fprint(w, "5")
			return
		}
		switch params["offset"] {
		case float64(0):
			fmt.Fprint(w, `[{"id":1,"name":"a"},{"id":2,"name":"b"}]`)
		case float64(2):
			fmt.Fprint(w, `[{"id":3,"name":"c"},{"id":4,"name":"d"}]`)
		default:
			fmt.Fprint(w, `[{"id":5,"name":"e"}]`)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func runWith(t *testing.T, server *httptest.Server, args ...string) (string, error) {
	t.Helper()

	var (
		out  bytes.Buffer
		path string
		err  error
	)

	path = writeFile(t, "local.yaml", configYAML(server.URL, ""))
	err = run(context.Background(), append([]string{"-config", path}, args...), &out)
	return out.String(), err
}

func TestProductsListOffsetOrderDomain(t *testing.T) {
	var (
		requests []map[string]any
		server   *httptest.Server
		out      string
		err      error
	)

	server = pagedServer(t, &requests)
	out, err = runWith(t, server, "products", "list", "-limit", "2", "-offset", "2", "-order", "name",
		"-domain", `[["categ_id","=",3]]`)
	require.NoError(t, err)
	require.Len(t, requests, 1)
	assert.Equal(t, float64(2), requests[0]["offset"])
	assert.Equal(t, "name", requests[0]["order"])
	assert.Equal(t, []any{[]any{"categ_id", "=", float64(3)}}, requests[0]["domain"])
	assert.NotEmpty(t, out)

	_, err = runWith(t, server, "products", "list", "-domain", "{}")
	assert.ErrorContains(t, err, "domain must be a JSON array")
	_, err = runWith(t, server, "products", "list", "-offset", "-1")
	assert.ErrorContains(t, err, "offset")
}

func TestListAllFetchesEveryPage(t *testing.T) {
	for _, tc := range []struct {
		args []string
		path string
	}{
		{[]string{"products", "list", "-all", "-limit", "2"}, "product.product"},
		{[]string{"orders", "list", "-all", "-limit", "2"}, "sale.order"},
		{[]string{"categories", "list", "-all", "-limit", "2"}, "product.category"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			var (
				requests []map[string]any
				server   *httptest.Server
				out      string
				items    []map[string]any
				err      error
			)

			server = pagedServer(t, &requests)
			out, err = runWith(t, server, tc.args...)
			require.NoError(t, err)
			require.NoError(t, json.Unmarshal([]byte(out), &items))
			assert.Len(t, items, 5)
			assert.Len(t, requests, 3)
		})
	}
}

func TestCountCommands(t *testing.T) {
	for _, tc := range []struct {
		args []string
		path string
	}{
		{[]string{"products", "count"}, "/json/2/product.product/search_count"},
		{[]string{"orders", "count", "-domain", `[["state","=","sale"]]`}, "/json/2/sale.order/search_count"},
		{[]string{"categories", "count", "-parent-id", "0"}, "/json/2/product.category/search_count"},
		{[]string{"categories", "count", "-type", "website"}, "/json/2/product.public.category/search_count"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			var (
				requests []map[string]any
				server   *httptest.Server
				out      string
				err      error
			)

			server = pagedServer(t, &requests)
			out, err = runWith(t, server, tc.args...)
			require.NoError(t, err)
			assert.JSONEq(t, `{"count":5}`, out)
			require.Len(t, requests, 1)
			assert.Equal(t, tc.path, requests[0]["_path"])
			assert.NotContains(t, requests[0], "limit")
		})
	}

	var (
		requests []map[string]any
		err      error
	)

	_, err = runWith(t, pagedServer(t, &requests), "orders", "count", "-domain", "nope")
	assert.ErrorContains(t, err, "domain must be a JSON array")
}
