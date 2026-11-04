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

func TestListTicketCategories(t *testing.T) {
	var client *Client

	client = ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/json/2/helpdesk.ticket.category/search_read", r.URL.Path)
		assertJSON(t, requestParams(t, r), `{"domain":[],"fields":["id","name","complete_name",`+
			`"parent_id","company_id","sequence","active","show_in_portal"],"order":"sequence, id"}`)
		fmt.Fprint(w, `[{"id":8,"name":"Pumps","complete_name":"Equipment / Pumps",`+
			`"parent_id":[2,"Equipment"],"company_id":[1,"Main Company"],"sequence":3,`+
			`"active":true,"show_in_portal":false}]`)
	})

	got, err := client.ListTicketCategories(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "Equipment / Pumps", got[0].CompleteName)
	assert.Equal(t, 2, got[0].Parent.ID)
	assert.False(t, got[0].ShowInPortal)
}

func TestListTicketTags(t *testing.T) {
	var client *Client

	client = ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/json/2/helpdesk.ticket.tag/search_read", r.URL.Path)
		assertJSON(t, requestParams(t, r), `{"domain":[],"fields":["id","name","color","company_id",`+
			`"sequence","active"],"order":"sequence, id"}`)
		fmt.Fprint(w, `[{"id":4,"name":"Urgent","color":2,"company_id":false,"sequence":1,"active":true}]`)
	})

	got, err := client.ListTicketTags(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "Urgent", got[0].Name)
	assert.Equal(t, 2, got[0].Color)
	assert.Zero(t, got[0].Company.ID)
}

func TestTicketCategoryCRUD(t *testing.T) {
	var (
		active       = false
		client       *Client
		parent       = 0
		showInPortal = true
	)

	client = ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/json/2/helpdesk.ticket.category/create":
			assertJSON(t, requestParams(t, r), `{"vals_list":[{"name":"Pumps","parent_id":2,`+
				`"company_id":1,"sequence":3,"active":false,"show_in_portal":true}]}`)
			fmt.Fprint(w, `[8]`)
		case "/json/2/helpdesk.ticket.category/write":
			assertJSON(t, requestParams(t, r), `{"ids":[8],"vals":{"parent_id":false}}`)
			fmt.Fprint(w, `true`)
		case "/json/2/helpdesk.ticket.category/unlink":
			assertJSON(t, requestParams(t, r), `{"ids":[8]}`)
			fmt.Fprint(w, `true`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})

	id, err := client.CreateTicketCategory(context.Background(), CreateTicketCategoryRequest{
		Name: "Pumps", ParentID: 2, CompanyID: 1, Sequence: 3,
		Active: &active, ShowInPortal: &showInPortal,
	})
	require.NoError(t, err)
	assert.Equal(t, 8, id)
	require.NoError(t, client.UpdateTicketCategory(
		context.Background(), 8, UpdateTicketCategoryRequest{ParentID: &parent},
	))
	require.NoError(t, client.DeleteTicketCategory(context.Background(), 8))
}

func TestTicketTagCRUD(t *testing.T) {
	var (
		active = false
		client *Client
		color  = 5
	)

	client = ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/json/2/helpdesk.ticket.tag/create":
			assertJSON(t, requestParams(t, r), `{"vals_list":[{"name":"Urgent","color":2,"active":false}]}`)
			fmt.Fprint(w, `[4]`)
		case "/json/2/helpdesk.ticket.tag/write":
			assertJSON(t, requestParams(t, r), `{"ids":[4],"vals":{"color":5}}`)
			fmt.Fprint(w, `true`)
		case "/json/2/helpdesk.ticket.tag/unlink":
			assertJSON(t, requestParams(t, r), `{"ids":[4]}`)
			fmt.Fprint(w, `true`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})

	id, err := client.CreateTicketTag(context.Background(), CreateTicketTagRequest{
		Name: "Urgent", Color: 2, Active: &active,
	})
	require.NoError(t, err)
	assert.Equal(t, 4, id)
	require.NoError(t, client.UpdateTicketTag(context.Background(), 4, UpdateTicketTagRequest{Color: &color}))
	require.NoError(t, client.DeleteTicketTag(context.Background(), 4))
}

