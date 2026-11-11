package odoo

import (
	"context"
	jsonv2 "encoding/json/v2"
	"fmt"
	"math"
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

func lineUpdateHandler(t *testing.T, lineIDs string, writes *[]map[string]any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/json/2/sale.order/search_read":
			assertJSON(t, requestParams(t, r)["fields"], `["order_line"]`)
			fmt.Fprintf(w, `[{"order_line":%s}]`, lineIDs)
		case "/json/2/sale.order/write":
			*writes = append(*writes, requestParams(t, r))
			fmt.Fprint(w, "true")
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}
}

func TestUpdateSaleOrderLines(t *testing.T) {
	var writes []map[string]any

	client := orderTestClient(t, lineUpdateHandler(t, `[11,12,13]`, &writes))
	name, qty, price, zero := "Hardware", 3.0, 9.5, 0.0
	ref := "WX2"
	err := client.UpdateSaleOrder(context.Background(), 21, UpdateSaleOrderRequest{
		ClientOrderRef: &ref,
		Lines: &SaleOrderLineChanges{
			Update: []UpdateSaleOrderLine{{ID: 11, Quantity: &qty, UnitPrice: &price}, {ID: 12, Name: &name, UnitPrice: &zero}},
			Remove: []int{13},
			Add: []CreateSaleOrderLine{
				{DisplayType: LineSection, Name: "Extras"},
				{ProductID: 5, Quantity: 1},
				{DisplayType: LineNote, Name: "Fragile"},
			},
		},
	})
	require.NoError(t, err)
	require.Len(t, writes, 1)
	assertJSON(t, writes[0], `{"ids":[21],"vals":{"client_order_ref":"WX2","order_line":[`+
		`[1,11,{"product_uom_qty":3,"price_unit":9.5}],`+
		`[1,12,{"name":"Hardware","price_unit":0}],`+
		`[2,13],`+
		`[0,0,{"display_type":"line_section","name":"Extras"}],`+
		`[0,0,{"product_id":5,"product_uom_qty":1}],`+
		`[0,0,{"display_type":"line_note","name":"Fragile"}]]}}`)
}

func TestUpdateSaleOrderLinesRejectsWithoutWriting(t *testing.T) {
	var writes []map[string]any

	client := orderTestClient(t, lineUpdateHandler(t, `[11]`, &writes))
	empty, name, zero, nan := "", "x", 0.0, math.NaN()

	for _, c := range []struct {
		id      int
		changes SaleOrderLineChanges
		want    error
	}{
		{21, SaleOrderLineChanges{}, ErrInvalidArgument},
		{0, SaleOrderLineChanges{Remove: []int{11}}, ErrInvalidArgument},
		{21, SaleOrderLineChanges{Remove: []int{0}}, ErrInvalidArgument},
		{21, SaleOrderLineChanges{Update: []UpdateSaleOrderLine{{ID: 11}}}, ErrInvalidArgument},
		{21, SaleOrderLineChanges{Update: []UpdateSaleOrderLine{{ID: 0, Name: &name}}}, ErrInvalidArgument},
		{21, SaleOrderLineChanges{Update: []UpdateSaleOrderLine{{ID: 11, Name: &empty}}}, ErrInvalidArgument},
		{21, SaleOrderLineChanges{Update: []UpdateSaleOrderLine{{ID: 11, Quantity: &zero}}}, ErrInvalidArgument},
		{21, SaleOrderLineChanges{Update: []UpdateSaleOrderLine{{ID: 11, UnitPrice: &nan}}}, ErrInvalidArgument},
		{21, SaleOrderLineChanges{Update: []UpdateSaleOrderLine{{ID: 11, Name: &name}}, Remove: []int{11}}, ErrInvalidArgument},
		{21, SaleOrderLineChanges{Add: []CreateSaleOrderLine{{DisplayType: LineNote}}}, ErrInvalidArgument},
		{21, SaleOrderLineChanges{Remove: []int{99}}, ErrInvalidArgument}, // not a line of this order
		{21, SaleOrderLineChanges{Update: []UpdateSaleOrderLine{{ID: 99, Name: &name}}}, ErrInvalidArgument},
	} {
		changes := c.changes
		err := client.UpdateSaleOrder(context.Background(), c.id, UpdateSaleOrderRequest{Lines: &changes})
		assert.ErrorIs(t, err, c.want, "%+v", c.changes)
	}
	assert.Empty(t, writes)
}

func TestUpdateSaleOrderLinesUnknownOrder(t *testing.T) {
	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/json/2/sale.order/search_read", r.URL.Path)
		fmt.Fprint(w, `[]`)
	})
	err := client.UpdateSaleOrder(context.Background(), 21,
		UpdateSaleOrderRequest{Lines: &SaleOrderLineChanges{Remove: []int{1}}})
	assert.ErrorIs(t, err, ErrSaleOrderNotFound)
}

func TestUpdateSaleOrderRequestDecodesLineChangesFromFile(t *testing.T) {
	var (
		patch  UpdateSaleOrderRequest
		writes []map[string]any
	)

	require.NoError(t, jsonv2.Unmarshal([]byte(`{"order_line":{"add":[{"display_type":"line_note","name":"Fragile"}],`+
		`"update":[{"id":11,"product_uom_qty":3}],"remove":[13]}}`), &patch))
	client := orderTestClient(t, lineUpdateHandler(t, `[11,13]`, &writes))
	require.NoError(t, client.UpdateSaleOrder(context.Background(), 21, patch))
	require.Len(t, writes, 1)
	assertJSON(t, writes[0], `{"ids":[21],"vals":{"order_line":[[1,11,{"product_uom_qty":3}],[2,13],`+
		`[0,0,{"display_type":"line_note","name":"Fragile"}]]}}`)
}

func TestUpdateSaleOrderLinesAllowsNegativeQuantityAndPrice(t *testing.T) {
	var writes []map[string]any

	client := orderTestClient(t, lineUpdateHandler(t, `[11]`, &writes))
	qty, price := -1.0, -2.5
	require.NoError(t, client.UpdateSaleOrder(context.Background(), 21, UpdateSaleOrderRequest{
		Lines: &SaleOrderLineChanges{Update: []UpdateSaleOrderLine{{ID: 11, Quantity: &qty, UnitPrice: &price}}},
	}))
	assertJSON(t, writes[0], `{"ids":[21],"vals":{"order_line":[[1,11,{"product_uom_qty":-1,"price_unit":-2.5}]]}}`)
}

func TestUpdateSaleOrderLineDiscount(t *testing.T) {
	var writes []map[string]any

	client := orderTestClient(t, lineUpdateHandler(t, `[11,12]`, &writes))
	ten, clear, bad := 10.0, 0.0, 101.0
	require.NoError(t, client.UpdateSaleOrder(context.Background(), 21, UpdateSaleOrderRequest{
		Lines: &SaleOrderLineChanges{Update: []UpdateSaleOrderLine{{ID: 11, Discount: &ten}, {ID: 12, Discount: &clear}}},
	}))
	assertJSON(t, writes[0], `{"ids":[21],"vals":{"order_line":[[1,11,{"discount":10}],[1,12,{"discount":0}]]}}`)

	err := client.UpdateSaleOrder(context.Background(), 21, UpdateSaleOrderRequest{
		Lines: &SaleOrderLineChanges{Update: []UpdateSaleOrderLine{{ID: 11, Discount: &bad}}},
	})
	assert.ErrorIs(t, err, ErrInvalidArgument)
	assert.Len(t, writes, 1)
}
