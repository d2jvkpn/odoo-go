package odoo

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSearchProducts(t *testing.T) {
	var (
		client   *Client
		products []Product
		err      error
	)

	client = orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/json/2/product.product/search_read", r.URL.Path)
		assertJSON(t, requestParams(t, r), `{"domain":[["sale_ok","=",true]],`+
			`"fields":["id","name","default_code","list_price","categ_id","sale_ok","is_storable"],"limit":5,"offset":10,"order":"name"}`)
		fmt.Fprint(w, `[{"id":7,"name":"Desk","default_code":false,"list_price":100.5,`+
			`"categ_id":[3,"Furniture"],"sale_ok":true,"is_storable":false}]`)
	})

	products, err = client.SearchProducts(context.Background(), ProductQuery{
		Domain: []any{[]any{"sale_ok", "=", true}},
		Limit:  5,
		Offset: 10,
		Order:  "name",
	})
	require.NoError(t, err)
	assertJSON(t, products, `[{"id":7,"name":"Desk","default_code":"","list_price":100.5,`+
		`"categ_id":{"id":3,"name":"Furniture"},"sale_ok":true,"is_storable":false}]`)
}

func TestSearchProductsDefaultsAndEmpty(t *testing.T) {
	var (
		client   *Client
		products []Product
		err      error
	)

	client = orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		var params map[string]any

		params = requestParams(t, r)
		assertJSON(t, params["domain"], `[]`)
		assert.Equal(t, float64(100), params["limit"])
		assert.Equal(t, float64(0), params["offset"])
		assert.Equal(t, "id", params["order"])
		fmt.Fprint(w, `[]`)
	})

	products, err = client.SearchProducts(context.Background(), ProductQuery{})
	require.NoError(t, err)
	require.NotNil(t, products)
	assert.Empty(t, products)
}

func TestSearchProductsRejectsNegativePaging(t *testing.T) {
	var (
		client *Client
		err    error
	)

	client = NewClient("http://127.0.0.1:1", "key", "")

	for _, q := range []ProductQuery{{Limit: -1}, {Offset: -1}} {
		_, err = client.SearchProducts(context.Background(), q)
		assert.Error(t, err, "expected error for %+v", q)
	}
}

func TestSearchProductsRemoteError(t *testing.T) {
	var (
		client *Client
		err    error
	)

	client = orderTestClient(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(403) })

	_, err = client.SearchProducts(context.Background(), ProductQuery{})
	assert.Error(t, err, "expected remote error")
}

func TestGetProduct(t *testing.T) {
	var (
		client  *Client
		product *Product
		err     error
	)

	client = orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		var params map[string]any

		params = requestParams(t, r)
		assertJSON(t, params["domain"], `[["id","=",7]]`)
		assert.Equal(t, float64(1), params["limit"])
		fmt.Fprint(w, `[{"id":7,"name":"Desk","default_code":"D-1","list_price":100,"categ_id":false}]`)
	})

	product, err = client.GetProduct(context.Background(), 7)
	require.NoError(t, err)
	assert.Equal(t, 7, product.ID)
	assert.Equal(t, "D-1", string(product.DefaultCode))
	assert.Equal(t, 0, product.Category.ID)
}

func TestGetProductNotFoundAndInvalidID(t *testing.T) {
	var (
		client *Client
		err    error
	)

	client = orderTestClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `[]`) })

	_, err = client.GetProduct(context.Background(), 9)
	assert.ErrorIs(t, err, ErrProductNotFound)

	_, err = client.GetProduct(context.Background(), 0)
	assert.Error(t, err, "expected invalid ID error")
}
