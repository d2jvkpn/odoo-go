package odoo

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateProductCategory(t *testing.T) {
	for _, tc := range []struct {
		name  string
		kind  CategoryType
		input CreateProductCategoryRequest
		model string
		body  string
	}{
		{"internal child", "", CreateProductCategoryRequest{Name: "Tea", ParentID: 1},
			"product.category", `{"vals_list":[{"name":"Tea","parent_id":1}]}`},
		{"website root", WebsiteCategory, CreateProductCategoryRequest{Name: "Shop"},
			"product.public.category", `{"vals_list":[{"name":"Shop"}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls int

			client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				assert.Equal(t, "/json/2/"+tc.model+"/create", r.URL.Path)
				assertJSON(t, requestParams(t, r), tc.body)
				fmt.Fprint(w, "[9]")
			})
			id, err := client.CreateProductCategory(context.Background(), tc.kind, tc.input)
			require.NoError(t, err)
			assert.Equal(t, 9, id)
			assert.Equal(t, 1, calls)
		})
	}
}

func TestCreateProductCategoryRejectsBadInputWithoutRequests(t *testing.T) {
	var calls int

	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) { calls++ })
	for name, tc := range map[string]struct {
		kind  CategoryType
		input CreateProductCategoryRequest
	}{
		"empty name":      {"", CreateProductCategoryRequest{Name: " "}},
		"negative parent": {"", CreateProductCategoryRequest{Name: "Tea", ParentID: -1}},
		"bad type":        {"x", CreateProductCategoryRequest{Name: "Tea"}},
	} {
		_, err := client.CreateProductCategory(context.Background(), tc.kind, tc.input)
		assert.Error(t, err, name)
	}
	assert.Zero(t, calls)
}

func TestCreateProductCategoryUnexpectedResponseAndRemoteError(t *testing.T) {
	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "[]") })
	_, err := client.CreateProductCategory(context.Background(), "", CreateProductCategoryRequest{Name: "Tea"})
	assert.ErrorIs(t, err, ErrUnexpectedResponse)

	client = orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"boom"}`, http.StatusInternalServerError)
	})
	_, err = client.CreateProductCategory(context.Background(), "", CreateProductCategoryRequest{Name: "Tea"})
	assert.Error(t, err)
}
