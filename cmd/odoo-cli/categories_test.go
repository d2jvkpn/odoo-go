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

func TestCategoryCommandQueries(t *testing.T) {
	for _, tc := range []struct {
		name   string
		args   []string
		model  string
		domain string
		limit  float64
	}{
		{"list", []string{"list"}, "product.category", "[]", 1000},
		{
			"roots",
			[]string{"list", "-parent-id", "0", "-limit", "5", "-offset", "10", "-order", "name"},
			"product.category", `[["parent_id","=",false]]`, 5,
		},
		{
			"children",
			[]string{"list", "-type", "website", "-parent-id", "7", "-domain", `[["name","ilike","Desk"]]`},
			"product.public.category", `[["name","ilike","Desk"],["parent_id","=",7]]`, 100,
		},
		{
			"get",
			[]string{"get", "-type", "website", "-id", "2"},
			"product.public.category", `[["id","=",2]]`, 1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var (
				server *httptest.Server
				args   []string
				out    bytes.Buffer
				result any
				item   map[string]any
				err    error
			)

			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var (
					params map[string]any
					domain any
					err    error
				)

				assert.Equal(t, "/json/2/"+tc.model+"/search_read", r.URL.Path)
				assert.Equal(t, "bearer test-key", r.Header.Get("Authorization"))
				assert.Equal(t, "test-db", r.Header.Get("X-Odoo-Database"))

				err = json.NewDecoder(r.Body).Decode(&params)
				assert.NoError(t, err)

				err = json.Unmarshal([]byte(tc.domain), &domain)
				assert.NoError(t, err)
				assert.Equal(t, domain, params["domain"])
				assert.Equal(t, tc.limit, params["limit"])

				if tc.name == "roots" {
					assert.Equal(t, float64(10), params["offset"])
					assert.Equal(t, "name", params["order"])
				}

				if tc.model == "product.category" {
					assert.Equal(t, []any{"id", "name", "complete_name", "parent_id", "child_id", "parent_path"}, params["fields"])
					fmt.Fprint(w, `[{"id":2,"name":"Desks","complete_name":"Furniture / Desks","parent_id":false}]`)
				} else {
					fmt.Fprint(w, `[{"id":2,"name":"Desks","display_name":"Desks","parent_id":false,"child_id":[]}]`)
				}
			}))
			defer server.Close()

			args = append(tc.args, "-config", writeFile(t, "local.yaml", configYAML(server.URL, "test-db")))
			err = runCategories(context.Background(), args, &out)
			require.NoError(t, err)

			err = json.Unmarshal(out.Bytes(), &result)
			require.NoError(t, err)

			if tc.name != "get" {
				result = result.([]any)[0]
			}
			item = result.(map[string]any)
			require.Equal(t, float64(2), item["id"], "output = %s", out.String())

			if tc.model == "product.category" {
				assert.Equal(t, "Furniture / Desks", item["complete_name"], "output = %s", out.String())
			}
		})
	}
}

func TestCategoryCommandInvalidInput(t *testing.T) {
	var (
		server *httptest.Server
		config string
		err    error
	)

	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Fail(t, "invalid input reached server")
		fmt.Fprint(w, "[]")
	}))
	defer server.Close()

	config = writeFile(t, "local.yaml", configYAML(server.URL, "test-db"))
	for _, args := range [][]string{
		{"get"}, {"get", "-id", "0"}, {"get", "-id", "-1"},
		{"list", "-type", "unknown"}, {"list", "-parent-id", "-2"}, {"list", "-parent-id", "-1"},
		{"list", "-limit", "0"}, {"list", "-offset", "-1"},
		{"list", "-domain", "null"}, {"list", "-domain", "{}"}, {"list", "-domain", "["},
		{"create"}, {"list", "extra"}, {"list", "-unknown"},
	} {
		args = append(args, "-config", config)
		err = runCategories(context.Background(), args, io.Discard)
		assert.Error(t, err, "accepted %v", args)
	}
}

func TestCategoryCommandErrorsAndHelp(t *testing.T) {
	var (
		server *httptest.Server
		config string
		out    bytes.Buffer
		err    error
	)

	for _, args := range [][]string{nil, {"-h"}, {"--help"}, {"list", "-h"}, {"get", "-h"}} {
		out.Reset()
		err = runCategories(context.Background(), args, &out)
		assert.NoError(t, err, "help %v", args)
		assert.NotZero(t, out.Len(), "no help for %v", args)
	}

	err = runCategories(context.Background(), []string{"list", "-config", "missing.yaml"}, io.Discard)
	assert.Error(t, err, "expected missing config error")

	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		fmt.Fprint(w, "Access denied")
	}))
	defer server.Close()

	out.Reset()
	config = writeFile(t, "local.yaml", configYAML(server.URL, "test-db"))
	err = runCategories(context.Background(), []string{"list", "-config", config}, &out)
	assert.ErrorContains(t, err, "403")
	assert.Zero(t, out.Len(), "output = %s", out.String())
}
