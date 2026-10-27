package odoo

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingHandler answers search_read with one row and write with true, recording write bodies.
func recordingHandler(t *testing.T, model string, writes *[]map[string]any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/json/2/" + model + "/search_read":
			fmt.Fprint(w, `[{"id":21}]`)
		case "/json/2/" + model + "/write":
			*writes = append(*writes, requestParams(t, r))
			fmt.Fprint(w, "true")
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}
}

func TestUpdateProduct(t *testing.T) {
	var writes []map[string]any

	client := orderTestClient(t, recordingHandler(t, "product.product", &writes))
	price, off := 0.0, false
	err := client.UpdateProduct(context.Background(), 21,
		UpdateProductRequest{ListPrice: &price, SaleOK: &off})
	require.NoError(t, err)
	require.Len(t, writes, 1)
	assertJSON(t, writes[0], `{"ids":[21],"vals":{"list_price":0,"sale_ok":false}}`)
}

func TestUpdateProductValidation(t *testing.T) {
	var calls int

	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) { calls++ })
	blank, neg, negID, zeroID := " ", -1.0, -1, 0
	for name, patch := range map[string]UpdateProductRequest{
		"empty":    {},
		"blank":    {Name: &blank},
		"price":    {ListPrice: &neg},
		"category": {CategoryID: &negID},
		"zero cat": {CategoryID: &zeroID}, // categ_id is required in Odoo, it cannot be cleared
	} {
		err := client.UpdateProduct(context.Background(), 21, patch)
		assert.ErrorIs(t, err, ErrInvalidArgument, name)
	}
	assert.Zero(t, calls)
}

func TestUpdateProductNotFound(t *testing.T) {
	var writes int

	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/json/2/product.product/write" {
			writes++
		}
		fmt.Fprint(w, `[]`)
	})
	name := "x"
	err := client.UpdateProduct(context.Background(), 21, UpdateProductRequest{Name: &name})
	assert.ErrorIs(t, err, ErrProductNotFound)
	assert.Zero(t, writes)
}

func TestSetProductActive(t *testing.T) {
	var writes []map[string]any

	client := orderTestClient(t, recordingHandler(t, "product.product", &writes))
	require.NoError(t, client.SetProductActive(context.Background(), 21, false))
	assertJSON(t, writes[0], `{"ids":[21],"vals":{"active":false}}`)

	require.NoError(t, client.SetProductActive(context.Background(), 21, true))
	require.Len(t, writes, 2)
	assertJSON(t, writes[1], `{"ids":[21],"vals":{"active":true}}`)
}

func TestUpdateProductCategory(t *testing.T) {
	var writes []map[string]any

	client := orderTestClient(t, recordingHandler(t, "product.public.category", &writes))
	name, parent := "Shop", 0
	err := client.UpdateProductCategory(context.Background(), WebsiteCategory, 3,
		UpdateProductCategoryRequest{Name: &name, ParentID: &parent})
	require.NoError(t, err)
	assertJSON(t, writes[0], `{"ids":[3],"vals":{"name":"Shop","parent_id":false}}`)

	err = client.UpdateProductCategory(context.Background(), "bad", 3, UpdateProductCategoryRequest{Name: &name})
	assert.ErrorIs(t, err, ErrInvalidArgument)
}
