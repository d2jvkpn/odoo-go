package odoo

import (
	"context"
	jsonv2 "encoding/json/v2"
	"fmt"
	"slices"
)

// SyncPartner is a res.partner record with its active flag, as odoo-sync reads it.
type SyncPartner struct {
	Partner
	Active bool
}

// UnmarshalJSON decodes the partner fields and the active flag.
func (p *SyncPartner) UnmarshalJSON(data []byte) error {
	var aux struct {
		Active bool `json:"active"`
	}

	if err := jsonv2.Unmarshal(data, &p.Partner); err != nil {
		return err
	}
	if err := jsonv2.Unmarshal(data, &aux); err != nil {
		return err
	}
	p.Active = aux.Active
	return nil
}

// syncPartnerPage is the page size of ListSyncPartners; tests lower it.
var syncPartnerPage = 500

// ListSyncPartners returns every partner, archived ones included, ordered by id. It reads page by
// page and may be retried like any read.
func (c *Client) ListSyncPartners(ctx context.Context) ([]SyncPartner, error) {
	var (
		fields = append(slices.Clone(partnerFields), "active")
		out    = make([]SyncPartner, 0)
		page   []SyncPartner
		err    error
	)

	for offset := 0; ; offset += syncPartnerPage {
		page = nil
		err = c.CallRead(ctx, "res.partner", "search_read", map[string]any{
			"domain": []any{[]any{"active", "in", []any{true, false}}},
			"fields": fields,
			"limit":  syncPartnerPage,
			"offset": offset,
			"order":  "id",
		}, &page)
		if err != nil {
			return nil, fmt.Errorf("list partners: %w", err)
		}
		out = append(out, page...)
		if len(page) < syncPartnerPage {
			return out, nil
		}
	}
}

// StateInfo is a res.country.state record with the country it belongs to.
type StateInfo struct {
	ID      int      `json:"id"`
	Name    string   `json:"name"`
	Code    string   `json:"code"`
	Country Many2One `json:"country_id"`
}

// ListStatesByIDs returns the states with the given ids, ordered by id, in one request. Unknown ids
// are left out of the result.
func (c *Client) ListStatesByIDs(ctx context.Context, ids []int) ([]StateInfo, error) {
	var (
		states = make([]StateInfo, 0)
		err    error
	)

	for _, id := range ids {
		if id <= 0 {
			return nil, invalidArgument("state ids must be positive")
		}
	}
	if len(ids) == 0 {
		return states, nil
	}
	err = c.CallRead(ctx, "res.country.state", "search_read", map[string]any{
		"domain": []any{[]any{"id", "in", ids}},
		"fields": []string{"id", "name", "code", "country_id"},
		"order":  "id",
	}, &states)
	if err != nil {
		return nil, fmt.Errorf("list states: %w", err)
	}
	return states, nil
}

// ListActiveLanguages returns the codes of the languages installed (active) in the database, such
// as "en_US", ordered by code. A partner's lang must be one of them. It may be retried like any
// read.
func (c *Client) ListActiveLanguages(ctx context.Context) ([]string, error) {
	var (
		rows []struct {
			Code string `json:"code"`
		}
		out = make([]string, 0)
	)

	err := c.CallRead(ctx, "res.lang", "search_read", map[string]any{
		"domain": []any{[]any{"active", "=", true}},
		"fields": []string{"code"},
		"order":  "code",
	}, &rows)
	if err != nil {
		return nil, fmt.Errorf("list languages: %w", err)
	}
	for _, r := range rows {
		out = append(out, r.Code)
	}
	return out, nil
}
