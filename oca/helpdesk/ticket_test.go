package helpdesk

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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
	`"partner_id":[15,"Acme"],"partner_name":"Acme contact","partner_email":"help@acme.test",` +
	`"stage_id":[2,"In Progress"],` +
	`"team_id":false,"user_id":[2,"Administrator"],"priority":"2","channel_id":[1,"Web"],` +
	`"category_id":false,"tag_ids":[7],"company_id":[1,"Main Company"],"active":true,"closed":false,` +
	`"unattended":true,"color":4,"kanban_state":"blocked","sequence":12,` +
	`"last_stage_update":"2026-11-03 02:59:00","assigned_date":false,"closed_date":false,` +
	`"message_ids":[17677,17686],"create_date":"2026-11-03 02:52:45","write_date":"2026-11-03 03:00:00"}`

func TestSearchTickets(t *testing.T) {
	client := ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/json/2/helpdesk.ticket/search_read", r.URL.Path)
		params := requestParams(t, r)
		assertJSON(t, params, `{"domain":[],"fields":["id","name","number","description","partner_id",`+
			`"partner_name","partner_email","stage_id","team_id","user_id","priority","channel_id",`+
			`"category_id","tag_ids","company_id","active","closed","unattended","color","kanban_state",`+
			`"sequence","last_stage_update","assigned_date","closed_date","message_ids","create_date",`+
			`"write_date"],"limit":100,"offset":0,"order":"id desc"}`)
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
	assert.Empty(t, ticket.SaleOrderIDs)
	assert.Equal(t, 2, ticket.Stage.ID)
	assert.Equal(t, "In Progress", ticket.Stage.Name)
	assert.Equal(t, "2", ticket.Priority)
	assert.Equal(t, []int{7}, ticket.TagIDs)
	assert.Equal(t, 1, ticket.Company.ID)
	assert.True(t, ticket.Active)
	assert.True(t, ticket.Unattended)
	assert.Equal(t, "blocked", string(ticket.KanbanState))
	assert.False(t, ticket.Closed)
}

func TestSearchTicketsIncludesOptionalAddonFieldsOnRequest(t *testing.T) {
	var client *Client

	client = ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		var params map[string]any

		params = requestParams(t, r)
		assertJSON(t, params["fields"], `["id","name","number","description","partner_id",`+
			`"partner_name","partner_email","stage_id","team_id","user_id","priority","channel_id",`+
			`"category_id","tag_ids","company_id","active","closed","unattended","color","kanban_state",`+
			`"sequence","last_stage_update","assigned_date","closed_date","message_ids","create_date",`+
			`"write_date","sale_order_ids","product_id"]`)
		fmt.Fprint(w, `[{"id":7,"sale_order_ids":[41,42],"product_id":[8,"Pump"]}]`)
	})

	got, err := client.SearchTickets(context.Background(), TicketQuery{
		IncludeSaleOrders: true,
		IncludeProduct:    true,
	})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, []int{41, 42}, got[0].SaleOrderIDs)
	assert.Equal(t, 8, got[0].Product.ID)
}

func TestSearchTicketsIncludesArchivedAndValidatesPagination(t *testing.T) {
	var (
		calls  int
		client *Client
		err    error
	)

	client = ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		assertJSON(t, requestParams(t, r)["context"], `{"active_test":false}`)
		fmt.Fprint(w, `[]`)
	})
	_, err = client.SearchTickets(context.Background(), TicketQuery{IncludeArchived: true})
	require.NoError(t, err)
	assert.Equal(t, 1, calls)

	_, err = client.SearchTickets(context.Background(), TicketQuery{Limit: -1})
	assert.ErrorIs(t, err, odoo.ErrInvalidArgument)
	_, err = client.SearchTickets(context.Background(), TicketQuery{Offset: -1})
	assert.ErrorIs(t, err, odoo.ErrInvalidArgument)
	assert.Equal(t, 1, calls)
}

