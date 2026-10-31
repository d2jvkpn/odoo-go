package odoo

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProductAttributeValues(t *testing.T) {
	var (
		client *Client
		got    map[int]ProductAttributeValue
		err    error
	)

	client = orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/json/2/product.template.attribute.value/search_read", r.URL.Path)
		assertJSON(t, requestParams(t, r), `{"domain":[["id","in",[3735,1627]]],`+
			`"fields":["id","attribute_id","name","price_extra"],"limit":2,"order":"id",`+
			`"context":{"active_test":false}}`)
		fmt.Fprint(w, `[{"id":1627,"name":"600g","attribute_id":[2,"Weight"],"price_extra":0.5},`+
			`{"id":3735,"name":"0.5cm","attribute_id":[16,"Width"],"price_extra":0}]`)
	})

	got, err = client.ProductAttributeValues(context.Background(), []int{3735, 1627, 3735})
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, ProductAttributeValue{ID: 1627, Attribute: Many2One{ID: 2, Name: "Weight"},
		Value: "600g", PriceExtra: 0.5}, got[1627])
	assert.Equal(t, "0.5cm", got[3735].Value)
}

func TestProductAttributeValuesEmptyAndInvalid(t *testing.T) {
	var (
		client = NewClient("http://127.0.0.1:1", "key", "")
		got    map[int]ProductAttributeValue
		err    error
	)

	got, err = client.ProductAttributeValues(context.Background(), nil)
	require.NoError(t, err, "no ID means no request")
	assert.Empty(t, got)
	_, err = client.ProductAttributeValues(context.Background(), []int{0})
	assert.ErrorIs(t, err, ErrInvalidArgument)
}
