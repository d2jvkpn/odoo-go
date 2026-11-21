package main

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sampleFile() *CatalogFile {
	return &CatalogFile{
		Format: formatVersion, ExportedAt: "2026-11-21T10:00:00Z", Include: []string{"products"},
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
		{"format", func(f *CatalogFile) { f.Format = 2 }, "unsupported format 2"},
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
