package odoo

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pickingHandler answers the reads around a validation. validate is the body of button_validate;
// state is the delivery state before it. Writes are recorded in order.
func pickingHandler(t *testing.T, state, validate string, writes *[]string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		params := requestParams(t, r)
		switch r.URL.Path {
		case "/json/2/stock.picking/read":
			if fields, _ := params["fields"].([]any); len(fields) == 1 {
				fmt.Fprintf(w, `[{"id":21,"state":%q}]`, state)
				return
			}
			fmt.Fprint(w, `[{"id":21,"name":"WH/OUT/1","state":"done","origin":"S1","scheduled_date":"2026-11-04 10:00:00",`+
				`"date_done":"2026-11-05 08:00:00","picking_type_id":[2,"Delivery"],"location_id":[5,"WH/Stock"],`+
				`"location_dest_id":[2,"Customers"]}]`)
		case "/json/2/stock.move/search_read":
			fmt.Fprint(w, `[{"id":61,"picking_id":[21,"WH/OUT/1"],"product_id":[87,"Tea"],"product_uom_qty":10,"quantity":10,`+
				`"product_uom":[1,"Units"],"state":"done"}]`)
		case "/json/2/stock.picking/button_validate":
			*writes = append(*writes, "button_validate")
			assertJSON(t, params, `{"ids":[21]}`)
			fmt.Fprint(w, validate)
		case "/json/2/stock.backorder.confirmation/create":
			*writes = append(*writes, "wizard.create")
			assert.Equal(t, map[string]any{"button_validate_picking_ids": []any{float64(21)}}, params["context"])
			fmt.Fprint(w, "[5]")
		case "/json/2/stock.backorder.confirmation/process":
			*writes = append(*writes, "wizard.process")
			assertJSON(t, params["ids"], `[5]`)
			fmt.Fprint(w, "true")
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}
}

const backorderAction = `{"name":"Create Backorder?","type":"ir.actions.act_window","res_model":"stock.backorder.confirmation",` +
	`"context":{"button_validate_picking_ids":[21]}}`

func TestValidatePickingReady(t *testing.T) {
	var writes []string

	client := orderTestClient(t, pickingHandler(t, "assigned", "true", &writes))
	picking, err := client.ValidatePicking(context.Background(), 21, ValidatePickingOptions{})
	require.NoError(t, err)
	assert.Equal(t, "done", picking.State)
	require.Len(t, picking.Moves, 1)
	assert.Equal(t, []string{"button_validate"}, writes)
}

func TestValidatePickingBackorder(t *testing.T) {
	var writes []string

	client := orderTestClient(t, pickingHandler(t, "assigned", backorderAction, &writes))
	_, err := client.ValidatePicking(context.Background(), 21, ValidatePickingOptions{Backorder: true})
	require.NoError(t, err)
	assert.Equal(t, []string{"button_validate", "wizard.create", "wizard.process"}, writes)
}

func TestValidatePickingBackorderNotRequested(t *testing.T) {
	var writes []string

	client := orderTestClient(t, pickingHandler(t, "assigned", backorderAction, &writes))
	_, err := client.ValidatePicking(context.Background(), 21, ValidatePickingOptions{})
	assert.ErrorIs(t, err, ErrBackorderRequired)
	assert.Equal(t, []string{"button_validate"}, writes, "the wizard is neither created nor processed")
}

func TestValidatePickingUnhandledWizard(t *testing.T) {
	var writes []string

	other := `{"res_model":"stock.immediate.transfer","context":{}}`
	client := orderTestClient(t, pickingHandler(t, "assigned", other, &writes))
	_, err := client.ValidatePicking(context.Background(), 21, ValidatePickingOptions{Backorder: true})
	assert.ErrorIs(t, err, ErrUnexpectedResponse)
	assert.ErrorContains(t, err, "stock.immediate.transfer")
	assert.Equal(t, []string{"button_validate"}, writes)
}

func TestValidatePickingRejectsBadStateAndUnknownID(t *testing.T) {
	var writes []string

	ctx := context.Background()
	for _, state := range []string{"draft", "confirmed", "done", "cancel"} {
		client := orderTestClient(t, pickingHandler(t, state, "true", &writes))
		_, err := client.ValidatePicking(ctx, 21, ValidatePickingOptions{})
		assert.ErrorIs(t, err, ErrInvalidPickingState, state)
	}
	assert.Empty(t, writes)

	missing := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "[]") })
	_, err := missing.ValidatePicking(ctx, 21, ValidatePickingOptions{})
	assert.ErrorIs(t, err, ErrPickingNotFound)
	_, err = missing.ValidatePicking(ctx, 0, ValidatePickingOptions{})
	assert.ErrorIs(t, err, ErrInvalidArgument)
}

func TestValidatePickingMalformedResult(t *testing.T) {
	var writes []string

	for _, body := range []string{`false`, `"yes"`, `{}`, `{"res_model":false}`} {
		client := orderTestClient(t, pickingHandler(t, "assigned", body, &writes))
		_, err := client.ValidatePicking(context.Background(), 21, ValidatePickingOptions{})
		assert.ErrorIs(t, err, ErrUnexpectedResponse, body)
	}
}