func TestTicketReadsUnsetPriority(t *testing.T) {
	for _, priority := range []string{"false", "null", `""`} {
		t.Run(priority, func(t *testing.T) {
			var (
				client  *Client
				tickets []Ticket
				ticket  *Ticket
				err     error
			)

			client = ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/json/2/helpdesk.ticket/search_read", r.URL.Path)
				fmt.Fprint(w, `[`+strings.Replace(ticketRow, `"priority":"2"`, `"priority":`+priority, 1)+`]`)
			})
			tickets, err = client.SearchTickets(context.Background(), TicketQuery{})
			require.NoError(t, err)
			require.Len(t, tickets, 1)
			assert.Equal(t, "", tickets[0].Priority)
			assert.Equal(t, 7, tickets[0].ID)
			assert.Empty(t, tickets[0].SaleOrderIDs)

			ticket, err = client.GetTicket(context.Background(), 7)
			require.NoError(t, err)
			assert.Equal(t, "", ticket.Priority)
			assert.Equal(t, 7, ticket.ID)
		})
	}
}

func TestGetTicket(t *testing.T) {
	client := ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/json/2/helpdesk.ticket/search_read", r.URL.Path)
		assertJSON(t, requestParams(t, r), `{"domain":[["id","=",7]],"fields":["id","name","number",`+
			`"description","partner_id","partner_name","partner_email","stage_id","team_id","user_id",`+
			`"priority","channel_id","category_id","tag_ids","company_id","active","closed","unattended",`+
			`"color","kanban_state","sequence","last_stage_update","assigned_date","closed_date",`+
			`"message_ids","create_date","write_date"],"limit":1,"offset":0,"order":"id desc",`+
			`"context":{"active_test":false}}`)
		fmt.Fprint(w, `[`+ticketRow+`]`)
	})

	got, err := client.GetTicket(context.Background(), 7)
	require.NoError(t, err)
	assert.Equal(t, 7, got.ID)

	_, err = client.GetTicket(context.Background(), 0)
	assert.ErrorIs(t, err, odoo.ErrInvalidArgument)
}

func TestGetTicketWithQueryIncludesOptionalFieldsAndArchived(t *testing.T) {
	var client *Client

	client = ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		var params map[string]any

		params = requestParams(t, r)
		assertJSON(t, params["domain"], `[["id","=",7]]`)
		assertJSON(t, params["context"], `{"active_test":false}`)
		assertJSON(t, params["fields"], `["id","name","number","description","partner_id",`+
			`"partner_name","partner_email","stage_id","team_id","user_id","priority","channel_id",`+
			`"category_id","tag_ids","company_id","active","closed","unattended","color","kanban_state",`+
			`"sequence","last_stage_update","assigned_date","closed_date","message_ids","create_date",`+
			`"write_date","sale_order_ids"]`)
		fmt.Fprint(w, `[{"id":7,"sale_order_ids":[41]}]`)
	})

	got, err := client.GetTicketWithQuery(context.Background(), 7, TicketQuery{
		Domain:            []any{[]any{"name", "=", "ignored"}},
		Limit:             99,
		IncludeSaleOrders: true,
	})
	require.NoError(t, err)
	assert.Equal(t, []int{41}, got.SaleOrderIDs)
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

func TestCountTicketsWithQueryIncludesArchived(t *testing.T) {
	var client *Client

	client = ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/json/2/helpdesk.ticket/search_count", r.URL.Path)
		assertJSON(t, requestParams(t, r), `{"domain":[["closed","=",false]],`+
			`"context":{"active_test":false}}`)
		fmt.Fprint(w, `4`)
	})

	got, err := client.CountTicketsWithQuery(context.Background(), TicketQuery{
		Domain:          []any{[]any{"closed", "=", false}},
		IncludeArchived: true,
	})
	require.NoError(t, err)
	assert.Equal(t, 4, got)
}

