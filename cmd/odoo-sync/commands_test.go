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

const unitKGJSON = `[{"id":1,"name":"kg","relative_uom_id":false,"relative_factor":1,"active":true}]`

func TestExportThenDryRunImportCreatesNothing(t *testing.T) {
	var sourceWrites, targetWrites []string

	source := fakeOdoo(t, map[string]string{
		"/json/2/product.product/search_read": sourceVariantsJSON,
		"/json/2/uom.uom/search_read":         unitKGJSON,
		"/json/2/mrp.bom/search_read":         `[]`,
		"/json/2/product.template.attribute.value/search_read": `[{"id":30,"attribute_id":[3,"规格"],"name":"1kg","price_extra":0.0},` +
			`{"id":31,"attribute_id":[3,"规格"],"name":"1.5kg","price_extra":2.5}]`,
	}, &sourceWrites)
	target := fakeOdoo(t, map[string]string{
		"/json/2/product.product/search_read":         `[]`,
		"/json/2/product.attribute/search_read":       `[]`,
		"/json/2/product.attribute.value/search_read": `[]`,
		"/json/2/uom.uom/search_read":                 unitKGJSON,
		"/json/2/product.category/search_read": `[{"id":6,"name":"蔬菜","complete_name":"蔬菜","parent_id":false},` +
			`{"id":7,"name":"番茄类","complete_name":"蔬菜 / 番茄类","parent_id":[6,"蔬菜"]}]`,
	}, &targetWrites)

	catalog := filepath.Join(t.TempDir(), "catalog.json")
	var out bytes.Buffer
	config := writeConnection(t, "src.yaml", source)
	require.NoError(t, run(context.Background(), []string{"products", "export", "-source", config, "-out", catalog}, &out))
	assert.Contains(t, out.String(), "exported 2 templates, 3 variants")
	data, err := os.ReadFile(catalog)
	require.NoError(t, err)
	assert.Contains(t, string(data), fmt.Sprintf(`"filename": %q`, config))
	assert.NotContains(t, string(data), `"name": "src"`)

	out.Reset()
	require.NoError(t, run(context.Background(), []string{"products", "check", "-in", catalog}, &out))

	out.Reset()
	require.NoError(t, run(context.Background(), []string{
		"products", "import", "-target", writeConnection(t, "dst.yaml", target), "-in", catalog, "-dry-run",
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

func TestProductsExportWarnsAboutActiveVariantsWithoutDefaultCode(t *testing.T) {
	var writes []string
	source := fakeOdoo(t, map[string]string{
		"/json/2/product.product/search_read": `[` +
			`{"id":10,"product_tmpl_id":[1,"Active"],"name":"Active","default_code":false,` +
			`"categ_id":false,"uom_id":[1,"kg"],"active":true},` +
			`{"id":11,"product_tmpl_id":[2,"Archived"],"name":"Archived","default_code":false,` +
			`"categ_id":false,"uom_id":[1,"kg"],"active":false}]`,
		"/json/2/product.template.attribute.value/search_read": `[]`,
		"/json/2/uom.uom/search_read":                          unitKGJSON,
	}, &writes)
	var out bytes.Buffer
	catalog := filepath.Join(t.TempDir(), "catalog.json")
	err := run(context.Background(), []string{
		"products", "export", "-source", writeConnection(t, "src.yaml", source), "-out", catalog,
	}, &out)
	require.NoError(t, err)
	assert.Contains(t, out.String(), "warning: 1 active variant without default_code was exported")
	assert.Contains(t, out.String(), "omitted 1 archived variants without default_code")
	assert.Empty(t, writes)
}

func TestImportRejectsBadFlagsBeforeAnyRequest(t *testing.T) {
	ctx := context.Background()
	var out bytes.Buffer

	assert.ErrorContains(t, run(ctx, []string{"products", "import", "-target", "t.yaml", "-in", "c.json", "-include", "boms"}, &out), "flag provided but not defined")
	assert.ErrorContains(t, run(ctx, []string{"boms", "import", "-target", "t.yaml", "-in", "c.json", "-map-out", "m.csv"}, &out), "-map-out needs products")
	assert.ErrorContains(t, run(ctx, []string{"products", "import", "-target", "t.yaml", "-in", "c.json", "-empty-code", "maybe"}, &out), "-empty-code")
	assert.ErrorContains(t, run(ctx, []string{"products", "import", "-target", "t.yaml", "-in", "c.json", "-sale-ok", "some"}, &out), "-sale-ok")
	assert.ErrorContains(t, run(ctx, []string{"products", "import", "-target", "t.yaml", "-in", "c.json", "-dry-run", "-map-out", "m.csv"}, &out), "-map-out")
	assert.ErrorContains(t, run(ctx, []string{"products", "import", "-in", "c.json"}, &out), "-target")
	assert.ErrorContains(t, run(ctx, []string{"products", "export", "-source", "s.yaml"}, &out), "-out")
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
	err := run(context.Background(), []string{"products", "import", "-target", writeConnection(t, "dst.yaml", target), "-in", catalog}, &out)
	require.Error(t, err)
	assert.Contains(t, out.String(), `unit of measure "kg"`)
}

const bomVariantJSON = `{"id":%d,"product_tmpl_id":[%d,"x"],"name":"x","default_code":"%s","list_price":1.0,` +
	`"categ_id":[8,"水果"],"sale_ok":true,"is_storable":true,"type":"consu","uom_id":[1,"kg"],"description_sale":false,` +
	`"barcode":false,"weight":0.0,"volume":0.0,"product_template_variant_value_ids":[],"active":true}`

func variantsJSON(rows ...[3]any) string {
	parts := make([]string, 0, len(rows))
	for _, r := range rows {
		parts = append(parts, fmt.Sprintf(bomVariantJSON, r[0], r[1], r[2]))
	}
	return "[" + strings.Join(parts, ",") + "]"
}

const sourceBOMJSON = `[{"id":258,"code":"B1","type":"normal","product_qty":1.0,"product_uom_id":[1,"kg"],` +
	`"product_tmpl_id":[1487,"x"],"product_id":[10688,"x"],"active":true,"operation_ids":[],"byproduct_ids":[],"bom_line_ids":[1]}]`

const sourceBOMLinesJSON = `[{"id":1,"bom_id":[258,"x"],"product_id":[500,"y"],"product_qty":1.2,"product_uom_id":[1,"kg"],` +
	`"bom_product_template_attribute_value_ids":[],"operation_id":false,"sequence":1}]`

func bomSourceRoutes() map[string]string {
	return map[string]string{
		"/json/2/product.product/search_read": variantsJSON([3]any{10688, 1487, "273001101"}, [3]any{500, 9, "121001090"}),
		"/json/2/uom.uom/search_read":         unitKGJSON,
		"/json/2/mrp.bom/search_read":         sourceBOMJSON,
		"/json/2/mrp.bom.line/search_read":    sourceBOMLinesJSON,
	}
}

// bomTargetRoutes is a target that holds both products and the given BOMs.
func bomTargetRoutes(boms, lines string) map[string]string {
	return map[string]string{
		"/json/2/product.product/search_read":         variantsJSON([3]any{7001, 8001, "273001101"}, [3]any{7002, 8002, "121001090"}),
		"/json/2/product.attribute/search_read":       `[]`,
		"/json/2/product.attribute.value/search_read": `[]`,
		"/json/2/uom.uom/search_read":                 unitKGJSON,
		"/json/2/product.category/search_read":        `[]`,
		"/json/2/mrp.bom/search_read":                 boms,
		"/json/2/mrp.bom.line/search_read":            lines,
		"/json/2/mrp.bom/create":                      `[91]`,
	}
}

func exportBOMFile(t *testing.T, routes map[string]string, section string) string {
	t.Helper()

	source := fakeOdoo(t, routes, &[]string{})
	catalog := filepath.Join(t.TempDir(), "catalog.json")
	var out bytes.Buffer
	require.NoError(t, run(context.Background(), []string{
		section, "export", "-source", writeConnection(t, "src.yaml", source), "-out", catalog,
	}, &out))
	return catalog
}

func TestExportBOMsWritesTheSectionAndRefusesUnsupportedFeatures(t *testing.T) {
	catalog := exportBOMFile(t, bomSourceRoutes(), "boms")
	f, err := readCatalogFile(catalog)
	require.NoError(t, err)
	assert.Equal(t, "boms", f.Section)
	require.Len(t, f.BOMs, 1)
	assert.Equal(t, "273001101", f.BOMs[0].Product.DefaultCode)
	assert.Equal(t, "121001090", f.BOMs[0].Lines[0].Component)

	var out bytes.Buffer
	require.NoError(t, run(context.Background(), []string{"boms", "check", "-in", catalog}, &out))
	assert.Contains(t, out.String(), "1 boms")

	routes := bomSourceRoutes()
	routes["/json/2/mrp.bom/search_read"] = strings.Replace(sourceBOMJSON, `"operation_ids":[]`, `"operation_ids":[3]`, 1)
	source := fakeOdoo(t, routes, &[]string{})
	bad := filepath.Join(t.TempDir(), "bad.json")
	err = run(context.Background(), []string{"boms", "export", "-source", writeConnection(t, "src.yaml", source), "-out", bad}, &out)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not support")
	assert.NoFileExists(t, bad)
}

func TestImportBOMsDryRunWritesNothingAndRealRunCreatesOneBOM(t *testing.T) {
	catalog := exportBOMFile(t, bomSourceRoutes(), "boms")

	var writes []string
	target := fakeOdoo(t, bomTargetRoutes(`[]`, `[]`), &writes)
	conn := writeConnection(t, "dst.yaml", target)
	var out bytes.Buffer
	require.NoError(t, run(context.Background(), []string{"boms", "import", "-target", conn, "-in", catalog, "-dry-run"}, &out))
	assert.Contains(t, out.String(), "create bom for 273001101")
	assert.Contains(t, out.String(), "1 to create, 0 to archive")
	assert.Empty(t, writes)

	out.Reset()
	require.NoError(t, run(context.Background(), []string{"boms", "import", "-target", conn, "-in", catalog}, &out))
	assert.Equal(t, []string{"/json/2/mrp.bom/create"}, writes)
	assert.Contains(t, out.String(), "boms: created 1, archived 0, code updated 0, unchanged 0, failed 0")
}

func TestImportBOMsAloneListsEveryMissingProductAndWritesNothing(t *testing.T) {
	catalog := exportBOMFile(t, bomSourceRoutes(), "boms")

	var writes []string
	routes := bomTargetRoutes(`[]`, `[]`)
	routes["/json/2/product.product/search_read"] = `[]`
	target := fakeOdoo(t, routes, &writes)
	var out bytes.Buffer
	err := run(context.Background(), []string{"boms", "import", "-target", writeConnection(t, "dst.yaml", target), "-in", catalog}, &out)
	require.Error(t, err)
	assert.Contains(t, out.String(), `product "273001101" does not exist in the target`)
	assert.Contains(t, out.String(), `component "121001090" does not exist in the target`)
	assert.Empty(t, writes)
}

func TestImportBOMsIsIdempotent(t *testing.T) {
	catalog := exportBOMFile(t, bomSourceRoutes(), "boms")

	targetBOM := `[{"id":91,"code":"B1","type":"normal","product_qty":1.0,"product_uom_id":[1,"kg"],` +
		`"product_tmpl_id":[8001,"x"],"product_id":[7001,"x"],"active":true,"operation_ids":[],"byproduct_ids":[],"bom_line_ids":[1]}]`
	targetLines := `[{"id":1,"bom_id":[91,"x"],"product_id":[7002,"y"],"product_qty":1.2,"product_uom_id":[1,"kg"],` +
		`"bom_product_template_attribute_value_ids":[],"operation_id":false,"sequence":1}]`
	var writes []string
	target := fakeOdoo(t, bomTargetRoutes(targetBOM, targetLines), &writes)
	var out bytes.Buffer
	require.NoError(t, run(context.Background(), []string{
		"boms", "import", "-target", writeConnection(t, "dst.yaml", target), "-in", catalog, "-dry-run",
	}, &out))
	assert.Contains(t, out.String(), "0 to create, 0 to archive, 0 code updates, 1 unchanged")
	assert.Empty(t, writes)
}

func TestImportNeedsTheSectionInTheFile(t *testing.T) {
	var out bytes.Buffer
	productsOnly := filepath.Join(t.TempDir(), "products.json")
	require.NoError(t, writeCatalogFile(productsOnly, sampleFile()))
	err := run(context.Background(), []string{"boms", "import", "-target", writeConnection(t, "dst.yaml", fakeOdoo(t, nil, &[]string{})), "-in", productsOnly}, &out)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "the file has no boms section")

	bomsOnly := exportBOMFile(t, bomSourceRoutes(), "boms")
	err = run(context.Background(), []string{"products", "import", "-target", writeConnection(t, "dst.yaml", fakeOdoo(t, nil, &[]string{})), "-in", bomsOnly}, &out)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "the file has no products section")
}

func TestImportProductsOnlyNeverTouchesBOMs(t *testing.T) {
	f := sampleFile()
	f.Section = "all"
	f.BOMs = []FileBOM{fBOM(1, "A-1", "", fLine("273001101", 1))}
	catalog := filepath.Join(t.TempDir(), "catalog.json")
	require.NoError(t, writeCatalogFile(catalog, f))

	target := fakeOdoo(t, map[string]string{ // no mrp routes: a BOM request would fail the test
		"/json/2/product.product/search_read":         `[]`,
		"/json/2/product.attribute/search_read":       `[]`,
		"/json/2/product.attribute.value/search_read": `[]`,
		"/json/2/uom.uom/search_read":                 unitKGJSON,
		"/json/2/product.category/search_read":        `[{"id":7,"name":"番茄类","complete_name":"蔬菜 / 番茄类","parent_id":false}]`,
	}, &[]string{})
	var out bytes.Buffer
	require.NoError(t, run(context.Background(), []string{
		"products", "import", "-target", writeConnection(t, "dst.yaml", target), "-in", catalog, "-dry-run",
	}, &out))
	assert.NotContains(t, out.String(), "bom summary")
}
