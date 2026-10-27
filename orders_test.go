package odoo

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const orderFixture = `[{"id":42,"name":"S00042","partner_id":[7,"Customer"],"date_order":"2026-10-27 10:00:00",` +
	`"state":"draft","amount_untaxed":20,"amount_tax":2,"amount_total":22,"currency_id":[1,"USD"],` +
	`"client_order_ref":false,"origin":"web","note":"<p>Leave at door</p>","order_line":[11,12]}]`

func orderTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()

	var server *httptest.Server

	server = httptest.NewServer(handler)
	t.Cleanup(server.Close)

	return NewClient(server.URL, "test-key", "test-db")
}

// assertJSON compares the JSON encoding of got with want, ignoring formatting and key order.
func assertJSON(t *testing.T, got any, want string) {
	t.Helper()

	var (
		data []byte
		err  error
	)

	data, err = json.Marshal(got)
	if !assert.NoError(t, err) {
		return
	}
	assert.JSONEq(t, want, string(data))
}

// requestParams decodes the JSON request body. It is safe to call from handler goroutines.
func requestParams(t *testing.T, r *http.Request) map[string]any {
	t.Helper()

	var (
		params map[string]any
		err    error
	)

	err = json.NewDecoder(r.Body).Decode(&params)
	assert.NoError(t, err)

	return params
}

func TestSearchSaleOrders(t *testing.T) {
	var (
		client *Client
		orders []SaleOrder
		err    error
	)

	client = orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/json/2/sale.order/search_read", r.URL.Path)
		assertJSON(t, requestParams(t, r), `{"domain":[["partner_id","=",7],["state","=","sale"]],`+
			`"limit":5,"offset":10,"order":"id desc","fields":["id","name","partner_id","date_order","state",`+
			`"amount_untaxed","amount_tax","amount_total","currency_id","client_order_ref","invoice_status","delivery_status","origin","note","order_line"]}`)
		fmt.Fprint(w, orderFixture)
	})

	orders, err = client.SearchSaleOrders(context.Background(), SaleOrderQuery{
		Domain: []any{[]any{"partner_id", "=", 7}, []any{"state", "=", "sale"}},
		Limit:  5,
		Offset: 10,
		Order:  "id desc",
	})
	require.NoError(t, err)
	require.Len(t, orders, 1)
	assert.Equal(t, 42, orders[0].ID)
	assert.Equal(t, 7, orders[0].Partner.ID)
	assert.Equal(t, "Customer", orders[0].Partner.Name)
	assert.Equal(t, float64(22), orders[0].AmountTotal)
	assert.Empty(t, orders[0].ClientOrderRef)
	assert.Equal(t, "web", string(orders[0].Origin))
	assert.Equal(t, "<p>Leave at door</p>", string(orders[0].Note))
}

func TestSearchSaleOrdersDefaultsAndEmptyResult(t *testing.T) {
	var (
		client *Client
		orders []SaleOrder
		err    error
	)

	client = orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		var params map[string]any

		params = requestParams(t, r)
		assertJSON(t, params["domain"], "[]")
		assert.Equal(t, float64(100), params["limit"])
		assert.Equal(t, float64(0), params["offset"])
		assert.Equal(t, "id desc", params["order"])
		fmt.Fprint(w, "[]")
	})

	orders, err = client.SearchSaleOrders(context.Background(), SaleOrderQuery{})
	require.NoError(t, err)
	require.NotNil(t, orders)
	assert.Empty(t, orders)
}

func TestGetSaleOrderIncludesLines(t *testing.T) {
	var (
		calls  int
		client *Client
		order  *SaleOrder
		err    error
	)

	client = orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		var params map[string]any

		calls++
		params = requestParams(t, r)

		switch r.URL.Path {
		case "/json/2/sale.order/search_read":
			assertJSON(t, params["domain"], `[["id","=",42]]`)
			assert.Equal(t, float64(1), params["limit"])
			fmt.Fprint(w, orderFixture)
		case "/json/2/sale.order.line/read":
			assertJSON(t, params["ids"], "[11,12]")
			assertJSON(t, params["fields"],
				`["id","product_id","name","product_uom_qty","price_unit","price_subtotal","display_type"]`)
			fmt.Fprint(w, `[{"id":11,"product_id":[3,"Desk"],"name":"Desk","product_uom_qty":2,"price_unit":10,`+
				`"price_subtotal":20,"display_type":false},{"id":12,"product_id":false,"name":"Notes",`+
				`"product_uom_qty":0,"price_unit":0,"price_subtotal":0,"display_type":"line_note"}]`)
		default:
			assert.Failf(t, "unexpected path", "path = %s", r.URL.Path)
			w.WriteHeader(404)
		}
	})

	order, err = client.GetSaleOrder(context.Background(), 42)
	require.NoError(t, err)
	assert.Equal(t, 2, calls)
	require.Len(t, order.Lines, 2)
	assert.Equal(t, 3, order.Lines[0].Product.ID)
	assert.Equal(t, float64(2), order.Lines[0].Quantity)
	assert.Equal(t, 0, order.Lines[1].Product.ID)
	assert.Equal(t, "line_note", string(order.Lines[1].DisplayType))
}

