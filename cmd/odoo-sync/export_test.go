package main

import (
	"context"
	"testing"
	"time"

	"github.com/d2jvkpn/odoo-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeSource struct {
	variants []odoo.CatalogVariant
	values   map[int]odoo.ProductAttributeValue
	boms     []odoo.BOM
	uoms     []odoo.SyncUoM
	noBOMs   bool // ListBOMs must not be called
}

func (f fakeSource) ListBOMs(context.Context, bool) ([]odoo.BOM, error) {
	if f.noBOMs {
		panic("ListBOMs must not be called")
	}
	return f.boms, nil
}

func (f fakeSource) ListCatalogVariants(context.Context, bool) ([]odoo.CatalogVariant, error) {
	return f.variants, nil
}

func (f fakeSource) ListSyncUoMs(context.Context) ([]odoo.SyncUoM, error) {
	if f.uoms == nil {
		return []odoo.SyncUoM{{ID: 1, Name: "kg", RelativeFactor: 1, Active: true}}, nil
	}
	return f.uoms, nil
}

func (f fakeSource) ProductAttributeValues(context.Context, []int) (map[int]odoo.ProductAttributeValue, error) {
	return f.values, nil
}

func sourceVariant(id, tmpl int, name, code string, valueIDs []int, active bool) odoo.CatalogVariant {
	return odoo.CatalogVariant{
		ID: id, Template: odoo.Many2One{ID: tmpl, Name: name}, Name: name, DefaultCode: odoo.OptionalString(code),
		ListPrice: 1, Category: odoo.Many2One{ID: 7, Name: "蔬菜 / 番茄类"}, SaleOK: true, IsStorable: true,
		Type: "consu", UoM: odoo.Many2One{ID: 1, Name: "kg"}, ValueIDs: valueIDs, Active: active,
	}
}

func TestBuildExportGroupsVariantsByTemplateAndSortsEverything(t *testing.T) {
	src := fakeSource{
		variants: []odoo.CatalogVariant{
			sourceVariant(12664, 1643, "西红柿块", "A-1", []int{30}, true),
			sourceVariant(12663, 1643, "西红柿块", "A-2", []int{31}, false),
			sourceVariant(10688, 1487, "冬枣散装", "273001101", nil, true),
		},
		values: map[int]odoo.ProductAttributeValue{
			30: {ID: 30, Attribute: odoo.Many2One{ID: 3, Name: "规格"}, Value: "1kg"},
			31: {ID: 31, Attribute: odoo.Many2One{ID: 3, Name: "规格"}, Value: "1.5kg", PriceExtra: 2.5},
		},
	}
	now := time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)
	f, omitted, err := buildExport(context.Background(), src, "src", "http://odoo.example", now, "products")
	require.NoError(t, err)
	assert.Zero(t, omitted)

	assert.Equal(t, formatVersion, f.Format)
	assert.Equal(t, "2026-11-21T10:00:00Z", f.ExportedAt)
	assert.Equal(t, "products", f.Section)
	assert.Equal(t, FileSource{Name: "src", BaseURL: "http://odoo.example"}, f.Source)
	assert.Equal(t, []FileCategory{{Path: "蔬菜 / 番茄类"}}, f.Categories)
	assert.Equal(t, []FileAttribute{{Name: "规格", Values: []string{"1.5kg", "1kg"}}}, f.Attributes)

	require.Len(t, f.Templates, 2)
	assert.Equal(t, 1487, f.Templates[0].SourceID)
	assert.Empty(t, f.Templates[0].AttributeLines)
	assert.Equal(t, map[string]string{}, f.Templates[0].Variants[0].Values)

	tomato := f.Templates[1]
	assert.Equal(t, 1643, tomato.SourceID)
	assert.Equal(t, "kg", tomato.UoM)
	assert.Equal(t, []FileAttrLine{{Attribute: "规格", Values: []FileAttrValue{
		{Name: "1.5kg", PriceExtra: 2.5}, {Name: "1kg"},
	}}}, tomato.AttributeLines)
	require.Len(t, tomato.Variants, 2)
	assert.Equal(t, 12663, tomato.Variants[0].SourceID)
	assert.False(t, tomato.Variants[0].Active)
	assert.Equal(t, map[string]string{"规格": "1.5kg"}, tomato.Variants[0].Values)
	assert.Empty(t, f.Validate())
}

