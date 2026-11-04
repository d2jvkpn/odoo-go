package helpdesk

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	odoo "github.com/d2jvkpn/odoo-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScheduleTicketActivity(t *testing.T) {
	client := ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/json/2/helpdesk.ticket/search_read":
			fmt.Fprint(w, `[{"id":7}]`)
		case "/json/2/helpdesk.ticket/activity_schedule":
			assertJSON(t, requestParams(t, r), `{"ids":[7],"activity_type_id":3,"user_id":2,`+
				`"summary":"Call customer","note":"<p>Bring quote</p>","date_deadline":"2026-11-15"}`)
			fmt.Fprint(w, `[41]`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})

	id, err := client.ScheduleTicketActivity(context.Background(), 7, ActivityRequest{
		TypeID:       3,
		UserID:       2,
		Summary:      "Call customer",
		Note:         "<p>Bring quote</p>",
		DateDeadline: "2026-11-15",
	})
	require.NoError(t, err)
	assert.Equal(t, 41, id)
}

func TestScheduleTicketActivityUsesServerDefaults(t *testing.T) {
	client := ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/json/2/helpdesk.ticket/search_read" {
			fmt.Fprint(w, `[{"id":7}]`)
			return
		}
		assertJSON(t, requestParams(t, r), `{"ids":[7]}`)
		fmt.Fprint(w, `[41]`)
	})

	id, err := client.ScheduleTicketActivity(context.Background(), 7, ActivityRequest{})
	require.NoError(t, err)
	assert.Equal(t, 41, id)
}

func TestListTicketActivities(t *testing.T) {
	client := ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/json/2/helpdesk.ticket/search_read":
			fmt.Fprint(w, `[{"id":7}]`)
		case "/json/2/mail.activity/search_read":
			assertJSON(t, requestParams(t, r), `{"domain":[["res_model","=","helpdesk.ticket"],`+
				`["res_id","=",7]],"fields":["id","activity_type_id","user_id","summary","note",`+
				`"date_deadline","state"],"order":"date_deadline asc, id asc"}`)
			fmt.Fprint(w, `[{"id":41,"activity_type_id":[3,"Call"],"user_id":[2,"Admin"],`+
				`"summary":false,"note":false,"date_deadline":"2026-11-15","state":"planned"}]`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})

	got, err := client.ListTicketActivities(context.Background(), 7)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, 41, got[0].ID)
	assert.Equal(t, 3, got[0].ActivityType.ID)
	assert.Equal(t, 2, got[0].User.ID)
	assert.Empty(t, got[0].Summary)
	assert.Empty(t, got[0].Note)
	assert.Equal(t, "2026-11-15", got[0].DateDeadline)
	assert.Equal(t, "planned", got[0].State)
}

func TestListTicketActivitiesReturnsEmptySlice(t *testing.T) {
	client := ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/json/2/helpdesk.ticket/search_read" {
			fmt.Fprint(w, `[{"id":7}]`)
			return
		}
		fmt.Fprint(w, `[]`)
	})

	got, err := client.ListTicketActivities(context.Background(), 7)
	require.NoError(t, err)
	assert.Empty(t, got)
	assert.NotNil(t, got)
}

func TestUpdateTicketActivity(t *testing.T) {
	var activityCalls int
	date := "2026-11-18"
	summary := "Send revised quote"
	typeID := 4

	client := ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/json/2/mail.activity/search_read":
			activityCalls++
			assertJSON(t, requestParams(t, r), `{"domain":[["id","=",41],`+
				`["res_model","=","helpdesk.ticket"],["res_id","=",7]],"fields":["id"],"limit":1}`)
			fmt.Fprint(w, `[{"id":41}]`)
		case "/json/2/mail.activity/write":
			activityCalls++
			assertJSON(t, requestParams(t, r), `{"ids":[41],"vals":{"activity_type_id":4,`+
				`"summary":"Send revised quote","date_deadline":"2026-11-18"}}`)
			fmt.Fprint(w, `true`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})

	err := client.UpdateTicketActivity(context.Background(), 7, 41, UpdateActivityRequest{
		TypeID:       &typeID,
		Summary:      &summary,
		DateDeadline: &date,
	})
	require.NoError(t, err)
	assert.Equal(t, 2, activityCalls)
}

func TestActivityMutationRejectsOwnershipMismatch(t *testing.T) {
	var writes int

	client := ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/json/2/mail.activity/search_read" {
			fmt.Fprint(w, `[]`)
			return
		}
		writes++
	})
	summary := "wrong ticket"

	err := client.UpdateTicketActivity(context.Background(), 7, 41, UpdateActivityRequest{Summary: &summary})
	assert.Error(t, err)
	assert.Zero(t, writes)
}

func TestActivityMutationRejectsUnexpectedOwnershipRow(t *testing.T) {
	var writes int

	client := ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/json/2/mail.activity/search_read" {
			fmt.Fprint(w, `[{"id":99}]`)
			return
		}
		writes++
	})
	summary := "wrong activity"

	err := client.UpdateTicketActivity(context.Background(), 7, 41, UpdateActivityRequest{Summary: &summary})
	assert.Error(t, err)
	assert.Zero(t, writes)
}

