package helpdesk

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	odoo "github.com/d2jvkpn/odoo-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListTicketMessages(t *testing.T) {
	client := ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/json/2/helpdesk.ticket/search_read":
			fmt.Fprint(w, `[`+ticketRow+`]`)
		case "/json/2/mail.message/search_read":
			assertJSON(t, requestParams(t, r), `{"domain":[["model","=","helpdesk.ticket"],`+
				`["res_id","=",7]],"fields":["id","body","subject","message_type","subtype_id",`+
				`"author_id","email_from","date","parent_id","attachment_ids","partner_ids",`+
				`"is_internal"],"order":"id asc"}`)
			fmt.Fprint(w, `[{"id":17686,"body":false,"subject":false,"message_type":"notification",`+
				`"subtype_id":[2,"Note"],"author_id":[3,"Administrator"],"email_from":false,`+
				`"date":"2026-11-03 02:53:49","parent_id":false,"attachment_ids":[],`+
				`"partner_ids":[],"is_internal":true},`+
				`{"id":17687,"body":"<p>resolved</p>","subject":"closed","message_type":"comment",`+
				`"subtype_id":[1,"Discussions"],"author_id":[15,"Acme"],"email_from":"acme@x.io",`+
				`"date":"2026-11-03 03:00:00","parent_id":false,"attachment_ids":[51],`+
				`"partner_ids":[15,16],"is_internal":false}]`)
		}
	})

	got, err := client.ListTicketMessages(context.Background(), 7)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, 17686, got[0].ID)
	assert.Equal(t, "", got[0].Body)
	assert.Equal(t, "notification", got[0].MessageType)
	assert.Equal(t, 2, got[0].Subtype.ID)
	assert.Equal(t, "Note", got[0].Subtype.Name)
	assert.True(t, got[0].IsInternal)
	assert.Equal(t, 17687, got[1].ID)
	assert.Equal(t, "<p>resolved</p>", got[1].Body)
	assert.Equal(t, "comment", got[1].MessageType)
	assert.Equal(t, 15, got[1].Author.ID)
	assert.Equal(t, "acme@x.io", string(got[1].EmailFrom))
	assert.Equal(t, []int{51}, got[1].AttachmentIDs)
	assert.Equal(t, []int{15, 16}, got[1].PartnerIDs)

	_, err = client.ListTicketMessages(context.Background(), 0)
	assert.ErrorIs(t, err, odoo.ErrInvalidArgument)
}

func TestSearchTicketMessagesQuery(t *testing.T) {
	var client *Client

	client = ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/json/2/helpdesk.ticket/search_read":
			fmt.Fprint(w, `[{"id":7}]`)
		case "/json/2/mail.message/search_read":
			assertJSON(t, requestParams(t, r), `{"domain":[["model","=","helpdesk.ticket"],`+
				`["res_id","=",7],["message_type","=","comment"]],"fields":["id","body",`+
				`"subject","message_type","subtype_id","author_id","email_from","date","parent_id",`+
				`"attachment_ids","partner_ids","is_internal"],"limit":2,"offset":3,"order":"date desc"}`)
			fmt.Fprint(w, `[]`)
		}
	})

	got, err := client.SearchTicketMessages(context.Background(), 7, MessageQuery{
		Limit:  2,
		Offset: 3,
		Order:  "date desc",
		Domain: []any{[]any{"message_type", "=", "comment"}},
	})
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestSearchTicketMessagesRetriesRead(t *testing.T) {
	var (
		attempts int
		client   *Client
	)

	client = ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/json/2/helpdesk.ticket/search_read":
			fmt.Fprint(w, `[{"id":7}]`)
		case "/json/2/mail.message/search_read":
			attempts++
			if attempts == 1 {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			fmt.Fprint(w, `[]`)
		}
	})
	client.Retry = odoo.RetryPolicy{MaxRetries: 1, Backoff: time.Millisecond, NoJitter: true}

	_, err := client.SearchTicketMessages(context.Background(), 7, MessageQuery{})
	require.NoError(t, err)
	assert.Equal(t, 2, attempts)
}

