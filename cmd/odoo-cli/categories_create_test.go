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

func TestCategoriesCreateUsesConfiguration(t *testing.T) {
	var (
		calls int
		out   bytes.Buffer
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		assert.Equal(t, "/json/2/product.public.category/create", r.URL.Path)
		fmt.Fprint(w, "[9]")
	}))
	defer server.Close()

	config := writeFile(t, "local.yaml", configYAML(server.URL, "test-db"))
	file := writeFile(t, "category.json", `{"name":"Tea","parent_id":1}`)
	err := runCategories(context.Background(),
		[]string{"create", "-type", "website", "-config", config, "-file", file}, &out)
	require.NoError(t, err)
	assert.JSONEq(t, `{"id":9}`, out.String())
	assert.Equal(t, 1, calls)
}

func TestCategoriesCreateRejectsBadInputWithoutRequests(t *testing.T) {
	var calls int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
	defer server.Close()
	config := writeFile(t, "local.yaml", configYAML(server.URL, ""))

	for name, body := range map[string]string{
		"malformed":     `{"name":`,
		"unknown field": `{"name":"Tea","x":1}`,
		"trailing":      `{"name":"Tea"} {"name":"B"}`,
		"empty name":    `{"name":""}`,
	} {
		args := []string{"create", "-config", config, "-file", writeFile(t, "c.json", body)}
		assert.Error(t, runCategories(context.Background(), args, &bytes.Buffer{}), name)
	}
	for _, args := range [][]string{
		{"create", "-config", config},
		{"create", "-config", config, "-file", "/nonexistent/c.json"},
		{"create", "-config", config, "-type", "bad", "-file", "x.json"},
	} {
		assert.Error(t, runCategories(context.Background(), args, &bytes.Buffer{}), "%v", args)
	}
	assert.Zero(t, calls)
}
