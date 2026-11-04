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

func TestOrdersFulfillmentReadsPrintJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/json/2/sale.order/read" {
			fmt.Fprint(w, `[{"id":29,"picking_ids":[],"invoice_ids":[]}]`)
			return
		}
		t.Errorf("unexpected path %s", r.URL.Path)
	}))
	defer server.Close()
	config := writeFile(t, "local.yaml", configYAML(server.URL, ""))

	for _, action := range []string{"pickings", "invoices", "payments"} {
		var out bytes.Buffer
		err := runOrders(context.Background(), []string{action, "-config", config, "-id", "29"}, &out)
		require.NoError(t, err, action)
		assert.JSONEq(t, `[]`, out.String(), action)
	}
}

func TestOrdersFulfillmentRejectsBadInputWithoutRequests(t *testing.T) {
	var calls int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
	defer server.Close()
	config := writeFile(t, "local.yaml", configYAML(server.URL, ""))

	for name, args := range map[string][]string{
		"no id":      {"pickings", "-config", config},
		"positional": {"invoices", "-config", config, "-id", "1", "x"},
	} {
		assert.Error(t, runOrders(context.Background(), args, &bytes.Buffer{}), name)
	}
	assert.Zero(t, calls)
}