func TestAllTicketsPaginatesAndReportsInvalidQuery(t *testing.T) {
	var (
		client  *Client
		errList []error
		ids     []int
		domains []any
		offsets []float64
	)

	client = ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		var params map[string]any

		params = requestParams(t, r)
		offsets = append(offsets, params["offset"].(float64))
		domains = append(domains, params["domain"])
		assert.Equal(t, float64(2), params["limit"])
		if len(domains) == 1 {
			fmt.Fprint(w, `[{"id":5},{"id":4}]`)
		} else {
			fmt.Fprint(w, `[{"id":3}]`)
		}
	})
	for ticket, err := range client.AllTickets(context.Background(), TicketQuery{Limit: 2}) {
		require.NoError(t, err)
		ids = append(ids, ticket.ID)
	}
	assert.Equal(t, []int{5, 4, 3}, ids)
	assert.Equal(t, []float64{0, 0}, offsets)
	assert.Equal(t, []any{}, domains[0])
	assert.Equal(t, []any{[]any{"id", "<", float64(4)}}, domains[1])

	for _, err := range client.AllTickets(context.Background(), TicketQuery{Offset: -1}) {
		errList = append(errList, err)
	}
	require.Len(t, errList, 1)
	assert.ErrorIs(t, errList[0], odoo.ErrInvalidArgument)
}

func TestAllTicketsPagesByOffsetForCustomOrder(t *testing.T) {
	var (
		ids     []int
		offsets []float64
	)

	client := ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		params := requestParams(t, r)
		offsets = append(offsets, params["offset"].(float64))
		assert.Equal(t, []any{}, params["domain"])
		if params["offset"].(float64) == 0 {
			fmt.Fprint(w, `[{"id":1},{"id":2}]`)
		} else {
			fmt.Fprint(w, `[{"id":3}]`)
		}
	})
	for ticket, err := range client.AllTickets(context.Background(), TicketQuery{Limit: 2, Order: "name"}) {
		require.NoError(t, err)
		ids = append(ids, ticket.ID)
	}
	assert.Equal(t, []int{1, 2, 3}, ids)
	assert.Equal(t, []float64{0, 2}, offsets)
}

func TestListTicketStages(t *testing.T) {
	client := ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/json/2/helpdesk.ticket.stage/search_read", r.URL.Path)
		assertJSON(t, requestParams(t, r), `{"domain":[],"fields":["id","name","description","sequence",`+
			`"active","unattended","closed","close_from_portal","mail_template_id","fold","company_id",`+
			`"team_ids"],`+
			`"order":"sequence"}`)
		fmt.Fprint(w, `[{"id":1,"name":"New","description":false,"sequence":1,"active":true,`+
			`"unattended":true,"closed":false,"close_from_portal":false,"mail_template_id":false,`+
			`"fold":false,"company_id":[1,"Main Company"],"team_ids":[]},`+
			`{"id":4,"name":"Done","description":"<p>Done</p>","sequence":4,"active":true,`+
			`"unattended":false,"closed":true,"close_from_portal":true,"mail_template_id":false,`+
			`"fold":true,"company_id":false,"team_ids":[2]}]`)
	})

	got, err := client.ListTicketStages(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "New", got[0].Name)
	assert.False(t, got[0].Closed)
	assert.True(t, got[0].Unattended)
	assert.Equal(t, "Done", got[1].Name)
	assert.True(t, got[1].Closed)
	assert.True(t, got[1].Fold)
	assert.Equal(t, "<p>Done</p>", string(got[1].Description))
	assert.Equal(t, []int{2}, got[1].TeamIDs)
}

func TestListTicketTeams(t *testing.T) {
	client := ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/json/2/helpdesk.ticket.team/search_read", r.URL.Path)
		assertJSON(t, requestParams(t, r), `{"domain":[],"fields":["id","name","complete_name",`+
			`"sequence","active","user_id","user_ids","category_ids","company_id","alias_id","color",`+
			`"show_in_portal","parent_id"],"order":"sequence"}`)
		fmt.Fprint(w, `[{"id":1,"name":"Support","complete_name":"Operations / Support",`+
			`"sequence":2,"active":true,"user_id":[3,"Lead"],"user_ids":[3,4],"category_ids":[8],`+
			`"company_id":[1,"Main Company"],"alias_id":[6,"support"],"color":5,`+
			`"show_in_portal":true,"parent_id":[9,"Operations"]}]`)
	})

	got, err := client.ListTicketTeams(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "Support", got[0].Name)
	assert.Equal(t, "Operations / Support", got[0].CompleteName)
	assert.Equal(t, []int{3, 4}, got[0].UserIDs)
	assert.Equal(t, 3, got[0].Leader.ID)
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
