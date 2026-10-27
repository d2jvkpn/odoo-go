package odoo

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCallReturnsHTTPError(t *testing.T) {
	var (
		client  *Client
		err     error
		httpErr *HTTPError
	)

	client = orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, "Access Denied")
	})
	err = client.Call(context.Background(), "res.partner", "search", nil, nil)
	require.ErrorAs(t, err, &httpErr)
	assert.Equal(t, http.StatusUnauthorized, httpErr.StatusCode)
	assert.Equal(t, "Access Denied", httpErr.Body)
	assert.EqualError(t, err, "HTTP 401: Access Denied")
}

func TestInvalidArgumentsWrapSentinel(t *testing.T) {
	var (
		client *Client
		err    error
	)

	client = NewClient("http://127.0.0.1:1", "key", "")
	_, err = client.GetProduct(context.Background(), 0)
	assert.ErrorIs(t, err, ErrInvalidArgument)
	_, err = client.SearchSaleOrders(context.Background(), SaleOrderQuery{Limit: -1})
	assert.ErrorIs(t, err, ErrInvalidArgument)
	_, err = client.CreateSaleOrder(context.Background(), CreateSaleOrderRequest{})
	assert.ErrorIs(t, err, ErrInvalidArgument)
	err = client.Call(context.Background(), "m", "n", []int{1}, nil)
	assert.ErrorIs(t, err, ErrInvalidArgument)
}

func TestInvalidConfigWrapsSentinel(t *testing.T) {
	var (
		path string
		err  error
	)

	path = filepath.Join(t.TempDir(), "local.yaml")
	require.NoError(t, os.WriteFile(path, []byte("odoo:\n  base_url: invalid\n  api_key: secret\n"), 0o600))
	_, err = LoadClient(path)
	assert.ErrorIs(t, err, ErrInvalidConfig)
	assert.NotContains(t, err.Error(), "secret")
}

func TestUnexpectedResponseWrapsSentinel(t *testing.T) {
	var (
		client *Client
		err    error
	)

	client = orderTestClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "[1, 2]") })
	_, err = client.CreateSaleOrder(context.Background(), CreateSaleOrderRequest{
		PartnerID: 1, Lines: []CreateSaleOrderLine{{ProductID: 1, Quantity: 1}},
	})
	assert.ErrorIs(t, err, ErrUnexpectedResponse)
}
