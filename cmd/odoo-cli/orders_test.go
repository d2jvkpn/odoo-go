package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOrderCommandsUseConfiguration(t *testing.T) {
	for _, action := range []string{"list", "get", "create"} {
		t.Run(action, func(t *testing.T) {
			var (
				calls  int
				server *httptest.Server
				config string
				args   []string
				file   string
				out    bytes.Buffer
				result any
				err    error
			)

			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var (
					params map[string]any
					want   []any
					err    error
				)

				calls++
				assert.Equal(t, "POST", r.Method)
				assert.Equal(t, "bearer test-key", r.Header.Get("Authorization"))
				assert.Equal(t, "test-db", r.Header.Get("X-Odoo-Database"))

				err = json.NewDecoder(r.Body).Decode(&params)
				assert.NoError(t, err)

				if action == "create" {
					assert.Equal(t, "/json/2/sale.order/create", r.URL.Path)
					assert.NotNil(t, params["vals_list"], "params = %#v", params)
					fmt.Fprint(w, "[42]")
					return
				}

				assert.Equal(t, "/json/2/sale.order/search_read", r.URL.Path)
				if action == "list" {
					want = []any{[]any{"partner_id", "=", float64(7)}}
					assert.Equal(t, want, params["domain"])
					assert.Equal(t, float64(5), params["limit"])
					assert.Equal(t, float64(10), params["offset"])
					assert.Equal(t, "id", params["order"])
				} else {
					want = []any{[]any{"id", "=", float64(42)}}
					assert.Equal(t, want, params["domain"])
				}
				fmt.Fprint(w, `[{"id":42,"name":"S00042","partner_id":[7,"Customer"],"order_line":[]}]`)
			}))
			defer server.Close()

			config = writeFile(t, "local.yaml", configYAML(server.URL, "test-db"))
			args = []string{action, "-config", config}
			switch action {
			case "list":
				args = append(args, "-domain", `[["partner_id","=",7]]`, "-limit", "5", "-offset", "10", "-order", "id")
			case "get":
				args = append(args, "-id", "42")
			case "create":
				file = writeFile(
					t, "order.json",
					`{"partner_id":7,"lines":[{"product_id":3,"product_uom_qty":2}]}`,
				)
				args = append(args, "-file", file)
			}

			err = runOrders(context.Background(), args, &out)
			require.NoError(t, err)

			err = json.Unmarshal(out.Bytes(), &result)
			require.NoError(t, err, "invalid JSON output %s", out.String())

			if action == "list" {
				result = result.([]any)[0]
			}
			assert.Equal(t, float64(42), result.(map[string]any)["id"], "output = %s", out.String())
			assert.Equal(t, 1, calls)
		})
	}
}

func TestOrderCommandInvalidInputDoesNotSendRequests(t *testing.T) {
	var (
		server *httptest.Server
		config string
		file   string
		err    error
	)

	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Fail(t, "invalid input sent a request")
		fmt.Fprint(w, "[42]")
	}))
	defer server.Close()

	config = writeFile(t, "local.yaml", configYAML(server.URL, ""))
	for _, input := range []string{
		"{", "null", "[]",
		`{"partner_id":7,"lines":[{"product_id":3,"product_uom_qty":1}],"unknown":true}`,
		`{"partner_id":7,"lines":[{"product_id":3,"quantity":1}]}`,
		`{"partner_id":7,"lines":[{"product_id":3,"product_uom_qty":1}]} {}`,
		`{"partner_id":0,"lines":[{"product_id":3,"product_uom_qty":1}]}`,
	} {
		file = writeFile(t, "order.json", input)
		err = runOrders(context.Background(), []string{"create", "-config", config, "-file", file}, io.Discard)
		assert.Error(t, err, "accepted %s", input)
	}

	for _, args := range [][]string{
		{"create"}, {"create", "-file", "missing.json"},
		{"get"}, {"get", "-id", "-1"},
		{"list", "-limit", "-1"}, {"list", "-limit", "0"}, {"list", "-offset", "-1"},
		{"list", "-domain", "{}"}, {"list", "-domain", "null"}, {"list", "-domain", "["},
		{"list", "unexpected"}, {"list", "-unknown"},
	} {
		args = append(args, "-config", config)
		err = runOrders(context.Background(), args, io.Discard)
		assert.Error(t, err, "accepted %v", args)
	}
}

func TestOrderCommandReportsErrors(t *testing.T) {
	var (
		server *httptest.Server
		config string
		out    bytes.Buffer
		err    error
	)

	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		fmt.Fprint(w, "Access denied")
	}))
	defer server.Close()

	config = writeFile(t, "local.yaml", configYAML(server.URL, ""))
	err = runOrders(context.Background(), []string{"list", "-config", config}, &out)
	assert.ErrorContains(t, err, "403")
	assert.Zero(t, out.Len(), "unexpected output: %s", out.String())

	err = runOrders(context.Background(), []string{"list", "-config", "missing.yaml"}, io.Discard)
	assert.Error(t, err, "accepted missing config")

	err = runOrders(context.Background(), []string{"unknown"}, io.Discard)
	assert.Error(t, err, "accepted unknown command")
}

func TestOrderCommandHelp(t *testing.T) {
	for _, args := range [][]string{nil, {"-h"}, {"--help"}, {"list", "-h"}, {"get", "-h"}, {"create", "-h"}} {
		var (
			out bytes.Buffer
			err error
		)

		err = runOrders(context.Background(), args, &out)
		require.NoError(t, err)
		assert.NotZero(t, out.Len(), "no help for %v", args)
	}
}