func TestBuildExportFailsWhenAValueIsMissing(t *testing.T) {
	src := fakeSource{variants: []odoo.CatalogVariant{sourceVariant(1, 1, "x", "A", []int{99}, true)}}
	_, _, err := buildExport(context.Background(), src, "src", "http://odoo.example", time.Now(), "products")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "attribute value 99")
}

func TestBuildExportOmitsArchivedVariantsWithoutCode(t *testing.T) {
	src := fakeSource{
		variants: []odoo.CatalogVariant{
			sourceVariant(7162, 15, "牛排薯条", "", []int{30}, false),
			sourceVariant(7163, 15, "牛排薯条", "121031111", []int{30}, true),
			sourceVariant(7500, 16, "旧品", "", nil, false),
			sourceVariant(7600, 17, "归档但有编号", "ARCH-1", nil, false),
		},
		values: map[int]odoo.ProductAttributeValue{
			30: {ID: 30, Attribute: odoo.Many2One{ID: 3, Name: "规格"}, Value: "0.9*2cm"},
		},
	}
	f, omitted, err := buildExport(context.Background(), src, "src", "http://odoo.example", time.Now(), "products")
	require.NoError(t, err)
	assert.Equal(t, 2, omitted)
	require.Len(t, f.Templates, 2)
	assert.Equal(t, 15, f.Templates[0].SourceID)
	require.Len(t, f.Templates[0].Variants, 1)
	assert.Equal(t, 7163, f.Templates[0].Variants[0].SourceID)
	assert.Equal(t, 17, f.Templates[1].SourceID, "an archived variant that has a code is kept")
	assert.False(t, f.Templates[1].Variants[0].Active)
	assert.Empty(t, f.Validate())
}

func bomSource() fakeSource {
	return fakeSource{
		variants: []odoo.CatalogVariant{
			sourceVariant(12664, 1643, "西红柿块", "A-1", []int{30}, true),
			sourceVariant(12663, 1643, "西红柿块", "A-2", []int{31}, true),
			sourceVariant(12662, 1643, "西红柿块", "A-3", []int{32}, false),
			sourceVariant(10688, 1487, "冬枣散装", "273001101", nil, true),
			sourceVariant(500, 9, "土豆", "121001090", nil, true),
			sourceVariant(501, 10, "无编号料", "", nil, true),
		},
		values: map[int]odoo.ProductAttributeValue{
			30: {ID: 30, Attribute: odoo.Many2One{ID: 3, Name: "规格"}, Value: "1kg"},
			31: {ID: 31, Attribute: odoo.Many2One{ID: 3, Name: "规格"}, Value: "1.5kg"},
			32: {ID: 32, Attribute: odoo.Many2One{ID: 3, Name: "规格"}, Value: "2kg"},
		},
		boms: []odoo.BOM{
			{ID: 259, Code: "T", Type: "normal", Quantity: 1, UoM: odoo.Many2One{ID: 1, Name: "kg"},
				Template: odoo.Many2One{ID: 1643, Name: "西红柿块"}, Active: false,
				Lines: []odoo.BOMLine{{ID: 2, Product: odoo.Many2One{ID: 501}, Quantity: 2, UoM: odoo.Many2One{ID: 1, Name: "kg"}}}},
			{ID: 258, Code: "V", Type: "phantom", Quantity: 2, UoM: odoo.Many2One{ID: 1, Name: "kg"},
				Template: odoo.Many2One{ID: 1487, Name: "冬枣散装"}, Variant: odoo.Many2One{ID: 10688, Name: "冬枣散装"}, Active: true,
				Lines: []odoo.BOMLine{{ID: 1, Product: odoo.Many2One{ID: 500}, Quantity: 1.2, UoM: odoo.Many2One{ID: 1, Name: "kg"}}}},
		},
	}
}

