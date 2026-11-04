package helpdesk

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"testing"
	"time"

	odoo "github.com/d2jvkpn/odoo-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUploadAndDownloadTicketAttachment(t *testing.T) {
	var (
		client  *Client
		content = []byte{0, 1, 2, 250, 255}
	)

	client = ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/json/2/helpdesk.ticket/search_read":
			fmt.Fprint(w, `[{"id":7}]`)
		case "/json/2/ir.attachment/create":
			assertJSON(t, requestParams(t, r), `{"vals_list":[{"name":"trace.bin",`+
				`"mimetype":"application/octet-stream","datas":"AAEC+v8=","type":"binary",`+
				`"res_model":"helpdesk.ticket","res_id":7}]}`)
			fmt.Fprint(w, `[51]`)
		case "/json/2/ir.attachment/search_read":
			assertJSON(t, requestParams(t, r), `{"domain":[["id","=",51],["res_model","=",`+
				`"helpdesk.ticket"],["res_id","=",7]],"fields":["id","datas"],"limit":1,`+
				`"context":{"bin_size":false}}`)
			fmt.Fprintf(w, `[{"id":51,"datas":%q}]`, base64.StdEncoding.EncodeToString(content))
		}
	})

	id, err := client.UploadTicketAttachment(
		context.Background(), 7, "trace.bin", "application/octet-stream", content,
	)
	require.NoError(t, err)
	assert.Equal(t, 51, id)

	got, err := client.DownloadTicketAttachment(context.Background(), 7, 51)
	require.NoError(t, err)
	assert.Equal(t, content, got)
}

func TestUploadTicketAttachmentAllowsEmptyData(t *testing.T) {
	var client *Client

	client = ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/json/2/helpdesk.ticket/search_read":
			fmt.Fprint(w, `[{"id":7}]`)
		case "/json/2/ir.attachment/create":
			assertJSON(t, requestParams(t, r), `{"vals_list":[{"name":"empty.txt",`+
				`"mimetype":"text/plain","datas":"","type":"binary",`+
				`"res_model":"helpdesk.ticket","res_id":7}]}`)
			fmt.Fprint(w, `[52]`)
		}
	})

	got, err := client.UploadTicketAttachment(context.Background(), 7, "empty.txt", "text/plain", nil)
	require.NoError(t, err)
	assert.Equal(t, 52, got)
}

func TestListTicketAttachments(t *testing.T) {
	var client *Client

	client = ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/json/2/helpdesk.ticket/search_read":
			fmt.Fprint(w, `[{"id":7}]`)
		case "/json/2/ir.attachment/search_read":
			assertJSON(t, requestParams(t, r), `{"domain":[["res_model","=","helpdesk.ticket"],`+
				`["res_id","=",7]],"fields":["id","name","mimetype","file_size","checksum",`+
				`"create_date","write_date"],"order":"id asc"}`)
			fmt.Fprint(w, `[{"id":51,"name":"trace.bin","mimetype":"application/octet-stream",`+
				`"file_size":5,"checksum":"f00","create_date":"2026-11-03 03:01:00",`+
				`"write_date":"2026-11-03 03:01:00"}]`)
		}
	})

	got, err := client.ListTicketAttachments(context.Background(), 7)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, 51, got[0].ID)
	assert.Equal(t, "trace.bin", got[0].Name)
	assert.Equal(t, "application/octet-stream", string(got[0].MimeType))
	assert.Equal(t, 5, got[0].Size)
	assert.Equal(t, "f00", string(got[0].Checksum))
}

func TestDownloadTicketAttachmentFalseDataIsEmpty(t *testing.T) {
	var client *Client

	client = ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/json/2/helpdesk.ticket/search_read":
			fmt.Fprint(w, `[{"id":7}]`)
		case "/json/2/ir.attachment/search_read":
			fmt.Fprint(w, `[{"id":51,"datas":false}]`)
		}
	})

	got, err := client.DownloadTicketAttachment(context.Background(), 7, 51)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestDownloadTicketAttachmentRejectsMalformedBase64(t *testing.T) {
	var client *Client

	client = ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/json/2/helpdesk.ticket/search_read":
			fmt.Fprint(w, `[{"id":7}]`)
		case "/json/2/ir.attachment/search_read":
			fmt.Fprint(w, `[{"id":51,"datas":"%%%"}]`)
		}
	})

	_, err := client.DownloadTicketAttachment(context.Background(), 7, 51)
	assert.ErrorIs(t, err, odoo.ErrUnexpectedResponse)
}

