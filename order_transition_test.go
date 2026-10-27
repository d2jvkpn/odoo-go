package odoo

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func transitionHandler(t *testing.T, state, method, wantBody string, paths *[]string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		*paths = append(*paths, r.URL.Path)
		switch r.URL.Path {
		case "/json/2/sale.order/search_read":
			assertJSON(t, requestParams(t, r)["fields"], `["state"]`)
			if state == "" {
				fmt.Fprint(w, `[]`)
				return
			}
			fmt.Fprintf(w, `[{"state":%q}]`, state)
		case "/json/2/sale.order/" + method:
			assertJSON(t, requestParams(t, r), wantBody)
			fmt.Fprint(w, "true")
		}
	}
}

func TestConfirmSaleOrder(t *testing.T) {
	for _, state := range []string{"draft", "sent"} {
		var paths []string

		client := orderTestClient(t, transitionHandler(t, state, "action_confirm", `{"ids":[5]}`, &paths))
		require.NoError(t, client.ConfirmSaleOrder(context.Background(), 5), state)
		assert.Equal(t, []string{"/json/2/sale.order/search_read", "/json/2/sale.order/action_confirm"}, paths)
	}
}

func TestCancelSaleOrder(t *testing.T) {
	var paths []string

	body := `{"ids":[5],"context":{"disable_cancel_warning":true}}`
	client := orderTestClient(t, transitionHandler(t, "sale", "action_cancel", body, &paths))
	require.NoError(t, client.CancelSaleOrder(context.Background(), 5))
	assert.Equal(t, []string{"/json/2/sale.order/search_read", "/json/2/sale.order/action_cancel"}, paths)
}

func TestOrderTransitionsRejectBadStateWithoutWriting(t *testing.T) {
	for name, tc := range map[string]struct {
		state string
		call  func(*Client) error
		want  error
	}{
		"confirm sale":    {"sale", func(c *Client) error { return c.ConfirmSaleOrder(context.Background(), 5) }, ErrInvalidOrderState},
		"confirm cancel":  {"cancel", func(c *Client) error { return c.ConfirmSaleOrder(context.Background(), 5) }, ErrInvalidOrderState},
		"cancel cancel":   {"cancel", func(c *Client) error { return c.CancelSaleOrder(context.Background(), 5) }, ErrInvalidOrderState},
		"confirm missing": {"", func(c *Client) error { return c.ConfirmSaleOrder(context.Background(), 5) }, ErrSaleOrderNotFound},
		"cancel bad id":   {"draft", func(c *Client) error { return c.CancelSaleOrder(context.Background(), 0) }, ErrInvalidArgument},
	} {
		t.Run(name, func(t *testing.T) {
			var paths []string

			client := orderTestClient(t, transitionHandler(t, tc.state, "unused", `{}`, &paths))
			assert.ErrorIs(t, tc.call(client), tc.want)
			for _, p := range paths {
				assert.Equal(t, "/json/2/sale.order/search_read", p) // never a write
			}
		})
	}
}

func TestOrderTransitionsAreNotRetried(t *testing.T) {
	for name, call := range map[string]func(*Client) error{
		"confirm": func(c *Client) error { return c.ConfirmSaleOrder(context.Background(), 5) },
		"cancel":  func(c *Client) error { return c.CancelSaleOrder(context.Background(), 5) },
	} {
		t.Run(name, func(t *testing.T) {
			var reads, writes int

			client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/json/2/sale.order/search_read" {
					reads++
					fmt.Fprint(w, `[{"state":"draft"}]`)
					return
				}
				writes++
				http.Error(w, "unavailable", http.StatusServiceUnavailable)
			})
			client.Retry = RetryPolicy{MaxRetries: 3, Backoff: time.Millisecond}

			var httpErr *HTTPError
			require.ErrorAs(t, call(client), &httpErr)
			assert.Equal(t, 503, httpErr.StatusCode)
			assert.Equal(t, 1, reads)
			assert.Equal(t, 1, writes) // the write is never retried, despite MaxRetries
		})
	}
}

func TestOrderStateCheckFollowsRetry(t *testing.T) {
	var reads int

	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/json/2/sale.order/search_read":
			reads++
			if reads == 1 {
				http.Error(w, "unavailable", http.StatusServiceUnavailable)
				return
			}
			fmt.Fprint(w, `[{"state":"draft"}]`)
		default:
			fmt.Fprint(w, "true")
		}
	})
	client.Retry = RetryPolicy{MaxRetries: 2, Backoff: time.Millisecond}

	require.NoError(t, client.ConfirmSaleOrder(context.Background(), 5))
	assert.Equal(t, 2, reads) // the read-only pre-check retried once
}
