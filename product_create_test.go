package odoo

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateProduct(t *testing.T) {
	var (
		calls  int
		client *Client
		id     int
		err    error
		zero   float64
		saleOK bool
	)

	client = orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch calls {
		case 1:
			assert.Equal(t, "/json/2/product.template/create", r.URL.Path)
			assert.Equal(t, "bearer test-key", r.Header.Get("Authorization"))
			assertJSON(t, requestParams(t, r), `{"vals_list":[{"name":"Tea","list_price":0,`+
				`"default_code":"TEA-1","categ_id":4,"sale_ok":false}]}`)
			fmt.Fprint(w, "[9]")
		case 2:
			assert.Equal(t, "/json/2/product.template/read", r.URL.Path)
			assertJSON(t, requestParams(t, r), `{"ids":[9],"fields":["product_variant_id"]}`)
			fmt.Fprint(w, `[{"id":9,"product_variant_id":[21,"Tea"]}]`)
		default:
			t.Errorf("unexpected call %d to %s", calls, r.URL.Path)
		}
	})

	zero, saleOK = 0, false
	id, err = client.CreateProduct(context.Background(), CreateProductRequest{
		Name: "Tea", ListPrice: &zero, DefaultCode: "TEA-1", CategoryID: 4, SaleOK: &saleOK,
	})
	require.NoError(t, err)
	assert.Equal(t, 21, id)
	assert.Equal(t, 2, calls)
}

func TestCreateProductOmitsUnsetFields(t *testing.T) {
	var (
		client *Client
		id     int
		err    error
	)

	client = orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/create") {
			assertJSON(t, requestParams(t, r), `{"vals_list":[{"name":"Tea"}]}`)
			fmt.Fprint(w, "[9]")
			return
		}
		fmt.Fprint(w, `[{"id":9,"product_variant_id":[21,"Tea"]}]`)
	})

	id, err = client.CreateProduct(context.Background(), CreateProductRequest{Name: "Tea"})
	require.NoError(t, err)
	assert.Equal(t, 21, id)
}

func TestCreateProductValidationDoesNotSendRequests(t *testing.T) {
	var (
		client        *Client
		neg, nan, inf float64
		cases         map[string]CreateProductRequest
		name          string
		input         CreateProductRequest
		id            int
		err           error
		calls         int
	)

	client = orderTestClient(t, func(w http.ResponseWriter, r *http.Request) { calls++ })
	neg, nan, inf = -1, math.NaN(), math.Inf(1)
	cases = map[string]CreateProductRequest{
		"empty name":     {},
		"blank name":     {Name: "  "},
		"negative price": {Name: "A", ListPrice: &neg},
		"nan price":      {Name: "A", ListPrice: &nan},
		"inf price":      {Name: "A", ListPrice: &inf},
		"negative categ": {Name: "A", CategoryID: -1},
	}
	for name, input = range cases {
		id, err = client.CreateProduct(context.Background(), input)
		assert.ErrorIs(t, err, ErrInvalidArgument, name)
		assert.Zero(t, id, name)
	}
	assert.Zero(t, calls)
}

func TestCreateProductRemoteErrorsAreNotRetried(t *testing.T) {
	var (
		calls  int
		client *Client
		id     int
		err    error
		he     *HTTPError
	)

	client = orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	client.Retry = RetryPolicy{MaxRetries: 3, NoJitter: true}

	id, err = client.CreateProduct(context.Background(), CreateProductRequest{Name: "Tea"})
	require.Error(t, err)
	assert.ErrorAs(t, err, &he)
	assert.Zero(t, id)
	assert.Equal(t, 1, calls)
}

func TestCreateProductUnexpectedCreateResponse(t *testing.T) {
	for _, body := range []string{"[]", "[1,2]", "[0]", `{"id":1}`} {
		client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) })
		id, err := client.CreateProduct(context.Background(), CreateProductRequest{Name: "Tea"})
		require.Error(t, err, body)
		assert.Zero(t, id, body)
	}
}

func TestCreateProductVariantLookupFailureReportsTemplateID(t *testing.T) {
	for name, read := range map[string]string{
		"empty":   `[]`,
		"no link": `[{"id":9,"product_variant_id":false}]`,
	} {
		t.Run(name, func(t *testing.T) {
			client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/create") {
					fmt.Fprint(w, "[9]")
					return
				}
				fmt.Fprint(w, read)
			})
			id, err := client.CreateProduct(context.Background(), CreateProductRequest{Name: "Tea"})
			require.Error(t, err)
			assert.Zero(t, id)
			assert.ErrorIs(t, err, ErrUnexpectedResponse)
			assert.Contains(t, err.Error(), "product.template 9")
		})
	}

	t.Run("http error", func(t *testing.T) {
		client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/create") {
				fmt.Fprint(w, "[9]")
				return
			}
			http.Error(w, "boom", http.StatusForbidden)
		})
		_, err := client.CreateProduct(context.Background(), CreateProductRequest{Name: "Tea"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "product.template 9")
	})
}
