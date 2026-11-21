package odoo

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateProductTemplateSendsAllFieldsAndReturnsTemplateID(t *testing.T) {
	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/json/2/product.template/create", r.URL.Path)
		assertJSON(t, requestParams(t, r), `{"vals_list":[{"name":"冬枣散装","list_price":9.5,"categ_id":7,`+
			`"sale_ok":true,"is_storable":true,"type":"consu","uom_id":1,"description_sale":"fresh"}]}`)
		fmt.Fprint(w, "[1487]")
	})
	id, err := client.CreateProductTemplate(context.Background(), CreateTemplateRequest{
		Name: "冬枣散装", ListPrice: 9.5, CategoryID: 7, SaleOK: true, IsStorable: true, Type: "consu", UoMID: 1,
		Description: "fresh",
	})
	require.NoError(t, err)
	assert.Equal(t, 1487, id)
}

func TestCreateProductTemplateOmitsUnsetRelationsAndText(t *testing.T) {
	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assertJSON(t, requestParams(t, r), `{"vals_list":[{"name":"x","list_price":0,"sale_ok":false,"is_storable":false}]}`)
		fmt.Fprint(w, "[5]")
	})
	_, err := client.CreateProductTemplate(context.Background(), CreateTemplateRequest{Name: "x"})
	require.NoError(t, err)
}

func TestCatalogWritesRejectBadInputWithoutRequests(t *testing.T) {
	var calls int

	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) { calls++ })
	ctx := context.Background()
	blank, negative, nan := " ", -1.0, 0.0
	nan = nan / nan

	_, err := client.CreateProductTemplate(ctx, CreateTemplateRequest{Name: blank})
	assert.ErrorIs(t, err, ErrInvalidArgument)
	_, err = client.CreateProductTemplate(ctx, CreateTemplateRequest{Name: "x", ListPrice: negative})
	assert.ErrorIs(t, err, ErrInvalidArgument)
	_, err = client.CreateProductTemplate(ctx, CreateTemplateRequest{Name: "x", CategoryID: -1})
	assert.ErrorIs(t, err, ErrInvalidArgument)
	assert.ErrorIs(t, client.UpdateProductTemplate(ctx, 1, TemplatePatch{Name: &blank}), ErrInvalidArgument)
	assert.ErrorIs(t, client.UpdateProductTemplate(ctx, 1, TemplatePatch{ListPrice: &nan}), ErrInvalidArgument)
	assert.ErrorIs(t, client.UpdateProductTemplate(ctx, 0, TemplatePatch{Name: new(string)}), ErrInvalidArgument)
	assert.ErrorIs(t, client.UpdateProductTemplate(ctx, 1, TemplatePatch{}), ErrInvalidArgument)
	assert.ErrorIs(t, client.UpdateVariantFields(ctx, 1, VariantPatch{Weight: &negative}), ErrInvalidArgument)
	assert.ErrorIs(t, client.UpdateVariantFields(ctx, 1, VariantPatch{}), ErrInvalidArgument)
	assert.ErrorIs(t, client.SetProductTemplateActive(ctx, 0, true), ErrInvalidArgument)
	assert.Zero(t, calls)
}

func TestUpdateProductTemplateWritesOnlySetFields(t *testing.T) {
	var writes []map[string]any

	client := orderTestClient(t, recordingHandler(t, "product.template", &writes))
	name, price, cat := "新名", 2.0, 9
	require.NoError(t, client.UpdateProductTemplate(context.Background(), 21, TemplatePatch{
		Name: &name, ListPrice: &price, CategoryID: &cat,
	}))
	require.Len(t, writes, 1)
	assertJSON(t, writes[0], `{"ids":[21],"vals":{"name":"新名","list_price":2,"categ_id":9}}`)
}

func TestUpdateProductTemplateUnknownID(t *testing.T) {
	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "[]") })
	name := "x"
	err := client.UpdateProductTemplate(context.Background(), 21, TemplatePatch{Name: &name})
	assert.ErrorIs(t, err, ErrProductNotFound)
}

func TestSetProductTemplateActive(t *testing.T) {
	var writes []map[string]any

	client := orderTestClient(t, recordingHandler(t, "product.template", &writes))
	require.NoError(t, client.SetProductTemplateActive(context.Background(), 21, false))
	require.Len(t, writes, 1)
	assertJSON(t, writes[0], `{"ids":[21],"vals":{"active":false}}`)
}

func TestUpdateVariantFieldsWritesOnlySetFields(t *testing.T) {
	var writes []map[string]any

	client := orderTestClient(t, recordingHandler(t, "product.product", &writes))
	code, weight := "273001101", 0.5
	require.NoError(t, client.UpdateVariantFields(context.Background(), 21, VariantPatch{DefaultCode: &code, Weight: &weight}))
	require.Len(t, writes, 1)
	assertJSON(t, writes[0], `{"ids":[21],"vals":{"default_code":"273001101","weight":0.5}}`)
}
