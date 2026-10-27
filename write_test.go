package odoo

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// failTransport counts requests and always fails, to prove how often a call is attempted.
type failTransport struct{ calls int }

func (f *failTransport) RoundTrip(*http.Request) (*http.Response, error) {
	f.calls++
	return nil, errors.New("boom")
}

func TestPatchValues(t *testing.T) {
	name, zero, off := "Tea", 0.0, false
	vals, err := patchValues(struct {
		Name  *string  `json:"name,omitempty"`
		Price *float64 `json:"list_price,omitempty"`
		Sale  *bool    `json:"sale_ok,omitempty"`
		Skip  *int     `json:"categ_id,omitempty"`
	}{Name: &name, Price: &zero, Sale: &off})
	require.NoError(t, err)
	assertJSON(t, vals, `{"name":"Tea","list_price":0,"sale_ok":false}`)

	_, err = patchValues(struct {
		Name *string `json:"name,omitempty"`
	}{})
	assert.ErrorIs(t, err, ErrInvalidArgument)
}

// Odoo clears a many2one with false; an integer 0 corrupts parent paths and display names.
func TestPatchValuesClearsZeroRelationWithFalse(t *testing.T) {
	zero, id := 0, 3
	vals, err := patchValues(struct {
		Parent  *int `json:"parent_id,omitempty"`
		Country *int `json:"country_id,omitempty"`
		Count   *int `json:"sequence,omitempty"`
	}{Parent: &zero, Country: &id, Count: &zero})
	require.NoError(t, err)
	assertJSON(t, vals, `{"parent_id":false,"country_id":3,"sequence":0}`)
}

func TestUpdateRecord(t *testing.T) {
	var paths []string

	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/json/2/res.partner/search_read":
			params := requestParams(t, r)
			assert.Equal(t, map[string]any{"active_test": false}, params["context"])
			fmt.Fprint(w, `[{"id":7}]`)
		case "/json/2/res.partner/write":
			assertJSON(t, requestParams(t, r), `{"ids":[7],"vals":{"name":"B"}}`)
			fmt.Fprint(w, "true")
		}
	})
	name := "B"
	err := client.updateRecord(context.Background(), "res.partner", 7,
		struct {
			Name *string `json:"name,omitempty"`
		}{&name}, ErrPartnerNotFound)
	require.NoError(t, err)
	assert.Equal(t, []string{"/json/2/res.partner/search_read", "/json/2/res.partner/write"}, paths)
}

func TestUpdateRecordRejectsWithoutWriting(t *testing.T) {
	var writes int

	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/json/2/res.partner/write" {
			writes++
		}
		fmt.Fprint(w, `[]`) // search_read finds nothing
	})
	name := "B"
	patch := struct {
		Name *string `json:"name,omitempty"`
	}{&name}

	err := client.updateRecord(context.Background(), "res.partner", 7, patch, ErrPartnerNotFound)
	assert.ErrorIs(t, err, ErrPartnerNotFound)
	err = client.updateRecord(context.Background(), "res.partner", 0, patch, ErrPartnerNotFound)
	assert.ErrorIs(t, err, ErrInvalidArgument)
	err = client.updateRecord(context.Background(), "res.partner", 7, struct {
		Name *string `json:"name,omitempty"`
	}{}, ErrPartnerNotFound)
	assert.ErrorIs(t, err, ErrInvalidArgument)
	assert.Zero(t, writes)
}

func TestWriteRecordIsNotRetried(t *testing.T) {
	transport := &failTransport{}
	client := NewClient("http://odoo.invalid", "k", "")
	client.HTTP = &http.Client{Transport: transport}
	client.Retry = RetryPolicy{MaxRetries: 3}

	err := client.writeRecord(context.Background(), "res.partner", 7, map[string]any{"name": "B"})
	require.Error(t, err)
	assert.Equal(t, 1, transport.calls)
}