func TestSearchTicketMessagesRejectsBadQuery(t *testing.T) {
	var (
		calls  int
		client *Client
	)

	client = ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) { calls++ })
	for name, query := range map[string]MessageQuery{
		"negative limit":  {Limit: -1},
		"negative offset": {Offset: -1},
	} {
		_, err := client.SearchTicketMessages(context.Background(), 7, query)
		assert.ErrorIs(t, err, odoo.ErrInvalidArgument, name)
	}
	assert.Zero(t, calls)
}

func TestListTicketMessagesNotFound(t *testing.T) {
	client := ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[]`)
	})
	_, err := client.ListTicketMessages(context.Background(), 7)
	assert.ErrorIs(t, err, ErrTicketNotFound)
}

func TestPostTicketMessageComment(t *testing.T) {
	var posted map[string]any

	client := ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/json/2/helpdesk.ticket/search_read":
			fmt.Fprint(w, `[{"id":7}]`)
		case "/json/2/helpdesk.ticket/message_post":
			posted = requestParams(t, r)
			fmt.Fprint(w, `true`)
		}
	})

	require.NoError(t, client.PostTicketMessage(context.Background(), 7, "<p>hi</p>", false))
	require.NotNil(t, posted)
	assertJSON(t, posted, `{"ids":[7],"body":"<p>hi</p>","subtype_xmlid":"mail.mt_comment",`+
		`"message_type":"comment","body_is_html":true}`)
}

func TestPostTicketMessageNote(t *testing.T) {
	var posted map[string]any

	client := ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/json/2/helpdesk.ticket/search_read":
			fmt.Fprint(w, `[{"id":7}]`)
		case "/json/2/helpdesk.ticket/message_post":
			posted = requestParams(t, r)
			fmt.Fprint(w, `true`)
		}
	})

	require.NoError(t, client.PostTicketMessage(context.Background(), 7, "internal", true))
	require.NotNil(t, posted)
	assertJSON(t, posted, `{"ids":[7],"body":"internal","subtype_xmlid":"mail.mt_note",`+
		`"message_type":"comment","body_is_html":true}`)
}

func TestPostTicketMessageRejectsBadInput(t *testing.T) {
	var calls int

	client := ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) { calls++ })
	for name, in := range map[string]struct {
		id   int
		body string
	}{
		"zero id":    {id: 0, body: "x"},
		"blank body": {id: 7, body: " "},
	} {
		err := client.PostTicketMessage(context.Background(), in.id, in.body, false)
		assert.ErrorIs(t, err, odoo.ErrInvalidArgument, name)
	}
	assert.Zero(t, calls)
}

func TestPostTicketMessageNotFound(t *testing.T) {
	client := ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[]`)
	})
	err := client.PostTicketMessage(context.Background(), 7, "x", false)
	assert.ErrorIs(t, err, ErrTicketNotFound)
}

func TestPostTicketMessageWithOptions(t *testing.T) {
	var (
		client *Client
		posted map[string]any
	)

	client = ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/json/2/helpdesk.ticket/search_read":
			fmt.Fprint(w, `[{"id":7}]`)
		case "/json/2/mail.message/search_read":
			assertJSON(t, requestParams(t, r), `{"domain":[["id","=",17687],["model","=",`+
				`"helpdesk.ticket"],["res_id","=",7]],"fields":["id"],"limit":1}`)
			fmt.Fprint(w, `[{"id":17687}]`)
		case "/json/2/ir.attachment/search_read":
			assertJSON(t, requestParams(t, r), `{"domain":[["id","in",[51,52]],["res_model","=",`+
				`"helpdesk.ticket"],["res_id","=",7]],"fields":["id"]}`)
			fmt.Fprint(w, `[{"id":51},{"id":52}]`)
		case "/json/2/helpdesk.ticket/message_post":
			posted = requestParams(t, r)
			fmt.Fprint(w, `[17690]`)
		}
	})

	got, err := client.PostTicketMessageWithOptions(context.Background(), 7, PostTicketMessageRequest{
		Body:          " ",
		Subject:       "diagnostics",
		Internal:      true,
		ParentID:      17687,
		PartnerIDs:    []int{15, 16},
		AttachmentIDs: []int{51, 52},
	})
	require.NoError(t, err)
	assert.Equal(t, 17690, got)
	assertJSON(t, posted, `{"ids":[7],"body":" ","subject":"diagnostics","parent_id":17687,`+
		`"partner_ids":[15,16],"attachment_ids":[51,52],"subtype_xmlid":"mail.mt_note",`+
		`"message_type":"comment","body_is_html":true}`)
}

