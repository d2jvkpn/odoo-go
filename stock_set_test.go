package odoo

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stockSetHandler answers the three calls SetProductStock makes and records their paths.
func stockSetHandler(t *testing.T, storable string, paths *[]string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		*paths = append(*paths, r.URL.Path)
		switch r.URL.Path {
		case "/json/2/product.product/search_read":
			params := requestParams(t, r)
			assertJSON(t, params["domain"], `[["id","=",21]]`)
			assertJSON(t, params["fields"], `["is_storable"]`)
			assert.Equal(t, map[string]any{"active_test": false}, params["context"])
			fmt.Fprint(w, storable)
		case "/json/2/stock.quant/create":
			assertJSON(t, requestParams(t, r), `{"vals_list":[{"product_id":21,"location_id":5,`+
				`"inventory_quantity":0}],"context":{"inventory_mode":true}}`)
			fmt.Fprint(w, "[9]")
		case "/json/2/stock.quant/action_apply_inventory":
			assertJSON(t, requestParams(t, r), `{"ids":[9],"context":{"inventory_mode":true}}`)
			fmt.Fprint(w, "null")
		}
	}
}

func TestSetProductStock(t *testing.T) {
	var paths []string

	client := orderTestClient(t, stockSetHandler(t, `[{"is_storable":true}]`, &paths))
	// zero is a real quantity (sold out) and must be sent, not omitted
	require.NoError(t, client.SetProductStock(context.Background(), 21, 5, 0))
	assert.Equal(t, []string{
		"/json/2/product.product/search_read",
		"/json/2/stock.quant/create",
		"/json/2/stock.quant/action_apply_inventory",
	}, paths)
}

func TestSetProductStockRejectsWithoutWriting(t *testing.T) {
	for name, tc := range map[string]struct {
		storable        string
		product, loc    int
		qty             float64
		want            error
		wantRequestPath string
	}{
		"not storable": {`[{"is_storable":false}]`, 21, 5, 1, ErrProductNotStorable, "read"},
		"no product":   {`[]`, 21, 5, 1, ErrProductNotFound, "read"},
		"bad product":  {`[]`, 0, 5, 1, ErrInvalidArgument, ""},
		"bad location": {`[]`, 21, 0, 1, ErrInvalidArgument, ""},
		"negative":     {`[]`, 21, 5, -1, ErrInvalidArgument, ""},
		"nan":          {`[]`, 21, 5, math.NaN(), ErrInvalidArgument, ""},
		"inf":          {`[]`, 21, 5, math.Inf(1), ErrInvalidArgument, ""},
	} {
		t.Run(name, func(t *testing.T) {
			var paths []string

			client := orderTestClient(t, stockSetHandler(t, tc.storable, &paths))
			err := client.SetProductStock(context.Background(), tc.product, tc.loc, tc.qty)
			assert.ErrorIs(t, err, tc.want)
			for _, p := range paths {
				assert.Equal(t, "/json/2/product.product/search_read", p) // never a write
			}
			if tc.wantRequestPath == "" {
				assert.Empty(t, paths) // invalid input is rejected before any request
			}
		})
	}
}

func TestSetProductStockIsNotRetried(t *testing.T) {
	var creates, applies int

	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/json/2/product.product/search_read":
			fmt.Fprint(w, `[{"is_storable":true}]`)
		case "/json/2/stock.quant/create":
			creates++
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
		default:
			applies++
		}
	})
	client.Retry = RetryPolicy{MaxRetries: 3}

	err := client.SetProductStock(context.Background(), 21, 5, 4)
	var httpErr *HTTPError
	require.ErrorAs(t, err, &httpErr)
	assert.Equal(t, 1, creates)
	assert.Zero(t, applies) // a failed create must not be followed by an apply
}

func TestSetProductStockReportsPendingAdjustment(t *testing.T) {
	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/json/2/product.product/search_read":
			fmt.Fprint(w, `[{"is_storable":true}]`)
		case "/json/2/stock.quant/create":
			fmt.Fprint(w, "[9]")
		default:
			http.Error(w, "boom", http.StatusInternalServerError)
		}
	})
	err := client.SetProductStock(context.Background(), 21, 5, 4)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "quant 9") // names the quant so it can be inspected in Odoo
}

func TestListStockLocations(t *testing.T) {
	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/json/2/stock.location/search_read", r.URL.Path)
		params := requestParams(t, r)
		assertJSON(t, params["domain"], `[["usage","=","internal"]]`)
		assertJSON(t, params["fields"], `["id","complete_name"]`)
		fmt.Fprint(w, `[{"id":5,"complete_name":"WH/Stock"}]`)
	})
	got, err := client.ListStockLocations(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []StockLocation{{ID: 5, Name: "WH/Stock"}}, got)
}

func TestIsStorableIsSentOnCreateAndUpdate(t *testing.T) {
	on := true
	assertJSON(t, CreateProductRequest{Name: "A", IsStorable: &on}, `{"name":"A","is_storable":true}`)
	assertJSON(t, CreateProductRequest{Name: "A"}, `{"name":"A"}`)

	var writes []map[string]any
	client := orderTestClient(t, recordingHandler(t, "product.product", &writes))
	off := false
	require.NoError(t, client.UpdateProduct(context.Background(), 21, UpdateProductRequest{IsStorable: &off}))
	assertJSON(t, writes[0], `{"ids":[21],"vals":{"is_storable":false}}`)
}

func TestSetProductStockReportsInventoryConflict(t *testing.T) {
	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/json/2/product.product/search_read":
			fmt.Fprint(w, `[{"id":21,"is_storable":true}]`)
		case "/json/2/stock.quant/create":
			fmt.Fprint(w, `[9]`)
		case "/json/2/stock.quant/action_apply_inventory":
			fmt.Fprint(w, `{"type":"ir.actions.act_window","res_model":"stock.inventory.conflict"}`)
		default:
			http.NotFound(w, r)
		}
	})

	err := client.SetProductStock(context.Background(), 21, 5, 30)
	require.ErrorIs(t, err, ErrInventoryConflict)
	assert.Contains(t, err.Error(), "quant 9")
}
