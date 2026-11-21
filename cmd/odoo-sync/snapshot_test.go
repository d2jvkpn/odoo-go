package main

import (
	"context"
	"iter"
	"testing"

	"github.com/d2jvkpn/odoo-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeSnapshotAPI struct {
	fakeSource
	attrs []odoo.ProductAttribute
	uoms  []odoo.UoM
	cats  []odoo.ProductCategory
}

func (f fakeSnapshotAPI) ListProductAttributes(context.Context) ([]odoo.ProductAttribute, error) {
	return f.attrs, nil
}

func (f fakeSnapshotAPI) ListUoMs(context.Context) ([]odoo.UoM, error) { return f.uoms, nil }

func (f fakeSnapshotAPI) AllProductCategories(
	_ context.Context, _ odoo.ProductCategoryQuery,
) iter.Seq2[odoo.ProductCategory, error] {
	return func(yield func(odoo.ProductCategory, error) bool) {
		for _, c := range f.cats {
			if !yield(c, nil) {
				return
			}
		}
	}
}

func TestLoadSnapshotIndexesTheTarget(t *testing.T) {
	api := fakeSnapshotAPI{
		fakeSource: fakeSource{
			variants: []odoo.CatalogVariant{sourceVariant(1, 5001, "x", "A-1", []int{30}, true)},
			values:   map[int]odoo.ProductAttributeValue{30: {ID: 30, Value: "1kg"}},
		},
		attrs: []odoo.ProductAttribute{{ID: 3, Name: "规格", CreateVariant: "always", Active: true, Values: []odoo.ProductAttributeOption{
			{ID: 10, Name: "1kg", Active: true}, {ID: 12, Name: "500g"},
		}}},
		uoms: []odoo.UoM{{ID: 1, Name: "kg"}},
		cats: []odoo.ProductCategory{{ID: 7, Name: "番茄类", CompleteName: "蔬菜 / 番茄类"}},
	}
	snap, err := loadSnapshot(context.Background(), api, false)
	require.NoError(t, err)
	assert.Equal(t, map[string]int{"蔬菜 / 番茄类": 7}, snap.Categories)
	assert.Equal(t, map[string]SnapAttribute{"规格": {
		ID: 3, CreateVariant: "always", Values: map[string]int{"1kg": 10}, ArchivedValues: map[string]bool{"500g": true},
	}}, snap.Attributes)
	assert.Equal(t, map[string]int{"kg": 1}, snap.UoMs)
	assert.Len(t, snap.Variants, 1)
	assert.Equal(t, "1kg", snap.Values[30].Value)
}

func TestLoadSnapshotReadsBOMsOnlyWhenAsked(t *testing.T) {
	boms := []odoo.BOM{{ID: 12, Type: "normal", Active: true}, {ID: 13, Active: false}}
	api := fakeSnapshotAPI{fakeSource: fakeSource{boms: boms, values: map[int]odoo.ProductAttributeValue{}}}

	snap, err := loadSnapshot(context.Background(), api, true)
	require.NoError(t, err)
	assert.Equal(t, boms, snap.BOMs)

	api.noBOMs = true
	snap, err = loadSnapshot(context.Background(), api, false)
	require.NoError(t, err)
	assert.Nil(t, snap.BOMs)
}