func TestGetSaleOrderMissingOrWithoutLines(t *testing.T) {
	for _, missing := range []bool{true, false} {
		t.Run(fmt.Sprint(missing), func(t *testing.T) {
			var (
				calls  int
				client *Client
				order  *SaleOrder
				err    error
			)

			client = orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				if missing {
					fmt.Fprint(w, "[]")
				} else {
					fmt.Fprint(w, `[{"id":42,"order_line":[]}]`)
				}
			})

			order, err = client.GetSaleOrder(context.Background(), 42)
			if missing {
				assert.ErrorIs(t, err, ErrSaleOrderNotFound)
				assert.Nil(t, order)
			} else {
				require.NoError(t, err)
				assert.Equal(t, 42, order.ID)
				assert.Empty(t, order.Lines)
			}
			assert.Equal(t, 1, calls)
		})
	}
}

func TestCreateSaleOrder(t *testing.T) {
	var (
		calls       int
		client      *Client
		id          int
		err         error
		zero, price float64
	)

	client = orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		assert.Equal(t, "/json/2/sale.order/create", r.URL.Path)
		assertJSON(t, requestParams(t, r), `{"vals_list":[{"partner_id":7,"pricelist_id":2,`+
			`"client_order_ref":"web-123","origin":"web","note":"<p>Leave at door</p>","order_line":[[0,0,{"product_id":3,"product_uom_qty":2}],`+
			`[0,0,{"product_id":4,"product_uom_qty":1,"price_unit":0}],`+
			`[0,0,{"product_id":5,"product_uom_qty":1.5,"price_unit":12.5}]]}]}`)
		fmt.Fprint(w, "[42]")
	})

	zero, price = 0.0, 12.5
	id, err = client.CreateSaleOrder(context.Background(), CreateSaleOrderRequest{
		PartnerID:      7,
		PricelistID:    2,
		ClientOrderRef: "web-123",
		Origin:         "web",
		Note:           "<p>Leave at door</p>",
		Lines: []CreateSaleOrderLine{
			{ProductID: 3, Quantity: 2},
			{ProductID: 4, Quantity: 1, UnitPrice: &zero},
			{ProductID: 5, Quantity: 1.5, UnitPrice: &price},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, 42, id)
	assert.Equal(t, 1, calls)
}

func TestCreateSaleOrderDefaults(t *testing.T) {
	var (
		client *Client
		id     int
		err    error
	)

	client = orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/json/2/sale.order/create", r.URL.Path)
		assert.Equal(t, "bearer test-key", r.Header.Get("Authorization"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assertJSON(t, requestParams(t, r), `{"vals_list":[{"partner_id":15,"client_order_ref":"WX202610270001",`+
			`"order_line":[[0,0,{"product_id":101,"product_uom_qty":2}],`+
			`[0,0,{"product_id":102,"product_uom_qty":5}]]}]}`)
		fmt.Fprint(w, "[42]")
	})

	id, err = client.CreateSaleOrder(context.Background(), CreateSaleOrderRequest{
		PartnerID:      15,
		ClientOrderRef: "WX202610270001",
		Lines: []CreateSaleOrderLine{
			{ProductID: 101, Quantity: 2},
			{ProductID: 102, Quantity: 5},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, 42, id)
}

func TestOrderValidationDoesNotSendRequests(t *testing.T) {
	var (
		client                       *Client
		badPrice, nanPrice, infPrice float64
		validLine                    CreateSaleOrderLine
		id                           int
		err                          error
	)

	client = orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Fail(t, "invalid input reached server")
		fmt.Fprint(w, "[]")
	})

	for _, query := range []SaleOrderQuery{{Limit: -1}, {Offset: -1}} {
		_, err = client.SearchSaleOrders(context.Background(), query)
		assert.Error(t, err, "accepted invalid query %+v", query)
	}
	for _, orderID := range []int{0, -1} {
		_, err = client.GetSaleOrder(context.Background(), orderID)
		assert.Error(t, err, "accepted invalid ID %d", orderID)
	}

	badPrice, nanPrice, infPrice = -1.0, math.NaN(), math.Inf(1)
	validLine = CreateSaleOrderLine{ProductID: 3, Quantity: 1}

	for _, input := range []CreateSaleOrderRequest{
		{PartnerID: 0, Lines: []CreateSaleOrderLine{validLine}},
		{PartnerID: -1, Lines: []CreateSaleOrderLine{validLine}},
		{PartnerID: 7},
		{PartnerID: 7, PricelistID: -1, Lines: []CreateSaleOrderLine{validLine}},
		{PartnerID: 7, Lines: []CreateSaleOrderLine{{ProductID: 0, Quantity: 1}}},
		{PartnerID: 7, Lines: []CreateSaleOrderLine{{ProductID: 3, Quantity: 0}}},
		{PartnerID: 7, Lines: []CreateSaleOrderLine{{ProductID: 3, Quantity: -1}}},
		{PartnerID: 7, Lines: []CreateSaleOrderLine{{ProductID: 3, Quantity: math.NaN()}}},
		{PartnerID: 7, Lines: []CreateSaleOrderLine{{ProductID: 3, Quantity: math.Inf(1)}}},
		{PartnerID: 7, Lines: []CreateSaleOrderLine{{ProductID: 3, Quantity: 1, UnitPrice: &badPrice}}},
		{PartnerID: 7, Lines: []CreateSaleOrderLine{{ProductID: 3, Quantity: 1, UnitPrice: &nanPrice}}},
		{PartnerID: 7, Lines: []CreateSaleOrderLine{{ProductID: 3, Quantity: 1, UnitPrice: &infPrice}}},
	} {
		id, err = client.CreateSaleOrder(context.Background(), input)
		assert.Error(t, err, "accepted invalid order: %+v", input)
		assert.Equal(t, 0, id)
	}
}

func TestOrderRemoteErrors(t *testing.T) {
	for _, operation := range []string{"search", "get", "lines", "create"} {
		t.Run(operation, func(t *testing.T) {
			var (
				calls     int
				wantCalls int
				client    *Client
				err       error
			)

			client = orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				if operation == "lines" && calls == 1 {
					fmt.Fprint(w, orderFixture)
					return
				}
				w.WriteHeader(403)
				fmt.Fprint(w, "Access denied")
			})

			switch operation {
			case "search":
				_, err = client.SearchSaleOrders(context.Background(), SaleOrderQuery{})
			case "get", "lines":
				_, err = client.GetSaleOrder(context.Background(), 42)
			case "create":
				_, err = client.CreateSaleOrder(context.Background(), CreateSaleOrderRequest{
					PartnerID: 7,
					Lines:     []CreateSaleOrderLine{{ProductID: 3, Quantity: 1}},
				})
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), "403")

			wantCalls = 1
			if operation == "lines" {
				wantCalls = 2
			}
			assert.Equal(t, wantCalls, calls)
		})
	}
}

