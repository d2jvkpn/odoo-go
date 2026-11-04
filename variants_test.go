package odoo

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAddProductVariantsSendsThreeWritesThenReadsVariants(t *testing.T) {
	var paths []string

	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		params := requestParams(t, r)
		switch r.URL.Path {
		case "/json/2/product.template/search_read":
			fmt.Fprint(w, `[{"id":45}]`)
		case "/json/2/product.attribute/create":
			assertJSON(t, params, `{"vals_list":[{"name":"Weight","create_variant":"always"}]}`)
			fmt.Fprint(w, "[12]")
		case "/json/2/product.attribute.value/create":
			assertJSON(t, params, `{"vals_list":[{"name":"500g","attribute_id":12},{"name":"1kg","attribute_id":12}]}`)
			fmt.Fprint(w, "[102,103]")
		case "/json/2/product.template.attribute.line/create":
			assertJSON(t, params, `{"vals_list":[{"product_tmpl_id":45,"attribute_id":12,"value_ids":[[6,0,[102,103]]]}]}`)
			fmt.Fprint(w, "[7]")
		case "/json/2/product.product/search_read":
			assertJSON(t, params["domain"], `[["product_tmpl_id","=",45]]`)
			fmt.Fprint(w, `[{"id":67,"display_name":"Tea (500g)","product_template_variant_value_ids":[29]},`+
				`{"id":68,"display_name":"Tea (1kg)","product_template_variant_value_ids":[30]}]`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})
	variants, err := client.AddProductVariants(context.Background(), 45, "Weight", []string{"500g", "1kg"})
	require.NoError(t, err)
	require.Len(t, variants, 2)
	assert.Equal(t, 67, variants[0].ID)
	assert.Equal(t, []int{30}, variants[1].ValueIDs)
	assert.Len(t, paths, 5)
}

func TestAddProductVariantsNamesCreatedIDsOnLaterFailure(t *testing.T) {
	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/json/2/product.template/search_read":
			fmt.Fprint(w, `[{"id":45}]`)
		case "/json/2/product.attribute/create":
			fmt.Fprint(w, "[12]")
		case "/json/2/product.attribute.value/create":
			fmt.Fprint(w, "[102]")
		default:
			http.Error(w, `{"message":"boom"}`, http.StatusInternalServerError)
		}
	})
	_, err := client.AddProductVariants(context.Background(), 45, "Weight", []string{"500g"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "product.attribute 12")
	assert.Contains(t, err.Error(), "template 45")
}

func TestVariantWritesRejectBadInputWithoutRequests(t *testing.T) {
	var calls int

	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) { calls++ })
	ctx := context.Background()

	_, err := client.CreateProductAttribute(ctx, " ")
	assert.Error(t, err)
	_, err = client.CreateProductAttributeValues(ctx, 0, []string{"a"})
	assert.Error(t, err)
	_, err = client.CreateProductAttributeValues(ctx, 1, nil)
	assert.Error(t, err)
	_, err = client.CreateProductAttributeValues(ctx, 1, []string{"a", "a"})
	assert.Error(t, err)
	_, err = client.CreateProductAttributeValues(ctx, 1, []string{" "})
	assert.Error(t, err)
	_, err = client.AddProductAttributeLine(ctx, 0, 1, []int{1})
	assert.Error(t, err)
	_, err = client.AddProductAttributeLine(ctx, 1, 1, nil)
	assert.Error(t, err)
	_, err = client.AddProductAttributeLine(ctx, 1, 1, []int{0})
	assert.Error(t, err)
	_, err = client.AddProductVariants(ctx, 1, "", []string{"a"})
	assert.Error(t, err)
	_, err = client.AddProductVariants(ctx, 1, "Weight", nil)
	assert.Error(t, err)
	_, err = client.ProductVariants(ctx, 0)
	assert.Error(t, err)
	assert.Error(t, client.SetVariantPriceExtra(ctx, 0, 1))
	assert.Zero(t, calls)
}

func TestCreateRecordsRejectsCountMismatch(t *testing.T) {
	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "[]") })
	_, err := client.CreateProductAttribute(context.Background(), "Weight")
	assert.ErrorIs(t, err, ErrUnexpectedResponse)
}

func TestSetVariantPriceExtra(t *testing.T) {
	var writes []map[string]any

	client := orderTestClient(t, recordingHandler(t, "product.template.attribute.value", &writes))
	require.NoError(t, client.SetVariantPriceExtra(context.Background(), 21, 0))
	require.Len(t, writes, 1)
	assertJSON(t, writes[0], `{"ids":[21],"vals":{"price_extra":0}}`)
}

func TestSetVariantPriceExtraUnknownID(t *testing.T) {
	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "[]") })
	err := client.SetVariantPriceExtra(context.Background(), 21, 2.5)
	assert.ErrorIs(t, err, ErrAttributeValueNotFound)
}

func TestAddProductVariantsUnknownTemplateWritesNothing(t *testing.T) {
	var writes int

	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/json/2/product.template/search_read" {
			writes++
		}
		fmt.Fprint(w, "[]")
	})
	_, err := client.AddProductVariants(context.Background(), 45, "Weight", []string{"500g"})
	assert.ErrorIs(t, err, ErrProductNotFound)
	assert.Zero(t, writes)
}
