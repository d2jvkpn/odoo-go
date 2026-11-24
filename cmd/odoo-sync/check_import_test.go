package main

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProductsCheckReportsUncodedVariantWithDefaultPolicy(t *testing.T) {
	f := sampleFile()
	f.Templates[0].Variants[0].DefaultCode = ""
	path := filepath.Join(t.TempDir(), "products.json")
	require.NoError(t, writeCatalogFile(path, f))
	var out bytes.Buffer
	err := run(context.Background(), []string{"products", "check", "-in", path}, &out)
	require.ErrorContains(t, err, "nothing was written")
	assert.Contains(t, out.String(), "variant 12664 has no default_code")

	out.Reset()
	require.NoError(t, run(context.Background(), []string{
		"products", "check", "-in", path, "-empty-code", "skip",
	}, &out))
}

func TestBOMsCheckReportsUncodedComponent(t *testing.T) {
	f := sampleFile()
	f.Section = "boms"
	f.Templates = nil
	b := sampleBOM()
	b.Lines[0].Component = ""
	f.BOMs = []FileBOM{b}
	path := filepath.Join(t.TempDir(), "boms.json")
	require.NoError(t, writeCatalogFile(path, f))
	var out bytes.Buffer
	err := run(context.Background(), []string{"boms", "check", "-in", path}, &out)
	require.ErrorContains(t, err, "nothing was written")
	assert.Contains(t, out.String(), "component has no default_code")

	out.Reset()
	require.NoError(t, run(context.Background(), []string{
		"boms", "check", "-in", path, "-empty-code", "skip",
	}, &out))
}

func TestPartnersCheckReportsMissingMatchKey(t *testing.T) {
	f := &PartnersFile{Format: partnersFormat, Partners: []FilePartner{{SourceID: 1, Name: "No key", Type: "contact"}}}
	path := filepath.Join(t.TempDir(), "partners.json")
	require.NoError(t, writePartnersFile(path, f))
	var out bytes.Buffer
	err := run(context.Background(), []string{"partners", "check", "-in", path}, &out)
	require.ErrorContains(t, err, "nothing was written")
	assert.Contains(t, out.String(), "no ref, vat or email")

	out.Reset()
	require.NoError(t, run(context.Background(), []string{
		"partners", "check", "-in", path, "-empty-key", "skip",
	}, &out))
}

func TestPartnersCheckPrintsNameStatsBeforePolicyProblems(t *testing.T) {
	f := &PartnersFile{Format: partnersFormat, Partners: []FilePartner{
		{SourceID: 1, Name: "Customer", Ref: "C1", Type: "contact"},
		{SourceID: 2, Name: "Customer", Type: "contact"},
		{SourceID: 3, ParentSourceID: 1, Name: "Shop A", Type: "delivery"},
		{SourceID: 4, ParentSourceID: 1, Name: "Shop B", Type: "delivery"},
		{SourceID: 5, ParentSourceID: 3, Name: "Store contact", Type: "contact"},
		{SourceID: 6, ParentSourceID: 4, Name: "Store contact", Type: "contact"},
	}}
	path := filepath.Join(t.TempDir(), "partners.json")
	require.NoError(t, writePartnersFile(path, f))
	var out bytes.Buffer
	err := run(context.Background(), []string{"partners", "check", "-in", path}, &out)
	require.ErrorContains(t, err, "nothing was written")
	assert.Contains(t, out.String(), "ParentType  Records  WithChildren  Stats")
	assert.Contains(t, out.String(), "top_level   2        1             is_company=false:2, names_unique=1, "+
		"duplicate_names=1, duplicate_records=1, child_delivery=2")
	assert.Contains(t, out.String(), "delivery    2        2             is_company=false:2, "+
		"names_unique=2, child_contact=2")
	assert.Contains(t, out.String(), "contact     2        0             is_company=false:2, names_unique=1, "+
		"duplicate_names=1, duplicate_records=1")
	assert.Contains(t, out.String(), "\nWarnings\n")
	assert.Contains(t, out.String(), "no ref, vat or email")
}

