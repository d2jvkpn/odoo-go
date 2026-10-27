package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeFile writes content to name under a fresh temporary directory and returns its path.
func writeFile(t *testing.T, name, content string) string {
	t.Helper()

	var (
		path string
		err  error
	)

	path = filepath.Join(t.TempDir(), name)
	err = os.WriteFile(path, []byte(content), 0600)
	require.NoError(t, err)

	return path
}

// configYAML renders a configuration file pointing at url, with an optional database.
func configYAML(url, db string) string {
	var data string

	data = fmt.Sprintf("odoo:\n  base_url: %q\n  api_key: test-key\n", url)
	if db != "" {
		data += "  db: " + db + "\n"
	}

	return data
}

func TestUnifiedSubcommands(t *testing.T) {
	for _, tc := range []struct {
		args []string
		path string
	}{
		{[]string{"products", "list", "-limit", "2"}, "/json/2/product.product/search_read"},
		{[]string{"orders", "list", "-limit", "2"}, "/json/2/sale.order/search_read"},
		{[]string{"orders", "get", "-id", "42"}, "/json/2/sale.order/search_read"},
		{[]string{"orders", "create"}, "/json/2/sale.order/create"},
		{[]string{"categories", "list", "-limit", "2"}, "/json/2/product.category/search_read"},
		{[]string{"categories", "get", "-id", "42"}, "/json/2/product.category/search_read"},
		{
			[]string{"categories", "list", "-type", "website", "-limit", "2"},
			"/json/2/product.public.category/search_read",
		},
	} {
		for _, configMode := range []string{"action", "global", "override"} {
			t.Run(strings.Join(tc.args, "_")+configMode, func(t *testing.T) {
				var (
					calls   int
					server  *httptest.Server
					config  string
					input   string
					missing string
					args    []string
					out     bytes.Buffer
					err     error
				)

				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					var (
						params map[string]any
						err    error
					)

					calls++
					assert.Equal(t, tc.path, r.URL.Path)
					assert.Equal(t, "bearer test-key", r.Header.Get("Authorization"))

					err = json.NewDecoder(r.Body).Decode(&params)
					assert.NoError(t, err)

					if tc.args[1] == "list" {
						assert.Equal(t, float64(2), params["limit"])
					}

					if tc.args[1] == "create" {
						assert.NotNil(t, params["vals_list"], "missing create payload")
						fmt.Fprint(w, "[42]")
					} else {
						fmt.Fprint(w, `[{"id":42,"order_line":[]}]`)
					}
				}))
				defer server.Close()

				config = writeFile(t, "local.yaml", configYAML(server.URL, ""))
				args = append([]string(nil), tc.args...)
				if tc.args[1] == "create" {
					input = writeFile(
						t, "order.json",
						`{"partner_id":7,"lines":[{"product_id":3,"product_uom_qty":1}]}`,
					)
					args = append(args, "-file", input)
				}

				if configMode == "global" {
					args = append([]string{"-config", config}, args...)
				} else {
					args = append(args, "-config", config)
					if configMode == "override" {
						missing = filepath.Join(t.TempDir(), "missing.yaml")
						args = append([]string{"-config", missing}, args...)
					}
				}

				err = run(context.Background(), args, &out)
				require.NoError(t, err)
				assert.Equal(t, 1, calls)
				assert.True(t, json.Valid(out.Bytes()), "output = %s", out.String())
				assert.Contains(t, out.String(), "42")
			})
		}
	}
}

func TestUnifiedHelp(t *testing.T) {
	for _, args := range [][]string{
		nil, {"-h"}, {"--help"},
		{"products"}, {"products", "-h"}, {"products", "list", "-h"},
		{"orders"}, {"orders", "get", "-h"},
		{"categories"}, {"categories", "list", "-h"},
	} {
		var (
			out bytes.Buffer
			err error
		)

		err = run(context.Background(), args, &out)
		assert.NoError(t, err, "%v", args)
		assert.NotZero(t, out.Len(), "no help for %v", args)
	}
}

func TestUnifiedInvalidCommands(t *testing.T) {
	for _, args := range [][]string{
		{"unknown"}, {"-unknown"}, {"products", "create"},
		{"products", "list", "extra"}, {"products", "list", "-unknown"},
		{"products", "list", "-limit", "0"},
		{"orders", "unknown"}, {"categories", "create"},
	} {
		assert.Error(t, run(context.Background(), args, io.Discard), "accepted %v", args)
	}
}
