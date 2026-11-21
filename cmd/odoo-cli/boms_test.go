package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bomServer answers mrp.bom reads with one BOM and one line and records the BOM request body.
func bomServer(t *testing.T, bomBody *string) string {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/json/2/mrp.bom/search_read":
			b, _ := io.ReadAll(r.Body)
			*bomBody = string(b)
			fmt.Fprint(w, `[{"id":7,"code":"CAKE","type":"normal","product_qty":2,"product_uom_id":[1,"Units"],`+
				`"product_tmpl_id":[1487,"Cake"],"product_id":false,"active":true,"operation_ids":[],`+
				`"byproduct_ids":[],"bom_line_ids":[70]}]`)
		case "/json/2/mrp.bom.line/search_read":
			fmt.Fprint(w, `[{"id":70,"bom_id":[7,"Cake"],"product_id":[10,"Flour"],"product_qty":0.5,`+
				`"product_uom_id":[2,"kg"],"bom_product_template_attribute_value_ids":[],"operation_id":false,"sequence":1}]`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)
	return writeFile(t, "local.yaml", configYAML(server.URL, "test-db"))
}

func TestRunBOMsListPrintsBOMsWithLines(t *testing.T) {
	var out bytes.Buffer
	var body string

	config := bomServer(t, &body)
	require.NoError(t, run(context.Background(), []string{"-config", config, "boms", "list"}, &out))
	assert.JSONEq(t, `[{"id":7,"code":"CAKE","type":"normal","product_qty":2,"product_uom_id":{"id":1,"name":"Units"},`+
		`"product_tmpl_id":{"id":1487,"name":"Cake"},"product_id":{"id":0,"name":""},"active":true,`+
		`"operation_ids":[],"byproduct_ids":[],"bom_line_ids":[70],"lines":[{"id":70,`+
		`"bom_id":{"id":7,"name":"Cake"},"product_id":{"id":10,"name":"Flour"},"product_qty":0.5,`+
		`"product_uom_id":{"id":2,"name":"kg"},"bom_product_template_attribute_value_ids":[],`+
		`"operation_id":{"id":0,"name":""},"sequence":1}]}]`, out.String())
	assert.NotContains(t, body, "active_test")
}

func TestRunBOMsListArchivedSendsActiveTest(t *testing.T) {
	var out bytes.Buffer
	var body string

	config := bomServer(t, &body)
	require.NoError(t, run(context.Background(), []string{"boms", "list", "-archived", "-config", config}, &out))
	assert.Contains(t, body, `"active_test":false`)
}

func TestRunBOMsGetPrintsOneBOM(t *testing.T) {
	var out bytes.Buffer
	var body string

	config := bomServer(t, &body)
	require.NoError(t, run(context.Background(), []string{"-config", config, "boms", "get", "-id", "7"}, &out))
	assert.Contains(t, out.String(), `"code": "CAKE"`)
	assert.Contains(t, out.String(), `"lines"`)
}

func TestRunBOMsRejectsBadUsage(t *testing.T) {
	var out bytes.Buffer

	ctx := context.Background()
	assert.ErrorContains(t, run(ctx, []string{"boms", "get"}, &out), "id must be greater than zero")
	assert.ErrorContains(t, run(ctx, []string{"boms", "get", "-id", "0"}, &out), "id must be greater than zero")
	assert.ErrorContains(t, run(ctx, []string{"boms", "frob"}, &out), `unknown boms command "frob"`)
	assert.ErrorContains(t, run(ctx, []string{"boms", "list", "extra"}, &out), "unexpected positional arguments")

	out.Reset()
	require.NoError(t, run(ctx, []string{"boms"}, &out))
	assert.Contains(t, out.String(), "odoo boms list")
}
