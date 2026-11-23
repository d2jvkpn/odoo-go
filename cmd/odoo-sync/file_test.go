package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sampleFile() *CatalogFile {
	return &CatalogFile{
		Format: formatVersion, ExportedAt: "2026-11-21T10:00:00Z", Section: "products",
		Source:     FileSource{Name: "src", BaseURL: "http://odoo.example"},
		Categories: []FileCategory{{Path: "蔬菜 / 番茄类"}},
		Attributes: []FileAttribute{{Name: "规格", Values: []string{"1kg", "1.5kg"}}},
		Templates: []FileTemplate{
			{
				SourceID: 1643, Name: "西红柿块", Category: "蔬菜 / 番茄类", ListPrice: 1, SaleOK: true, Type: "consu",
				IsStorable: true, UoM: "kg",
				AttributeLines: []FileAttrLine{{Attribute: "规格", Values: []FileAttrValue{
					{Name: "1kg"}, {Name: "1.5kg", PriceExtra: 2.5},
				}}},
				Variants: []FileVariant{
					{SourceID: 12664, DefaultCode: "A-1", Values: map[string]string{"规格": "1kg"}, Active: true},
					{SourceID: 12663, DefaultCode: "A-2", Values: map[string]string{"规格": "1.5kg"}, Active: true},
				},
			},
			{
				SourceID: 1487, Name: "冬枣散装", Category: "水果", SaleOK: true, UoM: "kg",
				Variants: []FileVariant{{SourceID: 10688, DefaultCode: "273001101", Values: map[string]string{}, Active: true}},
			},
		},
	}
}

func TestCatalogFileRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.json")
	require.NoError(t, writeCatalogFile(path, sampleFile()))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"section": "products"`)
	assert.NotContains(t, string(data), `"include"`)
	got, err := readCatalogFile(path)
	require.NoError(t, err)
	assert.Equal(t, sampleFile(), got)
}

func TestValidateAcceptsSampleFile(t *testing.T) {
	assert.Empty(t, sampleFile().Validate())
}

func TestValidateReportsEveryProblem(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(f *CatalogFile)
		want   string
	}{
		{"format", func(f *CatalogFile) { f.Format = 1 }, "unsupported format 1"},
		{"blank name", func(f *CatalogFile) { f.Templates[0].Name = " " }, "name is empty"},
		{"no variants", func(f *CatalogFile) { f.Templates[1].Variants = nil }, "no variants"},
		{"duplicate code", func(f *CatalogFile) { f.Templates[1].Variants[0].DefaultCode = "A-1" }, `default_code "A-1" appears in templates`},
		{"missing value", func(f *CatalogFile) { f.Templates[0].Variants[0].Values = map[string]string{} }, "one value per attribute line"},
		{"unknown value", func(f *CatalogFile) { f.Templates[0].Variants[0].Values["规格"] = "9kg" }, `value "9kg" for attribute "规格"`},
		{"repeated combination", func(f *CatalogFile) { f.Templates[0].Variants[1].Values["规格"] = "1kg" }, "repeats the value combination"},
		{"repeated line", func(f *CatalogFile) {
			f.Templates[0].AttributeLines = append(f.Templates[0].AttributeLines, f.Templates[0].AttributeLines[0])
		}, `attribute "规格" appears twice`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := sampleFile()
			tt.mutate(f)
			problems := f.Validate()
			require.NotEmpty(t, problems)
			assert.Contains(t, problems[0], tt.want)
		})
	}
}

func TestVariantKeyIsOrderIndependent(t *testing.T) {
	assert.Equal(t, variantKey(map[string]string{"a": "1", "b": "2"}), variantKey(map[string]string{"b": "2", "a": "1"}))
	assert.Equal(t, "", variantKey(nil))
}

func sampleBOM() FileBOM {
	return FileBOM{
		SourceID: 258, Product: &FileBOMProduct{DefaultCode: "273001101"}, Template: FileBOMTemplate{SourceID: 1487},
		Type: "normal", ProductQty: 1, UoM: "kg", Code: "B1", Active: true,
		Lines: []FileBOMLine{{Component: "121001090", Qty: 1.2, UoM: "kg"}},
	}
}

func TestCatalogFileWithBOMsRoundTripsAndHasSections(t *testing.T) {
	f := sampleFile()
	tmpl := sampleBOM()
	tmpl.SourceID, tmpl.Product = 259, nil
	tmpl.Template.DefaultCodes = []string{"A-1", "A-2"}
	f.BOMs = []FileBOM{sampleBOM(), tmpl}
	f.Section = "all"

	path := filepath.Join(t.TempDir(), "catalog.json")
	require.NoError(t, writeCatalogFile(path, f))
	got, err := readCatalogFile(path)
	require.NoError(t, err)
	assert.Equal(t, f, got)
	assert.True(t, got.HasSection("boms"))
	assert.True(t, got.HasSection("products"))
	assert.False(t, sampleFile().HasSection("boms"))
	assert.Empty(t, got.Validate())
}

func TestValidateReportsBOMProblems(t *testing.T) {
	nan := 0.0
	nan = nan / nan
	tests := []struct {
		name   string
		mutate func(b *FileBOM)
		want   string
	}{
		{"type", func(b *FileBOM) { b.Type = "subcontract" }, "type must be normal or phantom"},
		{"zero qty", func(b *FileBOM) { b.ProductQty = 0 }, "product_qty must be positive"},
		{"nan qty", func(b *FileBOM) { b.ProductQty = nan }, "product_qty must be positive"},
		{"no uom", func(b *FileBOM) { b.UoM = "" }, "uom is empty"},
		{"no lines", func(b *FileBOM) { b.Lines = nil }, "has no lines"},
		{"line qty", func(b *FileBOM) { b.Lines[0].Qty = 0 }, "line 1: qty must be positive"},
		{"line uom", func(b *FileBOM) { b.Lines[0].UoM = "" }, "line 1: uom is empty"},
		{"product and codes", func(b *FileBOM) { b.Template.DefaultCodes = []string{"A-1"} }, "both product and template default_codes"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := sampleFile()
			b := sampleBOM()
			tt.mutate(&b)
			f.BOMs = []FileBOM{b}
			problems := f.Validate()
			require.NotEmpty(t, problems)
			assert.Contains(t, problems[0], tt.want)
		})
	}

	f := sampleFile()
	b := sampleBOM()
	b.Product = nil
	f.BOMs = []FileBOM{b}
	assert.Empty(t, f.Validate(), "a BOM without any code is left to the empty-code policy")
}
