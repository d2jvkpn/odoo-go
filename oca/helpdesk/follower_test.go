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

func TestListTicketFollowers(t *testing.T) {
	var calls int

	client := ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch r.URL.Path {
		case "/json/2/helpdesk.ticket/search_read":
			fmt.Fprint(w, `[{"id":7}]`)
		case "/json/2/mail.followers/search_read":
			assertJSON(t, requestParams(t, r), `{"domain":[["res_model","=","helpdesk.ticket"],`+
				`["res_id","=",7]],"fields":["id","partner_id","subtype_ids"],"order":"id asc"}`)
			fmt.Fprint(w, `[{"id":31,"partner_id":[15,"Acme"],"subtype_ids":[1,4]}]`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})

	got, err := client.ListTicketFollowers(context.Background(), 7)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, 31, got[0].ID)
	assert.Equal(t, 15, got[0].Partner.ID)
	assert.Equal(t, "Acme", got[0].Partner.Name)
	assert.Equal(t, []int{1, 4}, got[0].SubtypeIDs)
	assert.Equal(t, 2, calls)
}

func TestListTicketFollowersReturnsEmptySlice(t *testing.T) {
	client := ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/json/2/helpdesk.ticket/search_read" {
			fmt.Fprint(w, `[{"id":7}]`)
			return
		}
		fmt.Fprint(w, `[]`)
	})

	got, err := client.ListTicketFollowers(context.Background(), 7)
	require.NoError(t, err)
	assert.Empty(t, got)
	assert.NotNil(t, got)
}

func TestSubscribeTicket(t *testing.T) {
	var writes int

	client := ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/json/2/helpdesk.ticket/search_read":
			fmt.Fprint(w, `[{"id":7}]`)
		case "/json/2/helpdesk.ticket/message_subscribe":
			writes++
			assertJSON(t, requestParams(t, r), `{"ids":[7],"partner_ids":[15,16],"subtype_ids":[1,4]}`)
			fmt.Fprint(w, `true`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})

	err := client.SubscribeTicket(context.Background(), 7, []int{15, 16}, []int{1, 4})
	require.NoError(t, err)
	assert.Equal(t, 1, writes)
}

func TestSubscribeTicketRejectsFalseResult(t *testing.T) {
	client := ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/json/2/helpdesk.ticket/search_read" {
			fmt.Fprint(w, `[{"id":7}]`)
			return
		}
		fmt.Fprint(w, `false`)
	})

	err := client.SubscribeTicket(context.Background(), 7, []int{15}, nil)
	assert.ErrorIs(t, err, odoo.ErrUnexpectedResponse)
}

func TestUnsubscribeTicket(t *testing.T) {
	client := ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/json/2/helpdesk.ticket/search_read":
			fmt.Fprint(w, `[{"id":7}]`)
		case "/json/2/helpdesk.ticket/message_unsubscribe":
			assertJSON(t, requestParams(t, r), `{"ids":[7],"partner_ids":[15,16]}`)
			fmt.Fprint(w, `true`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})

	err := client.UnsubscribeTicket(context.Background(), 7, []int{15, 16})
	require.NoError(t, err)
}

func TestUnsubscribeTicketAcceptsOdooNullResult(t *testing.T) {
	client := ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/json/2/helpdesk.ticket/search_read" {
			fmt.Fprint(w, `[{"id":7}]`)
			return
		}
		fmt.Fprint(w, `null`)
	})

	err := client.UnsubscribeTicket(context.Background(), 7, []int{15})
	require.NoError(t, err)
}

func TestUnsubscribeTicketRejectsExplicitFalseResult(t *testing.T) {
	client := ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/json/2/helpdesk.ticket/search_read" {
			fmt.Fprint(w, `[{"id":7}]`)
			return
		}
		fmt.Fprint(w, `false`)
	})

	err := client.UnsubscribeTicket(context.Background(), 7, []int{15})
	assert.ErrorIs(t, err, odoo.ErrUnexpectedResponse)
}

func TestFollowerWritesRejectInvalidIDsBeforeCallingOdoo(t *testing.T) {
	var calls int

	client := ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) { calls++ })
	tests := map[string]func() error{
		"subscribe ticket":  func() error { return client.SubscribeTicket(context.Background(), 0, []int{1}, nil) },
		"subscribe empty":   func() error { return client.SubscribeTicket(context.Background(), 7, nil, nil) },
		"subscribe partner": func() error { return client.SubscribeTicket(context.Background(), 7, []int{0}, nil) },
		"subscribe subtype": func() error { return client.SubscribeTicket(context.Background(), 7, []int{1}, []int{-1}) },
		"unsubscribe ticket": func() error {
			return client.UnsubscribeTicket(context.Background(), -1, []int{1})
		},
		"unsubscribe empty": func() error { return client.UnsubscribeTicket(context.Background(), 7, nil) },
		"unsubscribe partner": func() error {
			return client.UnsubscribeTicket(context.Background(), 7, []int{-1})
		},
	}

	for name, call := range tests {
		t.Run(name, func(t *testing.T) {
			err := call()
			assert.ErrorIs(t, err, odoo.ErrInvalidArgument)
		})
	}
	assert.Zero(t, calls)
}

func TestFollowerWritesDoNotRetry(t *testing.T) {
	var writes int

	client := ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/json/2/helpdesk.ticket/search_read" {
			fmt.Fprint(w, `[{"id":7}]`)
			return
		}
		writes++
		http.Error(w, "try later", http.StatusServiceUnavailable)
	})
	client.Retry = odoo.RetryPolicy{
		MaxRetries: 2,
		Backoff:    time.Nanosecond,
		MaxBackoff: time.Nanosecond,
		NoJitter:   true,
	}

	err := client.SubscribeTicket(context.Background(), 7, []int{15}, nil)
	assert.Error(t, err)
	assert.Equal(t, 1, writes)
}
