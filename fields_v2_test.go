package odoo

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Many2One, OptionalString and ReplaceIDs decode their payload with encoding/json/v2 inside UnmarshalJSON.

func TestRelationDecodesUnsetValues(t *testing.T) {
	var rel struct {
		A Many2One       `json:"a"`
		B Many2One       `json:"b"`
		C Many2One       `json:"c"`
		S OptionalString `json:"s"`
	}

	require.NoError(t, json.Unmarshal([]byte(`{"a":[7,"Alice"],"b":false,"c":[8,false],"s":null}`), &rel))
	assert.Equal(t, Many2One{ID: 7, Name: "Alice"}, rel.A)
	assert.Equal(t, Many2One{}, rel.B)
	assert.Equal(t, Many2One{ID: 8}, rel.C)
	assert.Empty(t, rel.S)
}

func TestRelationRejectsInvalidUTF8(t *testing.T) {
	var rel Many2One

	assert.Error(t, rel.UnmarshalJSON([]byte("[7,\"a\xffb\"]")))
	assert.Error(t, rel.UnmarshalJSON([]byte(`[0,"x"]`)))
	assert.Error(t, rel.UnmarshalJSON([]byte(`[7,3]`)))
}

func TestReplaceIDsRoundTrip(t *testing.T) {
	var ids ReplaceIDs

	require.NoError(t, json.Unmarshal([]byte(`[1,2]`), &ids))
	assert.Equal(t, ReplaceIDs{1, 2}, ids)
	out, err := json.Marshal(ids)
	require.NoError(t, err)
	assert.JSONEq(t, `[[6,0,[1,2]]]`, string(out))
}

// Request bodies are encoded with encoding/json/v2, whose omitempty keeps zero numbers and empty strings
// behind pointers; the request structs use omitzero for those fields.
func TestRequestOmitsUnsetFields(t *testing.T) {
	var got map[string]any

	vals, err := patchValues(CreatePartnerRequest{Name: "A", CategoryIDs: ReplaceIDs{}})
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"name": "A"}, vals)

	empty := ""
	vals, err = patchValues(UpdateSaleOrderRequest{Note: &empty})
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"note": ""}, vals)

	data, err := json.Marshal(CreateProductRequest{Name: "P"})
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &got))
	assert.NotContains(t, got, "categ_id")
}
