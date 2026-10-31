package odoo

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListCountries(t *testing.T) {
	var (
		client    *Client
		countries []Country
		err       error
	)

	client = orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/json/2/res.country/search_read", r.URL.Path)
		assertJSON(t, requestParams(t, r), `{"domain":[],"fields":["id","name","code"],"order":"name"}`)
		fmt.Fprint(w, `[{"id":161,"name":"Norway","code":"NO"},{"id":49,"name":"China","code":"CN"}]`)
	})

	countries, err = client.ListCountries(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []Country{{ID: 161, Name: "Norway", Code: "NO"}, {ID: 49, Name: "China", Code: "CN"}}, countries)
}

func TestListCountriesEmptyAndError(t *testing.T) {
	var (
		client    *Client
		countries []Country
		err       error
	)

	client = orderTestClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `[]`) })
	countries, err = client.ListCountries(context.Background())
	require.NoError(t, err)
	assert.NotNil(t, countries)
	assert.Empty(t, countries)

	client = orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"boom"}`, http.StatusInternalServerError)
	})
	_, err = client.ListCountries(context.Background())
	assert.ErrorContains(t, err, "list countries")
}