func TestPostTicketMessageWithOptionsChecksChildOwnership(t *testing.T) {
	for name, input := range map[string]PostTicketMessageRequest{
		"parent":     {Body: "x", ParentID: 17687},
		"attachment": {Body: "x", AttachmentIDs: []int{51}},
	} {
		t.Run(name, func(t *testing.T) {
			var (
				posts  int
				client *Client
			)

			client = ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/json/2/helpdesk.ticket/search_read":
					fmt.Fprint(w, `[{"id":7}]`)
				case "/json/2/mail.message/search_read", "/json/2/ir.attachment/search_read":
					fmt.Fprint(w, `[]`)
				case "/json/2/helpdesk.ticket/message_post":
					posts++
				}
			})

			_, err := client.PostTicketMessageWithOptions(context.Background(), 7, input)
			require.Error(t, err)
			assert.Zero(t, posts)
		})
	}
}

func TestPostTicketMessageWithOptionsRejectsBadInput(t *testing.T) {
	var (
		calls  int
		client *Client
	)

	client = ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) { calls++ })
	for name, test := range map[string]struct {
		id    int
		input PostTicketMessageRequest
	}{
		"zero ticket":       {input: PostTicketMessageRequest{Body: "x"}},
		"empty content":     {id: 7},
		"negative parent":   {id: 7, input: PostTicketMessageRequest{Body: "x", ParentID: -1}},
		"zero partner":      {id: 7, input: PostTicketMessageRequest{Body: "x", PartnerIDs: []int{0}}},
		"zero attachment":   {id: 7, input: PostTicketMessageRequest{Body: "x", AttachmentIDs: []int{0}}},
		"duplicate partner": {id: 7, input: PostTicketMessageRequest{Body: "x", PartnerIDs: []int{2, 2}}},
	} {
		_, err := client.PostTicketMessageWithOptions(context.Background(), test.id, test.input)
		assert.ErrorIs(t, err, odoo.ErrInvalidArgument, name)
	}
	assert.Zero(t, calls)
}

func TestPostTicketMessageWithOptionsValidatesResult(t *testing.T) {
	var client *Client

	client = ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/json/2/helpdesk.ticket/search_read":
			fmt.Fprint(w, `[{"id":7}]`)
		case "/json/2/helpdesk.ticket/message_post":
			fmt.Fprint(w, `[]`)
		}
	})

	_, err := client.PostTicketMessageWithOptions(
		context.Background(), 7, PostTicketMessageRequest{Body: "x"},
	)
	assert.ErrorIs(t, err, odoo.ErrUnexpectedResponse)
}

func TestPostTicketMessageWithOptionsDoesNotRetryWrite(t *testing.T) {
	var (
		posts  int
		client *Client
	)

	client = ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/json/2/helpdesk.ticket/search_read":
			fmt.Fprint(w, `[{"id":7}]`)
		case "/json/2/helpdesk.ticket/message_post":
			posts++
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	})
	client.Retry = odoo.RetryPolicy{MaxRetries: 3, Backoff: time.Millisecond, NoJitter: true}

	_, err := client.PostTicketMessageWithOptions(
		context.Background(), 7, PostTicketMessageRequest{Body: "x"},
	)
	require.Error(t, err)
	assert.Equal(t, 1, posts)
}
