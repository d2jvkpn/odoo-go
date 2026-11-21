package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeOdoo serves canned bodies by path and records every create and write request.
func fakeOdoo(t *testing.T, routes map[string]string, writes *[]string) string {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/create") || strings.HasSuffix(r.URL.Path, "/write") {
			*writes = append(*writes, r.URL.Path)
		}
		body, ok := routes[r.URL.Path]
		if !ok {
			t.Errorf("unexpected path %s", r.URL.Path)
			http.Error(w, "{}", http.StatusNotFound)
			return
		}
		fmt.Fprint(w, body)
	}))
	t.Cleanup(server.Close)
	return server.URL
}

func writeConnection(t *testing.T, name, url string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte("odoo:\n  base_url: "+url+"\n  api_key: test-key\n"), 0o600))
	return path
}

const sourceVariantsJSON = `[` +
	`{"id":10688,"product_tmpl_id":[1487,"冬枣散装"],"name":"冬枣散装","default_code":"273001101","list_price":9.5,` +
	`"categ_id":[8,"水果"],"sale_ok":true,"is_storable":true,"type":"consu","uom_id":[1,"kg"],"description_sale":false,` +
	`"barcode":false,"weight":0.0,"volume":0.0,"product_template_variant_value_ids":[],"active":true},` +
	`{"id":12664,"product_tmpl_id":[1643,"西红柿块"],"name":"西红柿块","default_code":"A-1","list_price":1.0,` +
	`"categ_id":[7,"蔬菜 / 番茄类"],"sale_ok":true,"is_storable":true,"type":"consu","uom_id":[1,"kg"],` +
	`"description_sale":false,"barcode":false,"weight":0.0,"volume":0.0,"product_template_variant_value_ids":[30],"active":true},` +
	`{"id":12663,"product_tmpl_id":[1643,"西红柿块"],"name":"西红柿块","default_code":"A-2","list_price":1.0,` +
	`"categ_id":[7,"蔬菜 / 番茄类"],"sale_ok":true,"is_storable":true,"type":"consu","uom_id":[1,"kg"],` +
	`"description_sale":false,"barcode":false,"weight":0.0,"volume":0.0,"product_template_variant_value_ids":[31],"active":true}]`

func TestExportThenDryRunImportCreatesNothing(t *testing.T) {
	var sourceWrites, targetWrites []string

	source := fakeOdoo(t, map[string]string{
		"/json/2/product.product/search_read": sourceVariantsJSON,
		"/json/2/product.template.attribute.value/search_read": `[{"id":30,"attribute_id":[3,"规格"],"name":"1kg","price_extra":0.0},` +
			`{"id":31,"attribute_id":[3,"规格"],"name":"1.5kg","price_extra":2.5}]`,
	}, &sourceWrites)
	target := fakeOdoo(t, map[string]string{
		"/json/2/product.product/search_read":         `[]`,
		"/json/2/product.attribute/search_read":       `[]`,
		"/json/2/product.attribute.value/search_read": `[]`,
		"/json/2/uom.uom/search_read":                 `[{"id":1,"name":"kg"}]`,
		"/json/2/product.category/search_read": `[{"id":6,"name":"蔬菜","complete_name":"蔬菜","parent_id":false},` +
			`{"id":7,"name":"番茄类","complete_name":"蔬菜 / 番茄类","parent_id":[6,"蔬菜"]}]`,
	}, &targetWrites)

	catalog := filepath.Join(t.TempDir(), "catalog.json")
	var out bytes.Buffer
	require.NoError(t, run(context.Background(), []string{"export", "-source", writeConnection(t, "src.yaml", source), "-out", catalog}, &out))
	assert.Contains(t, out.String(), "exported 2 templates, 3 variants")

	out.Reset()
	require.NoError(t, run(context.Background(), []string{"check", "-in", catalog}, &out))

	out.Reset()
	require.NoError(t, run(context.Background(), []string{
		"import", "-target", writeConnection(t, "dst.yaml", target), "-in", catalog, "-dry-run",
	}, &out))
	text := out.String()
	assert.Contains(t, text, `create category "水果"`)
	assert.Contains(t, text, `create attribute "规格"`)
	assert.Contains(t, text, `create template 1487 "冬枣散装"`)
	assert.Contains(t, text, `create template 1643 "西红柿块"`)
	assert.Contains(t, text, "summary: 2 to create")
	assert.Empty(t, sourceWrites)
	assert.Empty(t, targetWrites)
}

func TestImportRejectsBadFlagsBeforeAnyRequest(t *testing.T) {
	ctx := context.Background()
	var out bytes.Buffer

	assert.ErrorContains(t, run(ctx, []string{"import", "-target", "t.yaml", "-in", "c.json", "-include", "boms"}, &out), "not implemented")
	assert.ErrorContains(t, run(ctx, []string{"import", "-target", "t.yaml", "-in", "c.json", "-empty-code", "maybe"}, &out), "-empty-code")
	assert.ErrorContains(t, run(ctx, []string{"import", "-target", "t.yaml", "-in", "c.json", "-sale-ok", "some"}, &out), "-sale-ok")
	assert.ErrorContains(t, run(ctx, []string{"import", "-target", "t.yaml", "-in", "c.json", "-dry-run", "-map-out", "m.csv"}, &out), "-map-out")
	assert.ErrorContains(t, run(ctx, []string{"import", "-in", "c.json"}, &out), "-target")
	assert.ErrorContains(t, run(ctx, []string{"export", "-source", "s.yaml"}, &out), "-out")
}

func TestImportStopsOnProblemsAndPrintsThemAll(t *testing.T) {
	target := fakeOdoo(t, map[string]string{
		"/json/2/product.product/search_read":         `[]`,
		"/json/2/product.attribute/search_read":       `[]`,
		"/json/2/product.attribute.value/search_read": `[]`,
		"/json/2/uom.uom/search_read":                 `[]`,
		"/json/2/product.category/search_read":        `[]`,
	}, &[]string{})
	f := sampleFile()
	catalog := filepath.Join(t.TempDir(), "catalog.json")
	require.NoError(t, writeCatalogFile(catalog, f))

	var out bytes.Buffer
	err := run(context.Background(), []string{"import", "-target", writeConnection(t, "dst.yaml", target), "-in", catalog}, &out)
	require.Error(t, err)
	assert.Contains(t, out.String(), `unit of measure "kg"`)
}
