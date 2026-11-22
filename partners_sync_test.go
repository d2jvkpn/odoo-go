package odoo

import (
	"context"
	jsonv2 "encoding/json/v2"
	"fmt"
	"net/http"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListSyncPartnersPagesThroughActiveAndArchived(t *testing.T) {
	old := syncPartnerPage
	syncPartnerPage = 2
	t.Cleanup(func() { syncPartnerPage = old })
	fields, err := jsonv2.Marshal(append(slices.Clone(partnerFields), "active"))
	require.NoError(t, err)
	calls := 0
	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/json/2/res.partner/search_read", r.URL.Path)
		want := `{"domain":[["active","in",[true,false]]],"fields":%s,"limit":2,"offset":%d,"order":"id"}`
		switch calls {
		case 0:
			assertJSON(t, requestParams(t, r), fmt.Sprintf(want, fields, 0))
			fmt.Fprint(w, `[{"id":1,"name":"A","ref":"r1","active":true,"category_id":[]},`+
				`{"id":2,"name":"B","ref":false,"active":false,"category_id":[5]}]`)
		case 1:
			assertJSON(t, requestParams(t, r), fmt.Sprintf(want, fields, 2))
			fmt.Fprint(w, `[{"id":3,"name":"C","parent_id":[1,"A"],"active":true,"category_id":[]}]`)
		default:
			t.Errorf("unexpected extra page request")
		}
		calls++
	})

	got, err := client.ListSyncPartners(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 3)
	assert.Equal(t, 2, calls)
	assert.True(t, got[0].Active)
	assert.Equal(t, "r1", string(got[0].Ref))
	assert.False(t, got[1].Active)
	assert.Equal(t, []int{5}, got[1].CategoryIDs)
	assert.Equal(t, 1, got[2].Parent.ID)
}

func TestListSyncPartnersRequestsAFinalEmptyPageOnAnExactMultiple(t *testing.T) {
	old := syncPartnerPage
	syncPartnerPage = 2
	t.Cleanup(func() { syncPartnerPage = old })
	calls := 0
	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if calls == 0 {
			fmt.Fprint(w, `[{"id":1,"name":"A","active":true,"category_id":[]},`+
				`{"id":2,"name":"B","active":true,"category_id":[]}]`)
		} else {
			fmt.Fprint(w, `[]`)
		}
		calls++
	})

	got, err := client.ListSyncPartners(context.Background())
	require.NoError(t, err)
	assert.Len(t, got, 2)
	assert.Equal(t, 2, calls)
}

func TestListSyncPartnersErrorIsWrapped(t *testing.T) {
	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"boom"}`, http.StatusInternalServerError)
	})
	_, err := client.ListSyncPartners(context.Background())
	assert.ErrorContains(t, err, "list partners")
}

func TestListStatesByIDs(t *testing.T) {
	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/json/2/res.country.state/search_read", r.URL.Path)
		assertJSON(t, requestParams(t, r),
			`{"domain":[["id","in",[13,14]]],"fields":["id","name","code","country_id"],"order":"id"}`)
		fmt.Fprint(w, `[{"id":13,"name":"California","code":"CA","country_id":[233,"United States"]}]`)
	})

	got, err := client.ListStatesByIDs(context.Background(), []int{13, 14})
	require.NoError(t, err)
	assert.Equal(t, []StateInfo{{ID: 13, Name: "California", Code: "CA", Country: Many2One{ID: 233, Name: "United States"}}}, got)
}

func TestListStatesByIDsEmptyAndInvalid(t *testing.T) {
	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("no request expected")
	})
	got, err := client.ListStatesByIDs(context.Background(), nil)
	require.NoError(t, err)
	assert.NotNil(t, got)
	assert.Empty(t, got)
	_, err = client.ListStatesByIDs(context.Background(), []int{0})
	assert.ErrorContains(t, err, "positive")
}

func TestListActiveLanguages(t *testing.T) {
	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/json/2/res.lang/search_read", r.URL.Path)
		assertJSON(t, requestParams(t, r), `{"domain":[["active","=",true]],"fields":["code"],"order":"code"}`)
		fmt.Fprint(w, `[{"id":1,"code":"en_US"},{"id":2,"code":"fr_FR"}]`)
	})

	got, err := client.ListActiveLanguages(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"en_US", "fr_FR"}, got)
}

func TestListActiveLanguagesEmptyIsNotNil(t *testing.T) {
	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `[]`) })
	got, err := client.ListActiveLanguages(context.Background())
	require.NoError(t, err)
	assert.NotNil(t, got)
	assert.Empty(t, got)
}

func TestListActiveLanguagesErrorIsWrapped(t *testing.T) {
	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"boom"}`, http.StatusInternalServerError)
	})
	_, err := client.ListActiveLanguages(context.Background())
	assert.ErrorContains(t, err, "list languages")
}
