package odoo

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSearchAndGetPartner(t *testing.T) {
	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/json/2/res.partner/search_read", r.URL.Path)
		params := requestParams(t, r)
		assert.EqualValues(t, 1, params["limit"])
		fmt.Fprint(w, `[{"id":7,"name":"Ann","email":"a@x.io","phone":false,"ref":"u-1",`+
			`"street":false,"city":false,"zip":false,"country_id":false}]`)
	})
	got, err := client.GetPartner(context.Background(), 7)
	require.NoError(t, err)
	assert.Equal(t, "u-1", string(got.Ref))
	assert.Empty(t, got.Phone)
	assert.Zero(t, got.Country.ID)

	_, err = client.GetPartner(context.Background(), 0)
	assert.ErrorIs(t, err, ErrInvalidArgument)
}

func TestGetPartnerNotFound(t *testing.T) {
	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `[]`) })
	_, err := client.GetPartner(context.Background(), 7)
	assert.ErrorIs(t, err, ErrPartnerNotFound)
}

func TestCreatePartner(t *testing.T) {
	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/json/2/res.partner/create", r.URL.Path)
		assertJSON(t, requestParams(t, r),
			`{"vals_list":[{"name":"Ann","email":"a@x.io","ref":"u-1","country_id":233}]}`)
		fmt.Fprint(w, "[7]")
	})
	id, err := client.CreatePartner(context.Background(),
		CreatePartnerRequest{Name: "Ann", Email: "a@x.io", Ref: "u-1", CountryID: 233})
	require.NoError(t, err)
	assert.Equal(t, 7, id)
}

func TestCreatePartnerRejectsBadInput(t *testing.T) {
	var calls int

	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) { calls++ })
	for name, in := range map[string]CreatePartnerRequest{
		"blank name":  {Name: " "},
		"neg country": {Name: "A", CountryID: -1},
	} {
		_, err := client.CreatePartner(context.Background(), in)
		assert.ErrorIs(t, err, ErrInvalidArgument, name)
	}
	assert.Zero(t, calls)
}

func TestUpdatePartner(t *testing.T) {
	var writes []map[string]any

	client := orderTestClient(t, recordingHandler(t, "res.partner", &writes))
	phone := "123"
	require.NoError(t, client.UpdatePartner(context.Background(), 7, UpdatePartnerRequest{Phone: &phone}))
	assertJSON(t, writes[0], `{"ids":[7],"vals":{"phone":"123"}}`)
}

func upsertHandler(t *testing.T, found string, paths *[]string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		*paths = append(*paths, r.URL.Path)
		switch r.URL.Path {
		case "/json/2/res.partner/search_read":
			assertJSON(t, requestParams(t, r)["domain"], `[["ref","=","u-1"]]`)
			fmt.Fprint(w, found)
		case "/json/2/res.partner/create":
			// the ref passed to UpsertPartnerByRef must reach Odoo, or the next upsert misses it
			assertJSON(t, requestParams(t, r), `{"vals_list":[{"name":"Ann","ref":"u-1","city":"Oslo"}]}`)
			fmt.Fprint(w, "[99]")
		case "/json/2/res.partner/write":
			assertJSON(t, requestParams(t, r), `{"ids":[7],"vals":{"name":"Ann","ref":"u-1","city":"Oslo"}}`)
			fmt.Fprint(w, "true")
		}
	}
}

const partnerRow = `{"id":%d,"name":"Ann","email":false,"phone":false,"ref":"u-1",` +
	`"street":false,"city":false,"zip":false,"country_id":false}`

func TestUpsertPartnerByRef(t *testing.T) {
	input := CreatePartnerRequest{Name: "Ann", City: "Oslo"}

	t.Run("miss creates", func(t *testing.T) {
		var paths []string

		client := orderTestClient(t, upsertHandler(t, `[]`, &paths))
		id, created, err := client.UpsertPartnerByRef(context.Background(), "u-1", input)
		require.NoError(t, err)
		assert.Equal(t, 99, id)
		assert.True(t, created)
		assert.Equal(t, []string{"/json/2/res.partner/search_read", "/json/2/res.partner/create"}, paths)
	})
	t.Run("hit updates", func(t *testing.T) {
		var paths []string

		client := orderTestClient(t, upsertHandler(t, "["+fmt.Sprintf(partnerRow, 7)+"]", &paths))
		id, created, err := client.UpsertPartnerByRef(context.Background(), "u-1", input)
		require.NoError(t, err)
		assert.Equal(t, 7, id)
		assert.False(t, created)
		assert.Equal(t, []string{"/json/2/res.partner/search_read", "/json/2/res.partner/write"}, paths)
	})
	t.Run("two matches", func(t *testing.T) {
		var paths []string

		rows := "[" + fmt.Sprintf(partnerRow, 7) + "," + fmt.Sprintf(partnerRow, 8) + "]"
		client := orderTestClient(t, upsertHandler(t, rows, &paths))
		_, _, err := client.UpsertPartnerByRef(context.Background(), "u-1", input)
		assert.ErrorIs(t, err, ErrAmbiguousPartner)
		assert.Equal(t, []string{"/json/2/res.partner/search_read"}, paths)
	})
	t.Run("blank ref", func(t *testing.T) {
		client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) { t.Error("request sent") })
		_, _, err := client.UpsertPartnerByRef(context.Background(), " ", input)
		assert.ErrorIs(t, err, ErrInvalidArgument)
	})
}
