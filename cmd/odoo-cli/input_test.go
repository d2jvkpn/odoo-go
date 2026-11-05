package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrintJSONIsIndentedSortedAndUnescaped(t *testing.T) {
	var out bytes.Buffer

	require.NoError(t, printJSON(&out, map[string]any{"b": "<a&b>", "a": 1}))
	assert.Equal(t, "{\n  \"a\": 1,\n  \"b\": \"<a&b>\"\n}\n", out.String())
}

func TestDecodeStrictRejectsUnknownMembersAndTrailingData(t *testing.T) {
	var v struct {
		Name string `json:"name"`
	}

	require.NoError(t, decodeStrict(strings.NewReader(`{"name":"x"}`+"\n"), &v))
	assert.Equal(t, "x", v.Name)
	assert.Error(t, decodeStrict(strings.NewReader(`{"name":"x","other":1}`), &v))
	assert.ErrorIs(t, decodeStrict(strings.NewReader(`{"name":"x"} {}`), &v), errNotOneObject)
	assert.Error(t, decodeStrict(strings.NewReader(`{"NAME":"x","name":"y"}`), &struct{}{}))
}

func TestParseDomain(t *testing.T) {
	domain, err := parseDomain(`[["id","=",7]]`)
	require.NoError(t, err)
	assert.Len(t, domain, 1)
	_, err = parseDomain(`null`)
	assert.Error(t, err)
}
