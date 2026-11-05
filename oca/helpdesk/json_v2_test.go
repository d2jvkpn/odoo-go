package helpdesk

import (
	"context"
	jsonv2 "encoding/json/v2"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The custom decoders in this package use encoding/json/v2. These tests pin the behavior that differs from
// encoding/json, so a regression or a Go change shows up here.

func searchWith(t *testing.T, body string) ([]Ticket, error) {
	t.Helper()

	client := ticketTestClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) })
	return client.SearchTickets(context.Background(), TicketQuery{})
}

func TestTicketDecodesUnsetOptionalFields(t *testing.T) {
	tickets, err := searchWith(t, `[{"id":7,"name":"Pump","priority":false},{"id":8,"name":"Fan","priority":null}]`)
	require.NoError(t, err)
	require.Len(t, tickets, 2)
	assert.Empty(t, tickets[0].Priority)
	assert.Empty(t, tickets[1].Priority)
}

func TestTicketDecodingIsStricterThanEncodingJSON(t *testing.T) {
	for name, body := range map[string]string{
		"duplicate member": `[{"id":7,"name":"a","name":"b"}]`,
		"invalid UTF-8":    "[{\"id\":7,\"name\":\"a\xffb\"}]",
	} {
		_, err := searchWith(t, body)
		assert.Error(t, err, name)
	}
}

func TestTicketMemberNamesAreCaseSensitive(t *testing.T) {
	tickets, err := searchWith(t, `[{"id":7,"name":"quiet","NAME":"shout"}]`)
	require.NoError(t, err)
	require.Len(t, tickets, 1)
	assert.Equal(t, "quiet", tickets[0].Name)
}

func TestActivityAndAttachmentDecodeUnsetValues(t *testing.T) {
	var activity TicketActivity

	require.NoError(t, unmarshalForTest(`{"id":1,"summary":false,"note":null}`, &activity))
	assert.Empty(t, activity.Summary)
	assert.Empty(t, activity.Note)

	var data attachmentData
	require.NoError(t, unmarshalForTest(`false`, &data))
	assert.Nil(t, data)
	require.NoError(t, unmarshalForTest(`"aGk="`, &data))
	assert.Equal(t, "hi", string(data))
	assert.Error(t, unmarshalForTest(`"not base64!"`, &data))
	assert.Error(t, unmarshalForTest(`5`, &data))
}

func unmarshalForTest(text string, v any) error {
	return jsonv2.Unmarshal([]byte(text), v)
}
