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

func TestListTicketMessages(t *testing.T) {
	client := ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/json/2/helpdesk.ticket/search_read":
			fmt.Fprint(w, `[`+ticketRow+`]`)
		case "/json/2/mail.message/search_read":
			assertJSON(t, requestParams(t, r), `{"domain":[["model","=","helpdesk.ticket"],`+
				`["res_id","=",7]],"fields":["id","body","subject","message_type","subtype_id",`+
				`"author_id","email_from","date","parent_id"],"order":"id asc"}`)
			fmt.Fprint(w, `[{"id":17686,"body":"","subject":false,"message_type":"notification",`+
				`"subtype_id":[2,"Note"],"author_id":[3,"Administrator"],"email_from":false,`+
				`"date":"2026-11-03 02:53:49","parent_id":false},`+
				`{"id":17687,"body":"<p>resolved</p>","subject":"closed","message_type":"comment",`+
				`"subtype_id":[1,"Discussions"],"author_id":[15,"Acme"],"email_from":"acme@x.io",`+
				`"date":"2026-11-03 03:00:00","parent_id":false}]`)
		}
	})

	got, err := client.ListTicketMessages(context.Background(), 7)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, 17686, got[0].ID)
	assert.Equal(t, "notification", got[0].MessageType)
	assert.Equal(t, 2, got[0].Subtype.ID)
	assert.Equal(t, "Note", got[0].Subtype.Name)
	assert.Equal(t, 17687, got[1].ID)
	assert.Equal(t, "comment", got[1].MessageType)
	assert.Equal(t, 15, got[1].Author.ID)
	assert.Equal(t, "acme@x.io", string(got[1].EmailFrom))

	_, err = client.ListTicketMessages(context.Background(), 0)
	assert.ErrorIs(t, err, odoo.ErrInvalidArgument)
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
	assertJSON(t, posted, `{"ids":[7],"body":"<p>hi</p>","subtype_xmlid":"mail.mt_comment"}`)
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
	assertJSON(t, posted, `{"ids":[7],"body":"internal","subtype_xmlid":"mail.mt_note"}`)
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
