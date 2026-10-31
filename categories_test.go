package odoo

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSearchProductCategories(t *testing.T) {
	for _, tc := range []struct {
		kind  CategoryType
		model string
	}{
		{"", "product.category"},
		{InternalCategory, "product.category"},
		{WebsiteCategory, "product.public.category"},
	} {
		t.Run(string(tc.kind), func(t *testing.T) {
			var (
				client     *Client
				categories []ProductCategory
				err        error
			)

			client = orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/json/2/"+tc.model+"/search_read", r.URL.Path)
				if tc.kind == WebsiteCategory {
					assertJSON(t, requestParams(t, r), `{"domain":[["name","ilike","Desk"]],`+
						`"fields":["id","name","display_name","parent_id","child_id"],`+
						`"limit":5,"offset":10,"order":"id desc"}`)
					fmt.Fprint(w, `[{"id":2,"name":"Desks","display_name":"Furniture / Desks",`+
						`"parent_id":[1,"Furniture"],"child_id":[3,4]}]`)
				} else {
					assertJSON(t, requestParams(t, r), `{"domain":[["name","ilike","Desk"]],`+
						`"fields":["id","name","complete_name","parent_id","child_id","parent_path"],`+
						`"limit":5,"offset":10,"order":"id desc"}`)
					fmt.Fprint(w, `[{"id":2,"name":"Desks","complete_name":"Furniture / Desks",`+
						`"parent_id":[1,"Furniture"]}]`)
				}
			})

			categories, err = client.SearchProductCategories(context.Background(), ProductCategoryQuery{
				Type:   tc.kind,
				Domain: []any{[]any{"name", "ilike", "Desk"}},
				Limit:  5,
				Offset: 10,
				Order:  "id desc",
			})
			require.NoError(t, err)
			require.Len(t, categories, 1)
			assert.Equal(t, 2, categories[0].ID)
			assert.Equal(t, 1, categories[0].Parent.ID)

			if tc.kind == WebsiteCategory {
				assert.Len(t, categories[0].ChildIDs, 2)
				assert.Equal(t, "Furniture / Desks", categories[0].DisplayName)
			} else {
				assertJSON(t, categories[0], `{"id":2,"name":"Desks","complete_name":"Furniture / Desks",`+
					`"parent_id":{"id":1,"name":"Furniture"}}`)
			}
		})
	}
}

func TestCategoryParentsAndDefaults(t *testing.T) {
	for _, parent := range []int{0, 7} {
		t.Run(fmt.Sprint(parent), func(t *testing.T) {
			var (
				domain   []any
				sentinel []any
				full     []any
				client   *Client
				items    []ProductCategory
				err      error
			)

			domain = make([]any, 1, 3)
			domain[0] = []any{"name", "ilike", "Desk"}
			sentinel = []any{"untouched"}
			full = domain[:cap(domain)]
			full[1] = sentinel

			client = orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				var (
					params map[string]any
					want   string
				)

				params = requestParams(t, r)
				want = `[["name","ilike","Desk"],["parent_id","=",false]]`
				if parent != 0 {
					want = `[["name","ilike","Desk"],["parent_id","=",7]]`
				}
				assertJSON(t, params["domain"], want)
				assert.Equal(t, float64(1000), params["limit"])
				assert.Equal(t, float64(0), params["offset"])
				assert.Equal(t, "id", params["order"])
				fmt.Fprint(w, `[{"id":1,"name":"Root","complete_name":"Root","parent_id":false}]`)
			})

			items, err = client.SearchProductCategories(context.Background(), ProductCategoryQuery{
				ParentID: &parent,
				Domain:   domain,
			})
			require.NoError(t, err)
			require.Len(t, items, 1)
			assert.Equal(t, 0, items[0].Parent.ID)
			assertJSON(t, full[1], `["untouched"]`)
		})
	}
}