func TestCompleteTicketActivity(t *testing.T) {
	client := ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/json/2/mail.activity/search_read":
			fmt.Fprint(w, `[{"id":41}]`)
		case "/json/2/mail.activity/action_feedback":
			assertJSON(t, requestParams(t, r), `{"ids":[41],"feedback":"Customer confirmed"}`)
			fmt.Fprint(w, `false`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})

	err := client.CompleteTicketActivity(context.Background(), 7, 41, "Customer confirmed")
	require.NoError(t, err)
}

func TestDeleteTicketActivity(t *testing.T) {
	client := ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/json/2/mail.activity/search_read":
			fmt.Fprint(w, `[{"id":41}]`)
		case "/json/2/mail.activity/unlink":
			assertJSON(t, requestParams(t, r), `{"ids":[41]}`)
			fmt.Fprint(w, `true`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})

	err := client.DeleteTicketActivity(context.Background(), 7, 41)
	require.NoError(t, err)
}

func TestDeleteTicketActivityRejectsFalseResult(t *testing.T) {
	client := ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/json/2/mail.activity/search_read" {
			fmt.Fprint(w, `[{"id":41}]`)
			return
		}
		fmt.Fprint(w, `false`)
	})

	err := client.DeleteTicketActivity(context.Background(), 7, 41)
	assert.ErrorIs(t, err, odoo.ErrUnexpectedResponse)
}

func TestListTicketActivityTypes(t *testing.T) {
	client := ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/json/2/mail.activity.type/search_read", r.URL.Path)
		assertJSON(t, requestParams(t, r), `{"domain":["|",["res_model","=",false],`+
			`["res_model","=","helpdesk.ticket"]],"fields":["id","name","summary","sequence",`+
			`"icon","res_model"],"order":"sequence asc, id asc"}`)
		fmt.Fprint(w, `[{"id":3,"name":"Call","summary":false,"sequence":10,"icon":false,`+
			`"res_model":false},{"id":4,"name":"Email","summary":"Send email","sequence":20,`+
			`"icon":"fa-envelope","res_model":"helpdesk.ticket"}]`)
	})

	got, err := client.ListTicketActivityTypes(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "Call", got[0].Name)
	assert.Empty(t, got[0].Summary)
	assert.Empty(t, got[0].Icon)
	assert.Empty(t, got[0].ResModel)
	assert.Equal(t, "helpdesk.ticket", got[1].ResModel)
}

func TestActivityInputValidationMakesNoCalls(t *testing.T) {
	var calls int

	client := ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) { calls++ })
	badDate := "2026-02-30"
	zero := 0
	tests := map[string]func() error{
		"schedule ticket": func() error {
			_, err := client.ScheduleTicketActivity(context.Background(), 0, ActivityRequest{})
			return err
		},
		"schedule type": func() error {
			_, err := client.ScheduleTicketActivity(context.Background(), 7, ActivityRequest{TypeID: -1})
			return err
		},
		"schedule user": func() error {
			_, err := client.ScheduleTicketActivity(context.Background(), 7, ActivityRequest{UserID: -1})
			return err
		},
		"schedule date": func() error {
			_, err := client.ScheduleTicketActivity(context.Background(), 7, ActivityRequest{DateDeadline: "11/15/2026"})
			return err
		},
		"update ticket": func() error {
			return client.UpdateTicketActivity(context.Background(), 0, 41, UpdateActivityRequest{DateDeadline: &badDate})
		},
		"update activity": func() error {
			return client.UpdateTicketActivity(context.Background(), 7, 0, UpdateActivityRequest{DateDeadline: &badDate})
		},
		"update empty": func() error {
			return client.UpdateTicketActivity(context.Background(), 7, 41, UpdateActivityRequest{})
		},
		"update type": func() error {
			return client.UpdateTicketActivity(context.Background(), 7, 41, UpdateActivityRequest{TypeID: &zero})
		},
		"update date": func() error {
			return client.UpdateTicketActivity(context.Background(), 7, 41, UpdateActivityRequest{DateDeadline: &badDate})
		},
		"complete ticket": func() error {
			return client.CompleteTicketActivity(context.Background(), 0, 41, "done")
		},
		"complete activity": func() error {
			return client.CompleteTicketActivity(context.Background(), 7, -1, "done")
		},
		"delete ticket": func() error { return client.DeleteTicketActivity(context.Background(), 0, 41) },
		"delete activity": func() error {
			return client.DeleteTicketActivity(context.Background(), 7, 0)
		},
	}

	for name, call := range tests {
		t.Run(name, func(t *testing.T) {
			assert.ErrorIs(t, call(), odoo.ErrInvalidArgument)
		})
	}
	assert.Zero(t, calls)
}

func TestActivityWritesDoNotRetry(t *testing.T) {
	var writes int

	client := ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/search_read") {
			fmt.Fprint(w, `[{"id":41}]`)
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

	err := client.CompleteTicketActivity(context.Background(), 7, 41, "done")
	assert.Error(t, err)
	assert.Equal(t, 1, writes)
}