func TestCreateSaleOrderRejectsUnexpectedResponse(t *testing.T) {
	for _, body := range []string{"[]", "[0]", "[-1]", "[1,2]", "42", "null", "invalid"} {
		t.Run(body, func(t *testing.T) {
			var (
				calls  int
				client *Client
				id     int
				err    error
			)

			client = orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				fmt.Fprint(w, body)
			})

			id, err = client.CreateSaleOrder(context.Background(), CreateSaleOrderRequest{
				PartnerID: 7,
				Lines:     []CreateSaleOrderLine{{ProductID: 3, Quantity: 1}},
			})
			assert.Error(t, err)
			assert.Equal(t, 0, id)
			assert.Equal(t, 1, calls)
		})
	}
}

func TestMany2OneDecoding(t *testing.T) {
	var (
		value Many2One
		err   error
	)

	for _, tc := range []struct {
		raw  string
		want Many2One
	}{
		{`[7,"Customer"]`, Many2One{ID: 7, Name: "Customer"}},
		{"false", Many2One{}},
		{"null", Many2One{}},
	} {
		value = Many2One{ID: 99, Name: "old"}
		err = json.Unmarshal([]byte(tc.raw), &value)
		require.NoError(t, err, tc.raw)
		assert.Equal(t, tc.want, value, tc.raw)
	}

	for _, raw := range []string{"true", "7", "[]", "[7]", "[0,\"name\"]", "[7,false]", "[7,\"name\",8]"} {
		value = Many2One{}
		err = json.Unmarshal([]byte(raw), &value)
		assert.Error(t, err, "accepted %s", raw)
	}
}

func TestOptionalStringDecoding(t *testing.T) {
	var (
		value OptionalString
		err   error
	)

	for _, tc := range []struct{ raw, want string }{{`"web-123"`, "web-123"}, {"false", ""}, {"null", ""}} {
		value = OptionalString("old")
		err = json.Unmarshal([]byte(tc.raw), &value)
		require.NoError(t, err, tc.raw)
		assert.Equal(t, tc.want, string(value), tc.raw)
	}

	for _, raw := range []string{"true", "123", "[]"} {
		value = ""
		err = json.Unmarshal([]byte(raw), &value)
		assert.Error(t, err, "accepted %s", raw)
	}
}