func TestCategoryEmptySearch(t *testing.T) {
	var (
		client *Client
		items  []ProductCategory
		err    error
	)

	client = orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		var params map[string]any

		params = requestParams(t, r)
		assertJSON(t, params["domain"], "[]")
		assertJSON(t, params["fields"], `["id","name","complete_name","parent_id","child_id","parent_path"]`)
		assert.Equal(t, float64(1000), params["limit"])
		fmt.Fprint(w, "[]")
	})

	items, err = client.SearchProductCategories(context.Background(), ProductCategoryQuery{})
	require.NoError(t, err)
	require.NotNil(t, items)
	assert.Empty(t, items)
}

func TestWebsiteCategoryDefaultLimit(t *testing.T) {
	var (
		client *Client
		err    error
	)

	client = orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, float64(100), requestParams(t, r)["limit"], "website category limit changed")
		fmt.Fprint(w, "[]")
	})

	_, err = client.SearchProductCategories(context.Background(), ProductCategoryQuery{Type: WebsiteCategory})
	require.NoError(t, err)
}

func TestGetInternalCategoryCompleteName(t *testing.T) {
	var (
		client   *Client
		category *ProductCategory
		err      error
	)

	client = orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		var params map[string]any

		params = requestParams(t, r)
		assertJSON(t, params["fields"], `["id","name","complete_name","parent_id","child_id","parent_path"]`)
		assertJSON(t, params["domain"], `[["id","=",2]]`)
		assert.Equal(t, float64(1), params["limit"])
		fmt.Fprint(w, `[{"id":2,"name":"Desks","complete_name":"Furniture / Desks","parent_id":[1,"Furniture"]}]`)
	})

	category, err = client.GetProductCategory(context.Background(), InternalCategory, 2)
	require.NoError(t, err)
	assertJSON(t, category, `{"id":2,"name":"Desks","complete_name":"Furniture / Desks",`+
		`"parent_id":{"id":1,"name":"Furniture"}}`)
}

func TestGetProductCategory(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(fmt.Sprint(missing), func(t *testing.T) {
			var (
				client   *Client
				category *ProductCategory
				err      error
			)

			client = orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				var params map[string]any

				assert.Equal(t, "/json/2/product.public.category/search_read", r.URL.Path)
				params = requestParams(t, r)
				assertJSON(t, params["domain"], `[["id","=",2]]`)
				assert.Equal(t, float64(1), params["limit"])
				if missing {
					fmt.Fprint(w, "[]")
				} else {
					fmt.Fprint(w, `[{"id":2,"name":"Desks","parent_id":null,"child_id":[]}]`)
				}
			})

			category, err = client.GetProductCategory(context.Background(), WebsiteCategory, 2)
			if missing {
				assert.ErrorIs(t, err, ErrProductCategoryNotFound)
				assert.Nil(t, category)
			} else {
				require.NoError(t, err)
				assert.Equal(t, 2, category.ID)
			}
		})
	}
}

func TestCategoryInvalidInput(t *testing.T) {
	var (
		client   *Client
		negative int
		err      error
	)

	client = orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Fail(t, "invalid category query reached server")
		fmt.Fprint(w, "[]")
	})

	negative = -1

	for _, query := range []ProductCategoryQuery{{Type: "unknown"}, {Limit: -1}, {Offset: -1}, {ParentID: &negative}} {
		_, err = client.SearchProductCategories(context.Background(), query)
		assert.Error(t, err, "accepted %+v", query)
	}
	for _, id := range []int{0, -1} {
		_, err = client.GetProductCategory(context.Background(), InternalCategory, id)
		assert.Error(t, err, "accepted id %d", id)
	}

	_, err = client.GetProductCategory(context.Background(), "unknown", 1)
	assert.Error(t, err, "accepted unknown category type")
}

func TestCategoryRemoteErrors(t *testing.T) {
	var (
		client *Client
		err    error
	)

	client = orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		fmt.Fprint(w, "Access denied")
	})

	_, err = client.SearchProductCategories(context.Background(), ProductCategoryQuery{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "403")

	_, err = client.GetProductCategory(context.Background(), InternalCategory, 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "403")
}
