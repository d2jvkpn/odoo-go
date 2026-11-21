package odoo

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListCatalogVariantsDecodesUnsetFieldsAndReadsArchivedOnRequest(t *testing.T) {
	var params map[string]any

	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/json/2/product.product/search_read", r.URL.Path)
		params = requestParams(t, r)
		fmt.Fprint(w, `[{"id":10688,"product_tmpl_id":[1487,"冬枣散装"],"name":"冬枣散装","default_code":"273001101",`+
			`"list_price":9.5,"categ_id":[7,"水果 / 枣类"],"sale_ok":true,"is_storable":true,"type":"consu",`+
			`"uom_id":[1,"kg"],"description_sale":false,"barcode":false,"weight":0.0,"volume":0.0,`+
			`"product_template_variant_value_ids":[],"active":false}]`)
	})
	variants, err := client.ListCatalogVariants(context.Background(), true)
	require.NoError(t, err)
	require.Len(t, variants, 1)
	v := variants[0]
	assert.Equal(t, 10688, v.ID)
	assert.Equal(t, 1487, v.Template.ID)
	assert.Equal(t, OptionalString("273001101"), v.DefaultCode)
	assert.Equal(t, "水果 / 枣类", v.Category.Name)
	assert.Equal(t, "kg", v.UoM.Name)
	assert.Empty(t, v.Description)
	assert.Empty(t, v.Barcode)
	assert.Empty(t, v.ValueIDs)
	assert.False(t, v.Active)
	assertJSON(t, params["context"], `{"active_test":false}`)
	assertJSON(t, params["domain"], `[]`)
}

func TestListCatalogVariantsOmitsArchivedContextByDefault(t *testing.T) {
	var params map[string]any

	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		params = requestParams(t, r)
		fmt.Fprint(w, `[]`)
	})
	variants, err := client.ListCatalogVariants(context.Background(), false)
	require.NoError(t, err)
	assert.Empty(t, variants)
	assert.NotContains(t, params, "context")
}

func TestListCatalogVariantsReadsEveryPage(t *testing.T) {
	page := func(n, start int) string {
		parts := make([]string, n)
		for i := range parts {
			parts[i] = fmt.Sprintf(`{"id":%d,"product_tmpl_id":[1,"t"],"name":"t","categ_id":false,"uom_id":false,"active":true}`, start+i)
		}
		return "[" + strings.Join(parts, ",") + "]"
	}
	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch int(requestParams(t, r)["offset"].(float64)) {
		case 0:
			fmt.Fprint(w, page(catalogPageSize, 1))
		case catalogPageSize:
			fmt.Fprint(w, page(3, catalogPageSize+1))
		default:
			t.Errorf("unexpected offset")
		}
	})
	variants, err := client.ListCatalogVariants(context.Background(), false)
	require.NoError(t, err)
	assert.Len(t, variants, catalogPageSize+3)
}

func TestListProductAttributesGroupsValuesByAttributeAndReadsArchivedOnes(t *testing.T) {
	contexts := map[string]any{}

	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		contexts[r.URL.Path] = requestParams(t, r)["context"]
		switch r.URL.Path {
		case "/json/2/product.attribute/search_read":
			fmt.Fprint(w, `[{"id":3,"name":"规格","create_variant":"always","active":true},`+
				`{"id":4,"name":"颜色","create_variant":"no_variant","active":false}]`)
		case "/json/2/product.attribute.value/search_read":
			fmt.Fprint(w, `[{"id":10,"name":"1kg","attribute_id":[3,"规格"],"active":true},`+
				`{"id":11,"name":"1.5kg","attribute_id":[3,"规格"],"active":false}]`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})
	attrs, err := client.ListProductAttributes(context.Background())
	require.NoError(t, err)
	require.Len(t, attrs, 2)
	assert.Equal(t, "规格", attrs[0].Name)
	assert.Equal(t, "always", attrs[0].CreateVariant)
	assert.True(t, attrs[0].Active)
	assert.Equal(t, []ProductAttributeOption{{ID: 10, Name: "1kg", Active: true}, {ID: 11, Name: "1.5kg"}}, attrs[0].Values)
	assert.Equal(t, "no_variant", attrs[1].CreateVariant)
	assert.False(t, attrs[1].Active)
	assert.Empty(t, attrs[1].Values)
	for path, ctx := range contexts {
		assertJSON(t, ctx, `{"active_test":false}`)
		_ = path
	}
	assert.Len(t, contexts, 2)
}

func TestListUoMs(t *testing.T) {
	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/json/2/uom.uom/search_read", r.URL.Path)
		fmt.Fprint(w, `[{"id":1,"name":"kg"},{"id":2,"name":"Units"}]`)
	})
	uoms, err := client.ListUoMs(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []UoM{{ID: 1, Name: "kg"}, {ID: 2, Name: "Units"}}, uoms)
}