func TestTicketMetadataRejectsBadInputWithoutCallingServer(t *testing.T) {
	var (
		calls  int
		client *Client
		err    error
		neg    = -1
	)

	client = ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) { calls++ })
	_, err = client.CreateTicketCategory(context.Background(), CreateTicketCategoryRequest{Name: " "})
	assert.ErrorIs(t, err, odoo.ErrInvalidArgument)
	_, err = client.CreateTicketCategory(context.Background(), CreateTicketCategoryRequest{
		Name: "x", ParentID: -1,
	})
	assert.ErrorIs(t, err, odoo.ErrInvalidArgument)
	err = client.UpdateTicketCategory(context.Background(), 1, UpdateTicketCategoryRequest{})
	assert.ErrorIs(t, err, odoo.ErrInvalidArgument)
	err = client.UpdateTicketCategory(context.Background(), 1, UpdateTicketCategoryRequest{Sequence: &neg})
	assert.ErrorIs(t, err, odoo.ErrInvalidArgument)
	_, err = client.CreateTicketTag(context.Background(), CreateTicketTagRequest{Name: "x", Color: -1})
	assert.ErrorIs(t, err, odoo.ErrInvalidArgument)
	err = client.UpdateTicketTag(context.Background(), 1, UpdateTicketTagRequest{})
	assert.ErrorIs(t, err, odoo.ErrInvalidArgument)
	err = client.DeleteTicketTag(context.Background(), 0)
	assert.ErrorIs(t, err, odoo.ErrInvalidArgument)
	assert.Zero(t, calls)
}

func TestListTicketTeamMembers(t *testing.T) {
	var client *Client

	client = ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/json/2/res.users/search_read", r.URL.Path)
		assertJSON(t, requestParams(t, r), `{"domain":[["helpdesk_team_ids","=",3]],`+
			`"fields":["id","name"],"order":"name, id"}`)
		fmt.Fprint(w, `[{"id":4,"name":"Ada"},{"id":7,"name":"Lin"}]`)
	})

	got, err := client.ListTicketTeamMembers(context.Background(), 3)
	require.NoError(t, err)
	assert.Equal(t, []TicketUser{{ID: 4, Name: "Ada"}, {ID: 7, Name: "Lin"}}, got)
}

func TestListTicketTeamStagesUsesTeamCompanyAndGlobalStages(t *testing.T) {
	var client *Client

	client = ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/json/2/helpdesk.ticket.team/search_read":
			assertJSON(t, requestParams(t, r), `{"domain":[["id","=",3]],"fields":["id","company_id"],`+
				`"limit":1,"context":{"active_test":false}}`)
			fmt.Fprint(w, `[{"id":3,"company_id":[2,"Subsidiary"]}]`)
		case "/json/2/helpdesk.ticket.stage/search_read":
			assertJSON(t, requestParams(t, r)["domain"], `[["company_id","in",[false,2]],"|",`+
				`["team_ids","=",false],["team_ids","=",3]]`)
			fmt.Fprint(w, `[{"id":1,"name":"New"}]`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})

	got, err := client.ListTicketTeamStages(context.Background(), 3)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "New", got[0].Name)
}

func TestTeamMetadataRejectsInvalidIDs(t *testing.T) {
	var (
		calls  int
		client *Client
	)

	client = ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) { calls++ })
	_, err := client.ListTicketTeamMembers(context.Background(), 0)
	assert.ErrorIs(t, err, odoo.ErrInvalidArgument)
	_, err = client.ListTicketTeamStages(context.Background(), -1)
	assert.ErrorIs(t, err, odoo.ErrInvalidArgument)
	assert.Zero(t, calls)
}
