package odoo

import (
	"context"
	"fmt"
)

// Country is a res.country record. Code is the ISO 3166-1 alpha-2 code, such as "NO"; ID is the
// database ID that Partner.Country and CreatePartnerRequest.CountryID refer to. IDs differ between
// Odoo databases, so look them up by Code instead of hard-coding them.
type Country struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Code string `json:"code"`
}

// ListCountries returns every country, ordered by name, in one request.
func (c *Client) ListCountries(ctx context.Context) ([]Country, error) {
	var (
		err       error
		countries []Country
	)

	countries = make([]Country, 0)
	err = c.CallRead(ctx, "res.country", "search_read", map[string]any{
		"domain": []any{},
		"fields": []string{"id", "name", "code"},
		"order":  "name",
	}, &countries)
	if err != nil {
		return nil, fmt.Errorf("list countries: %w", err)
	}
	return countries, nil
}