func TestDeleteTicketAttachment(t *testing.T) {
	var (
		client  *Client
		deleted map[string]any
	)

	client = ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/json/2/helpdesk.ticket/search_read":
			fmt.Fprint(w, `[{"id":7}]`)
		case "/json/2/ir.attachment/search_read":
			assertJSON(t, requestParams(t, r), `{"domain":[["id","=",51],["res_model","=",`+
				`"helpdesk.ticket"],["res_id","=",7]],"fields":["id"],"limit":1}`)
			fmt.Fprint(w, `[{"id":51}]`)
		case "/json/2/ir.attachment/unlink":
			deleted = requestParams(t, r)
			fmt.Fprint(w, `true`)
		}
	})

	require.NoError(t, client.DeleteTicketAttachment(context.Background(), 7, 51))
	assertJSON(t, deleted, `{"ids":[51]}`)
}

func TestDeleteTicketAttachmentRejectsFalseResult(t *testing.T) {
	var client *Client

	client = ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/json/2/helpdesk.ticket/search_read":
			fmt.Fprint(w, `[{"id":7}]`)
		case "/json/2/ir.attachment/search_read":
			fmt.Fprint(w, `[{"id":51}]`)
		case "/json/2/ir.attachment/unlink":
			fmt.Fprint(w, `false`)
		}
	})

	err := client.DeleteTicketAttachment(context.Background(), 7, 51)
	assert.ErrorIs(t, err, odoo.ErrUnexpectedResponse)
}

func TestTicketAttachmentChecksOwnership(t *testing.T) {
	for name, operation := range map[string]func(context.Context, *Client) error{
		"download": func(ctx context.Context, client *Client) error {
			_, err := client.DownloadTicketAttachment(ctx, 7, 51)
			return err
		},
		"delete": func(ctx context.Context, client *Client) error {
			return client.DeleteTicketAttachment(ctx, 7, 51)
		},
	} {
		t.Run(name, func(t *testing.T) {
			var (
				writes int
				client *Client
			)

			client = ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/json/2/helpdesk.ticket/search_read":
					fmt.Fprint(w, `[{"id":7}]`)
				case "/json/2/ir.attachment/search_read":
					fmt.Fprint(w, `[]`)
				case "/json/2/ir.attachment/unlink":
					writes++
				}
			})

			err := operation(context.Background(), client)
			require.Error(t, err)
			assert.Zero(t, writes)
		})
	}
}

func TestTicketAttachmentRejectsBadInput(t *testing.T) {
	var (
		calls  int
		client *Client
	)

	client = ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) { calls++ })
	for name, operation := range map[string]func() error{
		"upload ticket": func() error {
			_, err := client.UploadTicketAttachment(context.Background(), 0, "x", "text/plain", []byte("x"))
			return err
		},
		"upload name": func() error {
			_, err := client.UploadTicketAttachment(context.Background(), 7, " ", "text/plain", []byte("x"))
			return err
		},
		"upload mimetype": func() error {
			_, err := client.UploadTicketAttachment(context.Background(), 7, "x", " ", []byte("x"))
			return err
		},
		"list ticket": func() error {
			_, err := client.ListTicketAttachments(context.Background(), 0)
			return err
		},
		"download attachment": func() error {
			_, err := client.DownloadTicketAttachment(context.Background(), 7, 0)
			return err
		},
		"delete attachment": func() error {
			return client.DeleteTicketAttachment(context.Background(), 7, -1)
		},
	} {
		err := operation()
		assert.ErrorIs(t, err, odoo.ErrInvalidArgument, name)
	}
	assert.Zero(t, calls)
}

func TestListTicketAttachmentsRetriesRead(t *testing.T) {
	var (
		attempts int
		client   *Client
	)

	client = ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/json/2/helpdesk.ticket/search_read":
			fmt.Fprint(w, `[{"id":7}]`)
		case "/json/2/ir.attachment/search_read":
			attempts++
			if attempts == 1 {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			fmt.Fprint(w, `[]`)
		}
	})
	client.Retry = odoo.RetryPolicy{MaxRetries: 1, Backoff: time.Millisecond, NoJitter: true}

	_, err := client.ListTicketAttachments(context.Background(), 7)
	require.NoError(t, err)
	assert.Equal(t, 2, attempts)
}

func TestUploadTicketAttachmentDoesNotRetryWrite(t *testing.T) {
	var (
		creates int
		client  *Client
	)

	client = ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/json/2/helpdesk.ticket/search_read":
			fmt.Fprint(w, `[{"id":7}]`)
		case "/json/2/ir.attachment/create":
			creates++
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	})
	client.Retry = odoo.RetryPolicy{MaxRetries: 3, Backoff: time.Millisecond, NoJitter: true}

	_, err := client.UploadTicketAttachment(context.Background(), 7, "x", "text/plain", []byte("x"))
	require.Error(t, err)
	assert.Equal(t, 1, creates)
}

func TestUploadTicketAttachmentRejectsOversizedData(t *testing.T) {
	client := ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("no request expected")
	})

	_, err := client.UploadTicketAttachment(context.Background(), 1, "a.bin", "application/octet-stream",
		make([]byte, MaxAttachmentSize+1))
	assert.ErrorIs(t, err, odoo.ErrInvalidArgument)
}
