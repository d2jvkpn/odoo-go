package odoo

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPartnerPricelistsAndPagination(t *testing.T) {
	var offsets []float64
	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/json/2/res.partner/search_read":
			assertJSON(t, requestParams(t, r),
				`{"domain":[["id","in",[2,5,8]]],"fields":["id","property_product_pricelist"],"limit":3}`)
			fmt.Fprint(w, `[{"id":2,"property_product_pricelist":[3,"Retail"]},{"id":5,"property_product_pricelist":false}]`)
		case "/json/2/product.pricelist.item/search_read":
			offset := requestParams(t, r)["offset"].(float64)
			offsets = append(offsets, offset)
			if offset == 0 {
				fmt.Fprint(w, `[{"id":1},{"id":2}]`)
			} else {
				fmt.Fprint(w, `[{"id":3}]`)
			}
		}
	})
	got, err := client.PartnerPricelists(context.Background(), []int{2, 5, 8})
	require.NoError(t, err)
	assert.Equal(t, map[int]Many2One{2: {ID: 3, Name: "Retail"}, 5: {}}, got)
	var ids []int
	for item, err := range client.AllPricelistItems(context.Background(), PricelistItemQuery{Limit: 2}) {
		require.NoError(t, err)
		ids = append(ids, item.ID)
	}
	assert.Equal(t, []int{1, 2, 3}, ids)
	assert.Equal(t, []float64{0, 2}, offsets)
}

func TestCreateFixedPricelistItemTemplateAndInvalidValues(t *testing.T) {
	var calls int
	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		assertJSON(t, requestParams(t, r), `{"vals_list":[{"pricelist_id":3,"product_tmpl_id":20,`+
			`"applied_on":"1_product","compute_price":"fixed","fixed_price":9.5,`+
			`"date_end":"2026-12-31 23:59:59"}]}`)
		fmt.Fprint(w, `[4]`)
	})
	id, err := client.CreateFixedPricelistItem(context.Background(), CreateFixedPricelistItemRequest{
		PricelistID: 3, ProductTemplateID: 20, FixedPrice: 9.5, DateEnd: "2026-12-31 23:59:59",
	})
	require.NoError(t, err)
	assert.Equal(t, 4, id)
	for _, input := range []CreateFixedPricelistItemRequest{
		{PricelistID: 3, ProductTemplateID: 20, FixedPrice: math.NaN()},
		{PricelistID: 3, ProductTemplateID: 20, FixedPrice: -1},
		{PricelistID: 3, ProductTemplateID: 20, MinQuantity: math.Inf(1)},
		{PricelistID: 3, ProductTemplateID: 20, DateStart: "2026-01-01"},
		{PricelistID: 3, ProductTemplateID: 20, DateStart: "2026-12-31 00:00:00", DateEnd: "2026-01-01 00:00:00"},
		{PricelistID: 0, ProductTemplateID: 20},
		{PricelistID: 3},
	} {
		_, err = client.CreateFixedPricelistItem(context.Background(), input)
		assert.ErrorIs(t, err, ErrInvalidArgument)
	}
	assert.Equal(t, 1, calls)
}

func TestCreatePricelistUnexpectedResponseAndRemoteError(t *testing.T) {
	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `[]`) })
	_, err := client.CreatePricelist(context.Background(), CreatePricelistRequest{Name: "Retail"})
	assert.ErrorIs(t, err, ErrUnexpectedResponse)
	client = orderTestClient(t, func(w http.ResponseWriter, r *http.Request) { http.Error(w, "denied", 403) })
	_, err = client.CreateFixedPricelistItem(context.Background(), CreateFixedPricelistItemRequest{
		PricelistID: 3, ProductID: 20,
	})
	var httpErr *HTTPError
	assert.ErrorAs(t, err, &httpErr)
}

