package odoo

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetProductStock(t *testing.T) {
	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/json/2/product.product/search_read", r.URL.Path)
		params := requestParams(t, r)
		assertJSON(t, params["domain"], `[["id","in",[21,22,99]]]`)
		assertJSON(t, params["fields"], `["id","qty_available","virtual_available","free_qty"]`)
		assert.Equal(t, map[string]any{"active_test": false}, params["context"])
		fmt.Fprint(w, `[{"id":21,"qty_available":5,"virtual_available":3,"free_qty":2.5},`+
			`{"id":22,"qty_available":0,"virtual_available":0,"free_qty":0}]`)
	})
	got, err := client.GetProductStock(context.Background(), []int{21, 22, 99})
	require.NoError(t, err)
	assert.Equal(t, []ProductStock{
		{ID: 21, QtyAvailable: 5, VirtualAvailable: 3, FreeQty: 2.5},
		{ID: 22},
	}, got) // ID 99 does not exist and is simply omitted
}

func TestGetProductStockEdges(t *testing.T) {
	var calls int

	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) { calls++ })
	got, err := client.GetProductStock(context.Background(), nil)
	require.NoError(t, err)
	assert.NotNil(t, got)
	assert.Empty(t, got)

	_, err = client.GetProductStock(context.Background(), []int{1, 0})
	assert.ErrorIs(t, err, ErrInvalidArgument)

	ids := make([]int, maxStockIDs+1)
	for i := range ids {
		ids[i] = i + 1
	}
	_, err = client.GetProductStock(context.Background(), ids)
	assert.ErrorIs(t, err, ErrInvalidArgument)
	assert.Zero(t, calls)
}