func TestPartnersCheckPrintsDirectChildrenByParentKind(t *testing.T) {
	f := &PartnersFile{Format: partnersFormat, Partners: []FilePartner{
		{SourceID: 1, Name: "Customer", Ref: "C1", Type: "contact"},
		{SourceID: 2, ParentSourceID: 1, Name: "Shop", Type: "delivery"},
		{SourceID: 3, ParentSourceID: 2, Name: "Shop contact", Type: "contact"},
		{SourceID: 4, ParentSourceID: 3, Name: "Billing", Type: "invoice"},
		{SourceID: 5, ParentSourceID: 1, Name: "Head office contact", Type: "contact"},
	}}
	path := filepath.Join(t.TempDir(), "partners.json")
	require.NoError(t, writePartnersFile(path, f))
	var out bytes.Buffer
	require.NoError(t, run(context.Background(), []string{"partners", "check", "-in", path}, &out))
	assert.Contains(t, out.String(), "top_level   1        1             is_company=false:1, names_unique=1, "+
		"child_contact=1, child_delivery=1")
	assert.Contains(t, out.String(), "delivery    1        1             is_company=false:1, "+
		"names_unique=1, child_contact=1")
	assert.Contains(t, out.String(), "contact     2        1             is_company=false:2, "+
		"names_unique=2, child_invoice=1")
	assert.Contains(t, out.String(), "invoice     1        0             is_company=false:1, names_unique=1")
}

func TestProductsCheckWithTargetReportsMissingUnitWithoutWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "products.json")
	require.NoError(t, writeCatalogFile(path, sampleFile()))
	var writes []string
	target := fakeOdoo(t, map[string]string{
		"/json/2/product.category/search_read":        `[]`,
		"/json/2/product.attribute/search_read":       `[]`,
		"/json/2/product.attribute.value/search_read": `[]`,
		"/json/2/uom.uom/search_read":                 `[]`,
		"/json/2/product.product/search_read":         `[]`,
	}, &writes)
	var out bytes.Buffer
	err := run(context.Background(), []string{
		"products", "check", "-in", path, "-target", writeConnection(t, "dst.yaml", target),
	}, &out)
	require.ErrorContains(t, err, "nothing was written")
	assert.Contains(t, out.String(), `unit of measure "kg" does not exist in the target`)
	assert.Empty(t, writes)
}

func TestPartnersCheckWithTargetReportsMissingCountryWithoutWrites(t *testing.T) {
	path := exportPartnersFile(t)
	routes := partnerTargetRoutes(`[]`)
	routes["/json/2/res.country/search_read"] = `[]`
	var writes []string
	target := fakeOdoo(t, routes, &writes)
	var out bytes.Buffer
	err := run(context.Background(), []string{
		"partners", "check", "-in", path, "-target", writeConnection(t, "dst.yaml", target),
	}, &out)
	require.ErrorContains(t, err, "nothing was written")
	assert.Contains(t, out.String(), `country "Norway" does not exist in the target`)
	assert.Empty(t, writes)
}

func TestBOMsCheckWithTargetReportsMissingComponentWithoutWrites(t *testing.T) {
	f := sampleFile()
	f.Section = "boms"
	f.Templates = nil
	f.BOMs = []FileBOM{sampleBOM()}
	path := filepath.Join(t.TempDir(), "boms.json")
	require.NoError(t, writeCatalogFile(path, f))
	routes := bomTargetRoutes(`[]`, `[]`)
	routes["/json/2/product.product/search_read"] = variantsJSON([3]any{7001, 8001, "273001101"})
	var writes []string
	target := fakeOdoo(t, routes, &writes)
	var out bytes.Buffer
	err := run(context.Background(), []string{
		"boms", "check", "-in", path, "-target", writeConnection(t, "dst.yaml", target),
	}, &out)
	require.ErrorContains(t, err, "nothing was written")
	assert.Contains(t, out.String(), `component "121001090" does not exist in the target`)
	assert.Empty(t, writes)
}

func TestPartnersCheckWithTargetPassesWithoutWrites(t *testing.T) {
	path := exportPartnersFile(t)
	var writes []string
	target := fakeOdoo(t, partnerTargetRoutes(`[]`), &writes)
	var out bytes.Buffer
	require.NoError(t, run(context.Background(), []string{
		"partners", "check", "-in", path, "-target", writeConnection(t, "dst.yaml", target),
	}, &out))
	assert.Contains(t, out.String(), "target preflight passed; nothing was written")
	assert.Empty(t, writes)
}
