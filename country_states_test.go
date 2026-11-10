package odoo

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListCountryStates(t *testing.T) {
	var client = orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/json/2/res.country.state/search_read", r.URL.Path)
		assertJSON(t, requestParams(t, r),
			`{"domain":[["country_id","=",49]],"fields":["id","name","code"],"order":"code"}`)
		fmt.Fprint(w, `[{"id":7,"name":"Guangdong","code":"GD"}]`)
	})
	var states, err = client.ListCountryStates(context.Background(), 49)
	require.NoError(t, err)
	assert.Equal(t, []CountryState{{ID: 7, Name: "Guangdong", Code: "GD"}}, states)
}

func TestListCountryStatesRejectsBadCountry(t *testing.T) {
	var client = orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("no request expected")
	})
	var _, err = client.ListCountryStates(context.Background(), 0)
	assert.ErrorIs(t, err, ErrInvalidArgument)
}

func TestCreateCountryState(t *testing.T) {
	var client = orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/json/2/res.country.state/create", r.URL.Path)
		assertJSON(t, requestParams(t, r),
			`{"vals_list":[{"name":"广东省","code":"44","country_id":49}]}`)
		fmt.Fprint(w, `[12]`)
	})
	var id, err = client.CreateCountryState(context.Background(), 49, "广东省", "44")
	require.NoError(t, err)
	assert.Equal(t, 12, id)
}

func TestCreateCountryStateValidates(t *testing.T) {
	var client = orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("no request expected")
	})
	for _, c := range []struct {
		country    int
		name, code string
	}{{0, "a", "b"}, {1, " ", "b"}, {1, "a", ""}} {
		var _, err = client.CreateCountryState(context.Background(), c.country, c.name, c.code)
		assert.ErrorIs(t, err, ErrInvalidArgument, "%+v", c)
	}
}
