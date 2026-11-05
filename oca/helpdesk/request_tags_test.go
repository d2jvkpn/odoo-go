package helpdesk

import (
	jsonv2 "encoding/json/v2"
	"reflect"
	"strings"
	"testing"

	odoo "github.com/d2jvkpn/odoo-go"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Request bodies are encoded with encoding/json/v2, where omitempty judges the encoded value. These tests
// check every request struct field by field.

var requestStructs = []any{
	CreateTicketRequest{}, UpdateTicketRequest{}, PostTicketMessageRequest{},
	ActivityRequest{}, UpdateActivityRequest{},
	CreateTicketCategoryRequest{}, UpdateTicketCategoryRequest{},
	CreateTicketTagRequest{}, UpdateTicketTagRequest{},
}

func TestRequestTagsUseOmitzeroForScalarsAndPointers(t *testing.T) {
	for _, v := range requestStructs {
		typ := reflect.TypeOf(v)
		for i := range typ.NumField() {
			field := typ.Field(i)
			if !strings.Contains(field.Tag.Get("json"), "omitempty") {
				continue
			}
			switch field.Type.Kind() {
			case reflect.Int, reflect.Float64, reflect.Bool:
				t.Errorf("%s.%s: use omitzero for %s", typ.Name(), field.Name, field.Type)
			case reflect.Pointer:
				if field.Type.Elem() != reflect.TypeFor[odoo.ReplaceIDs]() {
					t.Errorf("%s.%s: use omitzero for %s", typ.Name(), field.Name, field.Type)
				}
			}
		}
	}
}

func encodeKeys(t *testing.T, v any) map[string]any {
	t.Helper()

	var got map[string]any

	data, err := jsonv2.Marshal(v)
	require.NoError(t, err)
	require.NoError(t, jsonv2.Unmarshal(data, &got))
	return got
}

func jsonName(field reflect.StructField) string {
	name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
	return name
}

func TestRequestZeroValuesOmitOptionalScalars(t *testing.T) {
	for _, v := range requestStructs {
		typ := reflect.TypeOf(v)
		got := encodeKeys(t, v)
		for i := range typ.NumField() {
			field := typ.Field(i)
			tag := field.Tag.Get("json")
			if !strings.Contains(tag, "omitzero") && !strings.Contains(tag, "omitempty") {
				continue
			}
			assert.NotContains(t, got, jsonName(field), "%s.%s", typ.Name(), field.Name)
		}
	}
}

func TestUpdateRequestPointersToZeroAreSent(t *testing.T) {
	for _, v := range []any{
		UpdateTicketRequest{}, UpdateActivityRequest{}, UpdateTicketCategoryRequest{}, UpdateTicketTagRequest{},
	} {
		typ := reflect.TypeOf(v)
		val := reflect.New(typ).Elem()
		for i := range typ.NumField() {
			field := typ.Field(i)
			require.Equal(t, reflect.Pointer, field.Type.Kind(), "%s.%s", typ.Name(), field.Name)
			val.Field(i).Set(reflect.New(field.Type.Elem()))
		}
		got := encodeKeys(t, val.Interface())
		for i := range typ.NumField() {
			assert.Contains(t, got, jsonName(typ.Field(i)), "%s.%s", typ.Name(), typ.Field(i).Name)
		}
	}
}
