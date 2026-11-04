package odoo

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOrderPickingsAttachesMovesToTheirPicking(t *testing.T) {
	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		params := requestParams(t, r)
		switch r.URL.Path {
		case "/json/2/sale.order/read":
			assertJSON(t, params, `{"ids":[26],"fields":["picking_ids"]}`)
			fmt.Fprint(w, `[{"id":26,"picking_ids":[16,17]}]`)
		case "/json/2/stock.picking/read":
			assertJSON(t, params["ids"], `[16,17]`)
			fmt.Fprint(w, `[{"id":16,"name":"WH/OUT/1","state":"done","origin":"S26","scheduled_date":"2026-11-04 16:25:21",`+
				`"date_done":"2026-11-05 08:00:00","picking_type_id":[2,"Delivery"],"location_id":[5,"WH/Stock"],`+
				`"location_dest_id":[2,"Customers"]},`+
				`{"id":17,"name":"WH/OUT/2","state":"confirmed","origin":false,"scheduled_date":"2026-11-06 00:00:00",`+
				`"date_done":false,"picking_type_id":[2,"Delivery"],"location_id":[5,"WH/Stock"],"location_dest_id":[2,"Customers"]}]`)
		case "/json/2/stock.move/search_read":
			assertJSON(t, params["domain"], `[["picking_id","in",[16,17]]]`)
			fmt.Fprint(w, `[{"id":35,"picking_id":[17,"WH/OUT/2"],"product_id":[19,"Desk"],"product_uom_qty":10,"quantity":0,`+
				`"product_uom":[1,"Units"],"state":"confirmed"},`+
				`{"id":36,"picking_id":[16,"WH/OUT/1"],"product_id":[20,"Lamp"],"product_uom_qty":2,"quantity":2,`+
				`"product_uom":[1,"Units"],"state":"done"}]`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})
	pickings, err := client.OrderPickings(context.Background(), 26)
	require.NoError(t, err)
	require.Len(t, pickings, 2)
	assert.Equal(t, "done", pickings[0].State)
	assert.Equal(t, "2026-11-05 08:00:00", string(pickings[0].DateDone))
	require.Len(t, pickings[0].Moves, 1)
	assert.Equal(t, 36, pickings[0].Moves[0].ID)
	assert.Empty(t, pickings[1].DateDone)
	require.Len(t, pickings[1].Moves, 1)
	assert.Equal(t, 10.0, pickings[1].Moves[0].Demand)
}

func TestOrderFulfillmentEmptyAndUnknownOrder(t *testing.T) {
	var calls int

	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		fmt.Fprint(w, `[{"id":26,"picking_ids":[],"invoice_ids":[]}]`)
	})
	ctx := context.Background()

	pickings, err := client.OrderPickings(ctx, 26)
	require.NoError(t, err)
	assert.NotNil(t, pickings)
	assert.Empty(t, pickings)
	invoices, err := client.OrderInvoices(ctx, 26)
	require.NoError(t, err)
	assert.Empty(t, invoices)
	payments, err := client.OrderPayments(ctx, 26)
	require.NoError(t, err)
	assert.Empty(t, payments)
	assert.Equal(t, 3, calls, "no further reads when the order has no records")

	missing := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "[]") })
	_, err = missing.OrderInvoices(ctx, 99)
	assert.ErrorIs(t, err, ErrSaleOrderNotFound)

	_, err = client.OrderPayments(ctx, 0)
	assert.ErrorIs(t, err, ErrInvalidArgument)
}

func TestOrderInvoicesAndPaymentsDecodeOdooShapes(t *testing.T) {
	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		params := requestParams(t, r)
		switch r.URL.Path {
		case "/json/2/sale.order/read":
			fmt.Fprint(w, `[{"id":29,"invoice_ids":[84,85]}]`)
		case "/json/2/account.move/read":
			// A draft invoice has name false and no dates.
			fmt.Fprint(w, `[{"id":84,"name":false,"move_type":"out_invoice","state":"draft","payment_state":"not_paid",`+
				`"invoice_date":false,"invoice_date_due":false,"amount_untaxed":20,"amount_tax":0,"amount_total":20,`+
				`"amount_residual":20,"currency_id":[1,"USD"]},`+
				`{"id":85,"name":"INV/2026/00010","move_type":"out_invoice","state":"posted","payment_state":"paid",`+
				`"invoice_date":"2026-11-04","invoice_date_due":"2026-11-04","amount_untaxed":20,"amount_tax":0,`+
				`"amount_total":20,"amount_residual":0,"currency_id":[1,"USD"]}]`)
		case "/json/2/account.payment/search_read":
			assertJSON(t, params["domain"], `[["reconciled_invoice_ids","in",[84,85]]]`)
			fmt.Fprint(w, `[{"id":1,"name":"PBNK1/1","state":"paid","payment_type":"inbound","amount":20,"date":"2026-11-04",`+
				`"currency_id":[1,"USD"],"payment_method_line_id":[1,"Manual"],"memo":false,"reconciled_invoice_ids":[85]}]`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})
	ctx := context.Background()

	invoices, err := client.OrderInvoices(ctx, 29)
	require.NoError(t, err)
	require.Len(t, invoices, 2)
	assert.Empty(t, invoices[0].Name)
	assert.Empty(t, invoices[0].InvoiceDate)
	assert.Equal(t, 20.0, invoices[0].AmountDue)
	assert.Equal(t, "paid", invoices[1].PaymentState)

	payments, err := client.OrderPayments(ctx, 29)
	require.NoError(t, err)
	require.Len(t, payments, 1)
	assert.Equal(t, []int{85}, payments[0].InvoiceIDs)
	assert.Empty(t, payments[0].Memo)
}
