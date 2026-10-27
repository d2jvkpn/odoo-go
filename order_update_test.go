package odoo

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateSaleOrder(t *testing.T) {
	var writes []map[string]any

	client := orderTestClient(t, recordingHandler(t, "sale.order", &writes))
	origin, note, ref := "miniapp", "", "WX1"
	err := client.UpdateSaleOrder(context.Background(), 21,
		UpdateSaleOrderRequest{Origin: &origin, Note: &note, ClientOrderRef: &ref})
	require.NoError(t, err)
	require.Len(t, writes, 1)
	// a pointer to "" is sent, so a note can be cleared
	assertJSON(t, writes[0], `{"ids":[21],"vals":{"origin":"miniapp","note":"","client_order_ref":"WX1"}}`)
}

func TestUpdateSaleOrderRejectsWithoutWriting(t *testing.T) {
	var writes int

	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/json/2/sale.order/write" {
			writes++
		}
		fmt.Fprint(w, `[]`) // search_read finds nothing
	})
	origin := "web"

	err := client.UpdateSaleOrder(context.Background(), 21, UpdateSaleOrderRequest{Origin: &origin})
	assert.ErrorIs(t, err, ErrSaleOrderNotFound)
	err = client.UpdateSaleOrder(context.Background(), 21, UpdateSaleOrderRequest{})
	assert.ErrorIs(t, err, ErrInvalidArgument)
	err = client.UpdateSaleOrder(context.Background(), 0, UpdateSaleOrderRequest{Origin: &origin})
	assert.ErrorIs(t, err, ErrInvalidArgument)
	assert.Zero(t, writes)
}
