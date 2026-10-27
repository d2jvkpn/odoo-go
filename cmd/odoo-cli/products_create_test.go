package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProductsCreateUsesConfiguration(t *testing.T) {
	var (
		calls  int
		server *httptest.Server
		config string
		file   string
		out    bytes.Buffer
		err    error
	)

	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		assert.Equal(t, "bearer test-key", r.Header.Get("Authorization"))
		if calls == 1 {
			assert.Equal(t, "/json/2/product.template/create", r.URL.Path)
			fmt.Fprint(w, "[9]")
			return
		}
		assert.Equal(t, "/json/2/product.template/read", r.URL.Path)
		fmt.Fprint(w, `[{"id":9,"product_variant_id":[21,"Tea"]}]`)
	}))
	defer server.Close()

	config = writeFile(t, "local.yaml", configYAML(server.URL, "test-db"))
	file = writeFile(t, "product.json", `{"name":"Tea","list_price":3.5,"default_code":"TEA-1"}`)
	err = runProducts(context.Background(), []string{"create", "-config", config, "-file", file}, &out)
	require.NoError(t, err)
	assert.JSONEq(t, `{"id":21}`, out.String())
	assert.Equal(t, 2, calls)
}

func TestProductsCreateRejectsBadInputWithoutRequests(t *testing.T) {
	var (
		calls  int
		server *httptest.Server
		config string
		inputs map[string]string
		name   string
		body   string
		args   []string
		err    error
	)

	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
	defer server.Close()
	config = writeFile(t, "local.yaml", configYAML(server.URL, ""))

	inputs = map[string]string{
		"malformed":     `{"name":`,
		"unknown field": `{"name":"Tea","price":1}`,
		"trailing":      `{"name":"Tea"} {"name":"Coffee"}`,
		"empty name":    `{"name":""}`,
		"negative":      `{"name":"Tea","list_price":-1}`,
	}
	for name, body = range inputs {
		args = []string{"create", "-config", config, "-file", writeFile(t, "p.json", body)}
		err = runProducts(context.Background(), args, &bytes.Buffer{})
		assert.Error(t, err, name)
	}
	for _, args = range [][]string{
		{"create", "-config", config},
		{"create", "-config", config, "-file", "/nonexistent/product.json"},
		{"create", "-config", config, "-file", "x.json", "extra"},
	} {
		assert.Error(t, runProducts(context.Background(), args, &bytes.Buffer{}), "%v", args)
	}
	assert.Zero(t, calls)
}
