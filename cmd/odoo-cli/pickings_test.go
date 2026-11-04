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

func TestPickingsValidateNeedsYes(t *testing.T) {
	var calls int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
	defer server.Close()
	config := writeFile(t, "local.yaml", configYAML(server.URL, ""))

	var out bytes.Buffer
	require.NoError(t, runPickings(context.Background(), []string{"validate", "-config", config, "-id", "21"}, &out))
	assert.JSONEq(t, `{"action":"validate","id":21,"executed":false}`, out.String())
	assert.Zero(t, calls)
}

func TestPickingsValidateRunsWithYes(t *testing.T) {
	var validated int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/json/2/stock.picking/read":
			fmt.Fprint(w, `[{"id":21,"state":"assigned","name":"WH/OUT/1","scheduled_date":"2026-11-04 10:00:00"}]`)
		case "/json/2/stock.picking/button_validate":
			validated++
			fmt.Fprint(w, "true")
		case "/json/2/stock.move/search_read":
			fmt.Fprint(w, "[]")
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	config := writeFile(t, "local.yaml", configYAML(server.URL, ""))

	var out bytes.Buffer
	require.NoError(t, runPickings(context.Background(), []string{"validate", "-config", config, "-id", "21", "-yes"}, &out))
	assert.Equal(t, 1, validated)
	assert.Contains(t, out.String(), `"name": "WH/OUT/1"`)
}

func TestPickingsRejectsBadInputWithoutRequests(t *testing.T) {
	var calls int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
	defer server.Close()
	config := writeFile(t, "local.yaml", configYAML(server.URL, ""))

	for name, args := range map[string][]string{
		"no id":      {"validate", "-config", config, "-yes"},
		"positional": {"validate", "-config", config, "-id", "1", "x"},
		"unknown":    {"cancel"},
	} {
		assert.Error(t, runPickings(context.Background(), args, &bytes.Buffer{}), name)
	}
	assert.Zero(t, calls)
}
