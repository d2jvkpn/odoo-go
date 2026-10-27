package odoo

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCountEndpoints(t *testing.T) {
	var (
		client *Client
		paths  []string
		zero   int
		n      int
		err    error
	)

	client = orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if strings.Contains(r.URL.Path, "product.category") {
			assertJSON(t, requestParams(t, r), `{"domain":[["name","ilike","x"],["parent_id","=",false]]}`)
		}
		fmt.Fprint(w, "42")
	})

	n, err = client.CountProducts(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, 42, n)
	n, err = client.CountSaleOrders(context.Background(), []any{[]any{"state", "=", "sale"}})
	require.NoError(t, err)
	assert.Equal(t, 42, n)
	n, err = client.CountProductCategories(context.Background(), ProductCategoryQuery{
		Domain: []any{[]any{"name", "ilike", "x"}}, ParentID: &zero, Limit: 5,
	})
	require.NoError(t, err)
	assert.Equal(t, 42, n)
	assert.Equal(t, []string{
		"/json/2/product.product/search_count",
		"/json/2/sale.order/search_count",
		"/json/2/product.category/search_count",
	}, paths)

	_, err = client.CountProductCategories(context.Background(), ProductCategoryQuery{Type: "bad"})
	assert.ErrorIs(t, err, ErrInvalidArgument)
}

func TestAllProductsPaginates(t *testing.T) {
	var (
		client  *Client
		offsets []float64
		ids     []int
		p       Product
		err     error
	)

	client = orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		var params map[string]any

		params = requestParams(t, r)

		offsets = append(offsets, params["offset"].(float64))
		assert.Equal(t, float64(2), params["limit"])
		switch params["offset"].(float64) {
		case 0:
			fmt.Fprint(w, `[{"id":1,"name":"a"},{"id":2,"name":"b"}]`)
		case 2:
			fmt.Fprint(w, `[{"id":3,"name":"c"},{"id":4,"name":"d"}]`)
		default:
			fmt.Fprint(w, `[{"id":5,"name":"e"}]`)
		}
	})
	for p, err = range client.AllProducts(context.Background(), ProductQuery{Limit: 2}) {
		require.NoError(t, err)
		ids = append(ids, p.ID)
	}
	assert.Equal(t, []int{1, 2, 3, 4, 5}, ids)
	assert.Equal(t, []float64{0, 2, 4}, offsets)
}

func TestAllSaleOrdersStopsEarlyAndEmptyLastPage(t *testing.T) {
	var (
		calls  int
		client *Client
		count  int
		err    error
	)

	client = orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			fmt.Fprint(w, `[{"id":1},{"id":2}]`)
			return
		}
		fmt.Fprint(w, `[]`)
	})
	for _, err = range client.AllSaleOrders(context.Background(), SaleOrderQuery{Limit: 2}) {
		require.NoError(t, err)
		count++
	}
	assert.Equal(t, 2, count)
	assert.Equal(t, 2, calls)

	calls = 0
	for range client.AllSaleOrders(context.Background(), SaleOrderQuery{Limit: 2}) {
		break
	}
	assert.Equal(t, 1, calls)
}

func TestAllProductCategoriesAndErrors(t *testing.T) {
	var (
		client  *Client
		errs    []error
		c       ProductCategory
		httpErr *HTTPError
		err     error
		n       int
	)

	client = orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, float64(100), requestParams(t, r)["limit"]) // website default page size
		fmt.Fprint(w, `[{"id":1,"name":"a","display_name":"a","parent_id":false}]`)
	})
	for c, err = range client.AllProductCategories(
		context.Background(), ProductCategoryQuery{Type: WebsiteCategory},
	) {
		require.NoError(t, err)
		assert.Equal(t, 1, c.ID)
		n++
	}
	assert.Equal(t, 1, n)

	for _, err = range client.AllProducts(context.Background(), ProductQuery{Offset: -1}) {
		errs = append(errs, err)
	}
	require.Len(t, errs, 1)
	assert.ErrorIs(t, errs[0], ErrInvalidArgument)

	client = orderTestClient(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(403) })
	errs = nil
	for _, err = range client.AllProducts(context.Background(), ProductQuery{}) {
		errs = append(errs, err)
	}
	require.Len(t, errs, 1)
	assert.ErrorAs(t, errs[0], &httpErr)
}

func TestPaginateIteratorCanBeReused(t *testing.T) {
	fetch := func(limit, offset int) ([]int, error) {
		var page []int
		for id := offset + 1; id <= 5 && id <= offset+limit; id++ {
			page = append(page, id)
		}
		return page, nil
	}
	seq := paginate(2, 0, fetch)
	collect := func(stopAfter int) []int {
		var got []int
		for v, err := range seq {
			require.NoError(t, err)
			got = append(got, v)
			if len(got) == stopAfter {
				break
			}
		}
		return got
	}

	assert.Equal(t, []int{1, 2, 3, 4, 5}, collect(0))
	assert.Equal(t, []int{1, 2, 3, 4, 5}, collect(0))
	assert.Equal(t, []int{1, 2, 3}, collect(3))
	assert.Equal(t, []int{1, 2, 3, 4, 5}, collect(0))
}
