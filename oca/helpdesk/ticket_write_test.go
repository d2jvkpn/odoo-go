package helpdesk

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	odoo "github.com/d2jvkpn/odoo-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateTicket(t *testing.T) {
	client := ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/json/2/helpdesk.ticket/create", r.URL.Path)
		assertJSON(t, requestParams(t, r), `{"vals_list":[{"name":"Broken pump",`+
			`"description":"<p>leaks</p>","partner_id":15,"sale_order_ids":[[6,0,[42]]]}]}`)
		fmt.Fprint(w, `[101]`)
	})

	id, err := client.CreateTicket(context.Background(), CreateTicketRequest{
		Name:         "Broken pump",
		Description:  "<p>leaks</p>",
		PartnerID:    15,
		SaleOrderIDs: odoo.ReplaceIDs{42},
	})
	require.NoError(t, err)
	assert.Equal(t, 101, id)
}

func TestCreateTicketRejectsBadInput(t *testing.T) {
	var calls int

	client := ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) { calls++ })
	for name, in := range map[string]CreateTicketRequest{
		"blank name":        {Name: " ", Description: "d"},
		"blank description": {Name: "n", Description: " "},
		"neg partner":       {Name: "n", Description: "d", PartnerID: -1},
		"neg stage":         {Name: "n", Description: "d", StageID: -1},
		"bad priority":      {Name: "n", Description: "d", Priority: "9"},
	} {
		_, err := client.CreateTicket(context.Background(), in)
		assert.ErrorIs(t, err, odoo.ErrInvalidArgument, name)
	}
	assert.Zero(t, calls)
}

func TestUpdateTicket(t *testing.T) {
	var writes []map[string]any

	client := ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/json/2/helpdesk.ticket/search_read":
			assertJSON(t, requestParams(t, r)["domain"], `[["id","=",7]]`)
			fmt.Fprint(w, `[{"id":7}]`)
		case "/json/2/helpdesk.ticket/write":
			params := requestParams(t, r)
			writes = append(writes, params)
			fmt.Fprint(w, `true`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})

	stage := 4
	priority := "2"
	require.NoError(t, client.UpdateTicket(context.Background(), 7, UpdateTicketRequest{
		StageID:  &stage,
		Priority: &priority,
	}))
	require.Len(t, writes, 1)
	assertJSON(t, writes[0], `{"ids":[7],"vals":{"stage_id":4,"priority":"2"}}`)
}

func TestUpdateTicketClearsRelation(t *testing.T) {
	var writes []map[string]any

	client := ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/json/2/helpdesk.ticket/search_read":
			fmt.Fprint(w, `[{"id":7}]`)
		case "/json/2/helpdesk.ticket/write":
			writes = append(writes, requestParams(t, r))
			fmt.Fprint(w, `true`)
		}
	})

	partner := 0
	require.NoError(t, client.UpdateTicket(context.Background(), 7, UpdateTicketRequest{
		PartnerID: &partner,
	}))
	require.Len(t, writes, 1)
	assertJSON(t, writes[0], `{"ids":[7],"vals":{"partner_id":false}}`)
}

func TestUpdateTicketNotFound(t *testing.T) {
	client := ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[]`)
	})
	name := "x"
	err := client.UpdateTicket(context.Background(), 7, UpdateTicketRequest{Name: &name})
	assert.ErrorIs(t, err, ErrTicketNotFound)
}

func TestUpdateTicketRejectsEmptyPatch(t *testing.T) {
	var calls int

	client := ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) { calls++ })
	err := client.UpdateTicket(context.Background(), 7, UpdateTicketRequest{})
	assert.ErrorIs(t, err, odoo.ErrInvalidArgument)
	assert.Zero(t, calls)
}

func TestUpdateTicketRejectsBadInput(t *testing.T) {
	var calls int

	client := ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) { calls++ })
	blank := " "
	badPriority := "9"
	neg := -1
	for name, patch := range map[string]UpdateTicketRequest{
		"blank name":   {Name: &blank},
		"bad priority": {Priority: &badPriority},
		"neg user":     {UserID: &neg},
	} {
		err := client.UpdateTicket(context.Background(), 7, patch)
		assert.ErrorIs(t, err, odoo.ErrInvalidArgument, name)
	}
	assert.Zero(t, calls)
}
