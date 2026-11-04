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

func TestProductsVariantsPriceAndList(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/json/2/product.template.attribute.value/search_read":
			fmt.Fprint(w, `[{"id":29}]`)
		case "/json/2/product.template.attribute.value/write":
			fmt.Fprint(w, "true")
		case "/json/2/product.product/search_read":
			fmt.Fprint(w, `[{"id":67,"display_name":"Tea (500g)"}]`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	config := writeFile(t, "local.yaml", configYAML(server.URL, ""))

	var out bytes.Buffer
	err := runProducts(context.Background(),
		[]string{"variants", "price", "-config", config, "-value-id", "29", "-extra", "2.5"}, &out)
	require.NoError(t, err)
	assert.JSONEq(t, `{"value_id":29,"price_extra":2.5}`, out.String())

	out.Reset()
	err = runProducts(context.Background(),
		[]string{"variants", "list", "-config", config, "-template", "45"}, &out)
	require.NoError(t, err)
	assert.Contains(t, out.String(), `"display_name": "Tea (500g)"`)
}

func TestProductsVariantsAddRejectsBadInputWithoutRequests(t *testing.T) {
	var calls int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
	defer server.Close()
	config := writeFile(t, "local.yaml", configYAML(server.URL, ""))

	for name, args := range map[string][]string{
		"no values":   {"variants", "add", "-config", config, "-template", "45", "-attribute", "W"},
		"no template": {"variants", "add", "-config", config, "-attribute", "W", "-value", "a"},
		"unknown":     {"variants", "nope"},
		"positional":  {"variants", "list", "-config", config, "-template", "1", "x"},
		"no value id": {"variants", "price", "-config", config},
	} {
		assert.Error(t, runProducts(context.Background(), args, &bytes.Buffer{}), name)
	}
	assert.Zero(t, calls)
}
