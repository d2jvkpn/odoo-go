package odoo

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Many2One decodes Odoo's [id, display_name] relation representation.
// An unset relation (false or null) has ID zero. JSON output is an object: Odoo's [7, "Alice"]
// becomes {"id":7,"name":"Alice"}.
type Many2One struct {
	ID   int    `json:"id"`   // record ID; zero when the relation is unset
	Name string `json:"name"` // display name of the related record
}

// UnmarshalJSON decodes [id, display_name], or false/null into the zero value.
func (m *Many2One) UnmarshalJSON(data []byte) error {
	var (
		parts []json.RawMessage
		value Many2One
	)

	data = bytes.TrimSpace(data)
	if bytes.Equal(data, []byte("false")) || bytes.Equal(data, []byte("null")) {
		*m = Many2One{}
		return nil
	}
	if err := json.Unmarshal(data, &parts); err != nil {
		return fmt.Errorf("decode relation: %w", err)
	}
	if len(parts) != 2 {
		return fmt.Errorf("%w: relation must contain an ID and a display name", ErrUnexpectedResponse)
	}
	if err := json.Unmarshal(parts[0], &value.ID); err != nil {
		return fmt.Errorf("decode relation ID: %w", err)
	}
	if value.ID <= 0 {
		return fmt.Errorf("%w: relation ID must be positive", ErrUnexpectedResponse)
	}
	if len(parts[1]) == 0 || parts[1][0] != '"' {
		return fmt.Errorf("%w: relation display name must be a string", ErrUnexpectedResponse)
	}
	if err := json.Unmarshal(parts[1], &value.Name); err != nil {
		return fmt.Errorf("decode relation name: %w", err)
	}
	*m = value
	return nil
}

// OptionalString normalizes Odoo's false/null optional text fields to "".
type OptionalString string

// UnmarshalJSON decodes a string, or false/null into the empty string.
func (s *OptionalString) UnmarshalJSON(data []byte) error {
	var value string

	data = bytes.TrimSpace(data)
	if bytes.Equal(data, []byte("false")) || bytes.Equal(data, []byte("null")) {
		*s = ""
		return nil
	}
	if err := json.Unmarshal(data, &value); err != nil {
		return fmt.Errorf("decode optional string: %w", err)
	}
	*s = OptionalString(value)
	return nil
}
