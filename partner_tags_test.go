package odoo

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListPartnerTags(t *testing.T) {
	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/json/2/res.partner.category/search_read", r.URL.Path)
		assertJSON(t, requestParams(t, r), `{"domain":[],"fields":["id","name","display_name"],"order":"name"}`)
		fmt.Fprint(w, `[{"id":2,"name":"VIP","display_name":"Retail / VIP"}]`)
	})

	tags, err := client.ListPartnerTags(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []PartnerTag{{ID: 2, Name: "VIP", DisplayName: "Retail / VIP"}}, tags)
}

func TestListPartnerTagsEmptyAndError(t *testing.T) {
	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `[]`) })
	tags, err := client.ListPartnerTags(context.Background())
	require.NoError(t, err)
	assert.NotNil(t, tags)
	assert.Empty(t, tags)

	client = orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"boom"}`, http.StatusInternalServerError)
	})
	_, err = client.ListPartnerTags(context.Background())
	assert.ErrorContains(t, err, "list partner tags")
}

func TestCreatePartnerTag(t *testing.T) {
	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/json/2/res.partner.category/create", r.URL.Path)
		assertJSON(t, requestParams(t, r), `{"vals_list":[{"name":"VIP"}]}`)
		fmt.Fprint(w, `[5]`)
	})
	id, err := client.CreatePartnerTag(context.Background(), "VIP")
	require.NoError(t, err)
	assert.Equal(t, 5, id)

	_, err = client.CreatePartnerTag(context.Background(), "  ")
	assert.ErrorContains(t, err, "name must not be empty")
}

func TestCreatePartnerTagRejectsUnexpectedResponse(t *testing.T) {
	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `[]`) })
	_, err := client.CreatePartnerTag(context.Background(), "VIP")
	assert.ErrorIs(t, err, ErrUnexpectedResponse)
}
