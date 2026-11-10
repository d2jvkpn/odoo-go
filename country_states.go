package odoo

import (
	"context"
	"fmt"
	"strings"
)

// CountryState is a res.country.state record: a province or state of a country. Code is unique
// within the country in Odoo.
type CountryState struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Code string `json:"code"`
}

// ListCountryStates returns the states of the country, ordered by code, in one request.
func (c *Client) ListCountryStates(ctx context.Context, countryID int) ([]CountryState, error) {
	var (
		err    error
		states []CountryState
	)

	if countryID <= 0 {
		return nil, invalidArgument("country id must be positive")
	}
	states = make([]CountryState, 0)
	err = c.CallRead(ctx, "res.country.state", "search_read", map[string]any{
		"domain": []any{[]any{"country_id", "=", countryID}},
		"fields": []string{"id", "name", "code"},
		"order":  "code",
	}, &states)
	if err != nil {
		return nil, fmt.Errorf("list country states: %w", err)
	}
	return states, nil
}

// CreateCountryState creates a state of the country and returns its ID. It does not retry. Odoo
// refuses a second state with the same code in the country; look the state up again before
// resubmitting after an error.
func (c *Client) CreateCountryState(
	ctx context.Context, countryID int, name, code string,
) (int, error) {
	var (
		err  error
		ids  []int
		vals map[string]any
	)

	if countryID <= 0 {
		return 0, invalidArgument("country id must be positive")
	}
	if strings.TrimSpace(name) == "" || strings.TrimSpace(code) == "" {
		return 0, invalidArgument("name and code must not be empty")
	}
	vals = map[string]any{"name": name, "code": code, "country_id": countryID}
	if err = c.Call(ctx, "res.country.state", "create", map[string]any{"vals_list": []any{vals}}, &ids); err != nil {
		return 0, fmt.Errorf("create country state: %w", err)
	}
	if len(ids) != 1 || ids[0] <= 0 {
		return 0, fmt.Errorf("%w: expected one positive ID from create", ErrUnexpectedResponse)
	}
	return ids[0], nil
}
