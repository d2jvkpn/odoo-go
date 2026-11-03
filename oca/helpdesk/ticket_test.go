package helpdesk

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	odoo "github.com/d2jvkpn/odoo-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ticketTestClient builds a helpdesk Client against a mock HTTP server.
func ticketTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()

	var server *httptest.Server

	server = httptest.NewServer(handler)
	t.Cleanup(server.Close)

	return NewClient(odoo.NewClient(server.URL, "test-key", "test-db"))
}

// assertJSON compares the JSON encoding of got with want, ignoring formatting and key order.
func assertJSON(t *testing.T, got any, want string) {
	t.Helper()

	var (
		data []byte
		err  error
	)

	data, err = json.Marshal(got)
	if !assert.NoError(t, err) {
		return
	}
	assert.JSONEq(t, want, string(data))
}

// requestParams decodes the JSON request body. It is safe to call from handler goroutines.
func requestParams(t *testing.T, r *http.Request) map[string]any {
	t.Helper()

	var (
		params map[string]any
		err    error
	)

	err = json.NewDecoder(r.Body).Decode(&params)
	assert.NoError(t, err)

	return params
}

const ticketRow = `{"id":7,"name":"Broken pump","number":"HT00007","description":"<p>leaks</p>",` +
	`"partner_id":[15,"Acme"],"sale_order_ids":[41,42],"stage_id":[2,"In Progress"],` +
	`"team_id":false,"user_id":[2,"Administrator"],"priority":"2","channel_id":[1,"Web"],` +
	`"category_id":false,"tag_ids":[7],"product_id":false,"closed":false,` +
	`"message_ids":[17677,17686],"create_date":"2026-11-03 02:52:45","write_date":"2026-11-03 03:00:00"}`

func TestSearchTickets(t *testing.T) {
	client := ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/json/2/helpdesk.ticket/search_read", r.URL.Path)
		params := requestParams(t, r)
		assertJSON(t, params, `{"domain":[],"fields":["id","name","number","description","partner_id",`+
			`"sale_order_ids","stage_id","team_id","user_id","priority","channel_id","category_id",`+
			`"tag_ids","product_id","closed","message_ids","create_date","write_date"],"limit":100,"offset":0,"order":"id desc"}`)
		fmt.Fprint(w, `[`+ticketRow+`]`)
	})

	got, err := client.SearchTickets(context.Background(), TicketQuery{})
	require.NoError(t, err)
	require.Len(t, got, 1)
	ticket := got[0]
	assert.Equal(t, 7, ticket.ID)
	assert.Equal(t, "Broken pump", ticket.Name)
	assert.Equal(t, "HT00007", ticket.Number)
	assert.Equal(t, 15, ticket.Partner.ID)
	assert.Equal(t, "Acme", ticket.Partner.Name)
	assert.Equal(t, []int{41, 42}, ticket.SaleOrderIDs)
	assert.Equal(t, 2, ticket.Stage.ID)
	assert.Equal(t, "In Progress", ticket.Stage.Name)
	assert.Equal(t, "2", ticket.Priority)
	assert.Equal(t, []int{7}, ticket.TagIDs)
	assert.False(t, ticket.Closed)
}

func TestGetTicket(t *testing.T) {
	client := ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/json/2/helpdesk.ticket/search_read", r.URL.Path)
		assertJSON(t, requestParams(t, r), `{"domain":[["id","=",7]],"fields":["id","name","number",`+
			`"description","partner_id","sale_order_ids","stage_id","team_id","user_id","priority",`+
			`"channel_id","category_id","tag_ids","product_id","closed","message_ids","create_date","write_date"],`+
			`"limit":1,"offset":0,"order":"id desc"}`)
		fmt.Fprint(w, `[`+ticketRow+`]`)
	})

	got, err := client.GetTicket(context.Background(), 7)
	require.NoError(t, err)
	assert.Equal(t, 7, got.ID)

	_, err = client.GetTicket(context.Background(), 0)
	assert.ErrorIs(t, err, odoo.ErrInvalidArgument)
}

func TestGetTicketNotFound(t *testing.T) {
	client := ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[]`)
	})
	_, err := client.GetTicket(context.Background(), 7)
	assert.ErrorIs(t, err, ErrTicketNotFound)
}

func TestCountTickets(t *testing.T) {
	client := ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/json/2/helpdesk.ticket/search_count", r.URL.Path)
		assertJSON(t, requestParams(t, r), `{"domain":[]}`)
		fmt.Fprint(w, `3`)
	})

	got, err := client.CountTickets(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, 3, got)
}

func TestListTicketStages(t *testing.T) {
	client := ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/json/2/helpdesk.ticket.stage/search_read", r.URL.Path)
		assertJSON(t, requestParams(t, r), `{"domain":[],"fields":["id","name","sequence","closed","fold"],`+
			`"order":"sequence"}`)
		fmt.Fprint(w, `[{"id":1,"name":"New","sequence":1,"closed":false,"fold":false},`+
			`{"id":4,"name":"Done","sequence":4,"closed":true,"fold":true}]`)
	})

	got, err := client.ListTicketStages(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "New", got[0].Name)
	assert.False(t, got[0].Closed)
	assert.Equal(t, "Done", got[1].Name)
	assert.True(t, got[1].Closed)
	assert.True(t, got[1].Fold)
}

func TestListTicketTeams(t *testing.T) {
	client := ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/json/2/helpdesk.ticket.team/search_read", r.URL.Path)
		assertJSON(t, requestParams(t, r), `{"domain":[],"fields":["id","name"],"order":"sequence"}`)
		fmt.Fprint(w, `[{"id":1,"name":"Support"}]`)
	})

	got, err := client.ListTicketTeams(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "Support", got[0].Name)
}

func TestListTicketChannels(t *testing.T) {
	client := ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/json/2/helpdesk.ticket.channel/search_read", r.URL.Path)
		assertJSON(t, requestParams(t, r), `{"domain":[],"fields":["id","name"]}`)
		fmt.Fprint(w, `[{"id":1,"name":"Web"},{"id":2,"name":"Email"}]`)
	})

	got, err := client.ListTicketChannels(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "Web", got[0].Name)
	assert.Equal(t, "Email", got[1].Name)
}