func TestBuildExportWritesVariantAndTemplateBOMsSortedByID(t *testing.T) {
	f, _, err := buildExport(context.Background(), bomSource(), "src", "http://odoo.example", time.Now(), "all")
	require.NoError(t, err)
	assert.Equal(t, "all", f.Section)
	require.Len(t, f.BOMs, 2)

	v := f.BOMs[0]
	assert.Equal(t, 258, v.SourceID)
	require.NotNil(t, v.Product)
	assert.Equal(t, "273001101", v.Product.DefaultCode)
	assert.Empty(t, v.Template.DefaultCodes)
	assert.Equal(t, 1487, v.Template.SourceID)
	assert.Equal(t, "phantom", v.Type)
	assert.Equal(t, 2.0, v.ProductQty)
	assert.Equal(t, "kg", v.UoM)
	assert.True(t, v.Active)
	assert.Equal(t, []FileBOMLine{{Component: "121001090", Qty: 1.2, UoM: "kg"}}, v.Lines)

	tb := f.BOMs[1]
	assert.Equal(t, 259, tb.SourceID)
	assert.Nil(t, tb.Product)
	assert.Equal(t, []string{"A-1", "A-2"}, tb.Template.DefaultCodes, "active, coded variants only")
	assert.False(t, tb.Active)
	assert.Equal(t, "", tb.Lines[0].Component, "a component without a code exports an empty code")
	assert.Empty(t, f.Validate())
}

func TestBuildExportRefusesUnsupportedBOMFeatures(t *testing.T) {
	mutations := map[string]struct {
		mutate func(*odoo.BOM)
		want   string
	}{
		"operations":     {func(b *odoo.BOM) { b.OperationIDs = []int{3} }, "bom 258 has operations"},
		"byproducts":     {func(b *odoo.BOM) { b.ByproductIDs = []int{4} }, "bom 258 has byproducts"},
		"restricted":     {func(b *odoo.BOM) { b.Lines[0].ValueIDs = []int{5} }, "bom 258 has lines restricted by attribute value"},
		"line operation": {func(b *odoo.BOM) { b.Lines[0].OperationID = odoo.Many2One{ID: 9, Name: "Bake"} }, "bom 258 has operations"},
	}
	for name, m := range mutations {
		t.Run(name, func(t *testing.T) {
			src := bomSource()
			src.boms = []odoo.BOM{src.boms[1]}
			src.boms[0].Lines = append([]odoo.BOMLine(nil), src.boms[0].Lines...)
			m.mutate(&src.boms[0])
			_, _, err := buildExport(context.Background(), src, "src", "http://odoo.example", time.Now(), "all")
			require.Error(t, err)
			assert.Contains(t, err.Error(), m.want)
			assert.Contains(t, err.Error(), "does not support")
		})
	}
}

func TestBuildExportFailsOnUnknownComponentVariant(t *testing.T) {
	src := bomSource()
	src.boms = []odoo.BOM{src.boms[1]}
	src.boms[0].Lines = []odoo.BOMLine{{ID: 1, Product: odoo.Many2One{ID: 7777}, Quantity: 1, UoM: odoo.Many2One{Name: "kg"}}}
	_, _, err := buildExport(context.Background(), src, "src", "http://odoo.example", time.Now(), "all")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bom 258")
	assert.Contains(t, err.Error(), "7777")
}

func TestBuildExportIncludeSections(t *testing.T) {
	f, _, err := buildExport(context.Background(), bomSource(), "src", "http://odoo.example", time.Now(), "boms")
	require.NoError(t, err)
	assert.Equal(t, "boms", f.Section)
	assert.Empty(t, f.Templates)
	assert.Empty(t, f.Categories)
	assert.Empty(t, f.Attributes)
	assert.Len(t, f.BOMs, 2)

	src := bomSource()
	src.noBOMs = true
	f, _, err = buildExport(context.Background(), src, "src", "http://odoo.example", time.Now(), "products")
	require.NoError(t, err)
	assert.Equal(t, "products", f.Section)
	assert.Empty(t, f.BOMs)
	assert.NotEmpty(t, f.Templates)
}
