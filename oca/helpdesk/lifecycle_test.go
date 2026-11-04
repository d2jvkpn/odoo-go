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

func TestSetTicketActiveFindsArchivedTicket(t *testing.T) {
	var (
		calls  int
		client *Client
	)

	client = ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch r.URL.Path {
		case "/json/2/helpdesk.ticket/search_read":
			assertJSON(t, requestParams(t, r), `{"domain":[["id","=",7]],"fields":["id"],`+
				`"limit":1,"context":{"active_test":false}}`)
			fmt.Fprint(w, `[{"id":7}]`)
		case "/json/2/helpdesk.ticket/write":
			assertJSON(t, requestParams(t, r), `{"ids":[7],"vals":{"active":true}}`)
			fmt.Fprint(w, `true`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})

	require.NoError(t, client.SetTicketActive(context.Background(), 7, true))
	assert.Equal(t, 2, calls)
}

func TestTicketLifecycleRejectsInvalidOrMissingTicket(t *testing.T) {
	var (
		calls  int
		client *Client
		err    error
	)

	client = ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		fmt.Fprint(w, `[]`)
	})
	err = client.SetTicketActive(context.Background(), 0, false)
	assert.ErrorIs(t, err, odoo.ErrInvalidArgument)
	err = client.DeleteTicket(context.Background(), -1)
	assert.ErrorIs(t, err, odoo.ErrInvalidArgument)
	_, err = client.CopyTicket(context.Background(), 0, UpdateTicketRequest{})
	assert.ErrorIs(t, err, odoo.ErrInvalidArgument)
	assert.Zero(t, calls)

	err = client.SetTicketActive(context.Background(), 7, false)
	assert.ErrorIs(t, err, ErrTicketNotFound)
	assert.Equal(t, 1, calls)
}

func TestDeleteTicketChecksArchivedTicketAndUnlinksOnce(t *testing.T) {
	var client *Client

	client = ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/json/2/helpdesk.ticket/search_read":
			assertJSON(t, requestParams(t, r)["context"], `{"active_test":false}`)
			fmt.Fprint(w, `[{"id":7}]`)
		case "/json/2/helpdesk.ticket/unlink":
			assertJSON(t, requestParams(t, r), `{"ids":[7]}`)
			fmt.Fprint(w, `true`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})

	require.NoError(t, client.DeleteTicket(context.Background(), 7))
}

func TestCopyTicketAppliesValidatedDefaults(t *testing.T) {
	var (
		client *Client
		name   string = "Copy of broken pump"
	)

	client = ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/json/2/helpdesk.ticket/search_read":
			fmt.Fprint(w, `[{"id":7}]`)
		case "/json/2/helpdesk.ticket/copy":
			assertJSON(t, requestParams(t, r), `{"ids":[7],"default":{"name":"Copy of broken pump"}}`)
			fmt.Fprint(w, `101`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})

	id, err := client.CopyTicket(context.Background(), 7, UpdateTicketRequest{Name: &name})
	require.NoError(t, err)
	assert.Equal(t, 101, id)
}

func TestCopyTicketAllowsEmptyPatchAndRejectsBadPatch(t *testing.T) {
	var (
		calls  int
		client *Client
		badIDs = odoo.ReplaceIDs{0}
	)

	client = ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch r.URL.Path {
		case "/json/2/helpdesk.ticket/search_read":
			fmt.Fprint(w, `[{"id":7}]`)
		case "/json/2/helpdesk.ticket/copy":
			assertJSON(t, requestParams(t, r), `{"ids":[7],"default":{}}`)
			fmt.Fprint(w, `102`)
		}
	})

	id, err := client.CopyTicket(context.Background(), 7, UpdateTicketRequest{})
	require.NoError(t, err)
	assert.Equal(t, 102, id)
	assert.Equal(t, 2, calls)

	_, err = client.CopyTicket(context.Background(), 7, UpdateTicketRequest{TagIDs: &badIDs})
	assert.ErrorIs(t, err, odoo.ErrInvalidArgument)
	assert.Equal(t, 2, calls)
}
