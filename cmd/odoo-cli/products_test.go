package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/d2jvkpn/odoo-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunPrintsProductsFromConfiguredServer(t *testing.T) {
	for _, body := range []string{
		`[{"id":7,"name":"Desk","default_code":"D-1","list_price":100,"categ_id":{"id":3,"name":"Furniture"},` +
			`"sale_ok":true,"is_storable":true}]`,
		`[]`,
	} {
		t.Run(body, func(t *testing.T) {
			var (
				server *httptest.Server
				path   string
				out    bytes.Buffer
				err    error
			)

			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var (
					params map[string]any
					want   map[string]any
					err    error
				)

				assert.Equal(t, "POST", r.Method)
				assert.Equal(t, "/json/2/product.product/search_read", r.URL.Path)
				assert.Equal(t, "bearer test-key", r.Header.Get("Authorization"))
				assert.Equal(t, "test-db", r.Header.Get("X-Odoo-Database"))

				err = json.NewDecoder(r.Body).Decode(&params)
				assert.NoError(t, err)

				want = map[string]any{
					"domain": []any{[]any{"sale_ok", "=", true}},
					"fields": []any{"id", "name", "default_code", "list_price", "categ_id", "sale_ok", "is_storable"},
					"limit":  float64(3),
					"offset": float64(0),
					"order":  "id",
				}
				assert.Equal(t, want, params)

				if body == `[]` {
					fmt.Fprint(w, body)
					return
				}
				fmt.Fprint(w, `[{"id":7,"name":"Desk","default_code":"D-1","list_price":100,"categ_id":[3,"Furniture"],`+
					`"sale_ok":true,"is_storable":true}]`)
			}))
			defer server.Close()

			path = writeFile(t, "local.yaml", configYAML(server.URL, "test-db"))
			err = listProducts(context.Background(), path, saleableOptions(3), &out)
			require.NoError(t, err)
			assert.JSONEq(t, body, out.String())
		})
	}
}

func TestRunRejectsInvalidConfig(t *testing.T) {
	for _, config := range []string{
		"odoo: [",
		"odoo:\n  base_url: http://127.0.0.1:1\n",
		"odoo:\n  api_key: secret-key\n",
		"odoo:\n  base_url: invalid\n  api_key: secret-key\n",
	} {
		var (
			out bytes.Buffer
			err error
		)

		err = listProducts(context.Background(), writeFile(t, "local.yaml", config), saleableOptions(3), &out)
		require.Error(t, err, "expected configuration error")
		assert.NotContains(t, err.Error(), "secret-key", "error exposed API key")
		assert.Zero(t, out.Len(), "unexpected output on failure")
	}
}

func TestRunRejectsMissingConfigAndInvalidLimit(t *testing.T) {
	var (
		path string
		err  error
	)

	path = filepath.Join(t.TempDir(), "missing.yaml")
	err = listProducts(context.Background(), path, saleableOptions(3), io.Discard)
	assert.Error(t, err, "expected missing file error")

	path = writeFile(t, "local.yaml", "odoo:\n  base_url: http://127.0.0.1:1\n  api_key: test-key\n")
	for _, limit := range []int{0, -1} {
		err = listProducts(context.Background(), path, saleableOptions(limit), io.Discard)
		assert.ErrorContains(t, err, "limit")
	}
}

func TestRunPropagatesOdooFailure(t *testing.T) {
	var (
		server *httptest.Server
		path   string
		out    bytes.Buffer
		err    error
	)

	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		fmt.Fprint(w, "Access Denied")
	}))
	defer server.Close()

	path = writeFile(t, "local.yaml", configYAML(server.URL, ""))
	err = listProducts(context.Background(), path, saleableOptions(3), &out)
	assert.ErrorContains(t, err, "401")
	assert.Zero(t, out.Len(), "unexpected output on failure")
}

func TestRunProductsGet(t *testing.T) {
	var (
		server *httptest.Server
		path   string
		out    bytes.Buffer
		err    error
	)

	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var params map[string]any

		assert.Equal(t, "/json/2/product.product/search_read", r.URL.Path)
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&params))
		assert.Equal(t, []any{[]any{"id", "=", float64(7)}}, params["domain"])
		fmt.Fprint(w, `[{"id":7,"name":"Desk","default_code":false,"list_price":100,"categ_id":[3,"Furniture"],`+
			`"sale_ok":false,"is_storable":true}]`)
	}))
	defer server.Close()

	path = writeFile(t, "local.yaml", configYAML(server.URL, "test-db"))
	err = run(context.Background(), []string{"-config", path, "products", "get", "-id", "7"}, &out)
	require.NoError(t, err)
	assert.JSONEq(
		t,
		`{"id":7,"name":"Desk","default_code":"","list_price":100,"categ_id":{"id":3,"name":"Furniture"},`+
			`"sale_ok":false,"is_storable":true}`,
		out.String(),
	)
}

func TestRunProductsGetRejectsInvalidID(t *testing.T) {
	var err error

	err = getProduct(context.Background(), "unused.yaml", 0, io.Discard)
	assert.ErrorContains(t, err, "id must be greater than zero")
}

// saleableOptions lists saleable products with the given page size.
func saleableOptions(limit int) productListOptions {
	return productListOptions{Query: odoo.ProductQuery{
		Domain: []any{[]any{"sale_ok", "=", true}},
		Limit:  limit,
	}}
}