func TestSearchPricelistsAndItems(t *testing.T) {
	var calls []string
	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.URL.Path)
		switch r.URL.Path {
		case "/json/2/product.pricelist/search_read":
			assertJSON(t, requestParams(t, r), `{"domain":[],"fields":["id","name","currency_id",`+
				`"active"],"limit":100,"offset":0,"order":"id"}`)
			fmt.Fprint(w, `[{"id":3,"name":"Retail","currency_id":[7,"CNY"],"active":true}]`)
		case "/json/2/product.pricelist.item/search_read":
			assertJSON(t, requestParams(t, r), `{"domain":[["pricelist_id","=",3]],"fields":["id",`+
				`"pricelist_id","applied_on","product_tmpl_id","product_id","compute_price",`+
				`"fixed_price","min_quantity","date_start","date_end"],"limit":2,"offset":4,`+
				`"order":"id desc"}`)
			fmt.Fprint(w, `[{"id":8,"pricelist_id":[3,"Retail"],"applied_on":"1_product",`+
				`"product_tmpl_id":[20,"Apple"],"product_id":false,"compute_price":"fixed",`+
				`"fixed_price":12.5,"min_quantity":0,"date_start":false,`+
				`"date_end":"2026-12-31 00:00:00"}]`)
		}
	})
	lists, err := client.SearchPricelists(context.Background(), PricelistQuery{})
	require.NoError(t, err)
	assert.Equal(t, []Pricelist{{ID: 3, Name: "Retail", Currency: Many2One{ID: 7, Name: "CNY"}, Active: true}}, lists)
	items, err := client.SearchPricelistItems(context.Background(), PricelistItemQuery{
		Domain: []any{[]any{"pricelist_id", "=", 3}}, Limit: 2, Offset: 4, Order: "id desc",
	})
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, 8, items[0].ID)
	assert.Equal(t, Many2One{}, items[0].Product)
	assert.Equal(t, OptionalString("2026-12-31 00:00:00"), items[0].DateEnd)
	assert.Len(t, calls, 2)
}

func TestCreatePricelistAndFixedPriceRule(t *testing.T) {
	var calls []string
	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.URL.Path)
		switch r.URL.Path {
		case "/json/2/product.pricelist/create":
			assertJSON(t, requestParams(t, r), `{"vals_list":[{"name":"Retail","currency_id":7}]}`)
			fmt.Fprint(w, `[3]`)
		case "/json/2/product.pricelist.item/create":
			assertJSON(t, requestParams(t, r), `{"vals_list":[{"pricelist_id":3,`+
				`"applied_on":"0_product_variant","product_id":21,"compute_price":"fixed",`+
				`"fixed_price":0,"min_quantity":1,"date_start":"2026-01-01 00:00:00"}]}`)
			fmt.Fprint(w, `[8]`)
		}
	})
	id, err := client.CreatePricelist(context.Background(), CreatePricelistRequest{Name: "Retail", CurrencyID: 7})
	require.NoError(t, err)
	assert.Equal(t, 3, id)
	id, err = client.CreateFixedPricelistItem(context.Background(), CreateFixedPricelistItemRequest{
		PricelistID: 3, ProductID: 21, FixedPrice: 0, MinQuantity: 1, DateStart: "2026-01-01 00:00:00",
	})
	require.NoError(t, err)
	assert.Equal(t, 8, id)
	assert.Len(t, calls, 2)
}

func TestPricelistValidationSendsNoRequest(t *testing.T) {
	var calls int
	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) { calls++ })
	_, err := client.CreatePricelist(context.Background(), CreatePricelistRequest{Name: " "})
	assert.ErrorIs(t, err, ErrInvalidArgument)
	_, err = client.CreateFixedPricelistItem(context.Background(), CreateFixedPricelistItemRequest{
		PricelistID: 3, ProductTemplateID: 20, ProductID: 21,
	})
	assert.ErrorIs(t, err, ErrInvalidArgument)
	_, err = client.SearchPricelists(context.Background(), PricelistQuery{Limit: -1})
	assert.ErrorIs(t, err, ErrInvalidArgument)
	assert.Zero(t, calls)
}

func TestGetAndCountPricelist(t *testing.T) {
	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/json/2/product.pricelist/search_read":
			fmt.Fprint(w, `[]`)
		case "/json/2/product.pricelist.item/search_count":
			assertJSON(t, requestParams(t, r), `{"domain":[["pricelist_id","=",3]]}`)
			fmt.Fprint(w, `4`)
		}
	})
	_, err := client.GetPricelist(context.Background(), 3)
	assert.True(t, errors.Is(err, ErrPricelistNotFound))
	n, err := client.CountPricelistItems(context.Background(), []any{[]any{"pricelist_id", "=", 3}})
	require.NoError(t, err)
	assert.Equal(t, 4, n)
}
