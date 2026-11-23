package odoo

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListSyncUoMsReadsConversionsAndArchivedUnits(t *testing.T) {
	var fields any
	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/json/2/uom.uom/search_read", r.URL.Path)
		params := requestParams(t, r)
		fields = params["fields"]
		assertJSON(t, params["context"], `{"active_test":false}`)
		fmt.Fprint(w, `[{"id":1,"name":"个","relative_uom_id":false,"relative_factor":1,"active":true},`+
			`{"id":2,"name":"包","relative_uom_id":[1,"个"],"relative_factor":12,"active":false}]`)
	})
	units, err := client.ListSyncUoMs(context.Background())
	require.NoError(t, err)
	assertJSON(t, fields, `["id","name","relative_uom_id","relative_factor","active"]`)
	assert.Equal(t, []SyncUoM{
		{ID: 1, Name: "个", RelativeFactor: 1, Active: true},
		{ID: 2, Name: "包", RelativeTo: Many2One{ID: 1, Name: "个"}, RelativeFactor: 12},
	}, units)
}

func TestCreateSyncUoMSendsReferenceAndFactor(t *testing.T) {
	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/json/2/uom.uom/create", r.URL.Path)
		assertJSON(t, requestParams(t, r), `{"vals_list":[{"name":"包","relative_uom_id":7,"relative_factor":12}]}`)
		fmt.Fprint(w, `[8]`)
	})
	id, err := client.CreateSyncUoM(context.Background(), "包", 7, 12)
	require.NoError(t, err)
	assert.Equal(t, 8, id)
}
