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
}

func (f fakeSource) ListCatalogVariants(context.Context, bool) ([]odoo.CatalogVariant, error) {
	return f.variants, nil
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
	f, omitted, err := buildExport(context.Background(), src, "src", "http://odoo.example", now)
	require.NoError(t, err)
	assert.Zero(t, omitted)

	assert.Equal(t, formatVersion, f.Format)
	assert.Equal(t, "2026-11-21T10:00:00Z", f.ExportedAt)
	assert.Equal(t, []string{"products"}, f.Include)
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
	_, _, err := buildExport(context.Background(), src, "src", "http://odoo.example", time.Now())
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
	f, omitted, err := buildExport(context.Background(), src, "src", "http://odoo.example", time.Now())
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
