package odoo

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Partner is a res.partner record. Optional text fields, including the name of an unnamed address,
// are empty when unset in Odoo.
type Partner struct {
	ID      int            `json:"id"`
	Name    string         `json:"name"`
	Email   OptionalString `json:"email"`
	Phone   OptionalString `json:"phone"`
	Ref     OptionalString `json:"ref"` // the caller's own identifier, e.g. "user-42"; not unique in Odoo
	Street  OptionalString `json:"street"`
	City    OptionalString `json:"city"`
	Zip     OptionalString `json:"zip"`
	Country Many2One       `json:"country_id"` // res.country; zero ID when unset
}

// UnmarshalJSON decodes a partner, mapping the unset name Odoo returns as false (typical of
// invoice and delivery addresses) to the empty string.
func (p *Partner) UnmarshalJSON(data []byte) error {
	type plain Partner
	var aux struct {
		plain
		Name OptionalString `json:"name"`
	}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	*p = Partner(aux.plain)
	p.Name = string(aux.Name)
	return nil
}

// PartnerQuery filters and paginates partners. Limit defaults to 100 and Order to "id". Archived
// partners are not returned.
type PartnerQuery struct {
	Domain []any  // Odoo domain, for example []any{[]any{"ref", "=", "u-42"}}; nil matches all
	Limit  int    // zero means 100
	Offset int    // records to skip
	Order  string // Odoo order clause; empty means "id"
}

// SearchPartners returns res.partner records matching the query.
func (c *Client) SearchPartners(ctx context.Context, query PartnerQuery) ([]Partner, error) {
	var (
		err      error
		partners []Partner
	)

	if query.Limit < 0 || query.Offset < 0 {
		return nil, invalidArgument("limit and offset must not be negative")
	}
	if query.Limit == 0 {
		query.Limit = 100
	}
	if query.Order == "" {
		query.Order = "id"
	}
	if query.Domain == nil {
		query.Domain = []any{}
	}
	partners = make([]Partner, 0)
	err = c.CallRead(ctx, "res.partner", "search_read", map[string]any{
		"domain": query.Domain,
		"fields": []string{
			"id", "name", "email", "phone", "ref", "street", "city", "zip", "country_id",
		},
		"limit":  query.Limit,
		"offset": query.Offset,
		"order":  query.Order,
	}, &partners)
	if err != nil {
		return nil, fmt.Errorf("search partners: %w", err)
	}
	return partners, nil
}

// GetPartner retrieves one partner by ID, or ErrPartnerNotFound.
func (c *Client) GetPartner(ctx context.Context, id int) (*Partner, error) {
	var (
		err      error
		partners []Partner
	)

	if id <= 0 {
		return nil, invalidArgument("partner ID must be positive")
	}
	partners, err = c.SearchPartners(ctx, PartnerQuery{
		Domain: []any{[]any{"id", "=", id}},
		Limit:  1,
	})
	if err != nil {
		return nil, err
	}
	if len(partners) == 0 {
		return nil, fmt.Errorf("%w: %d", ErrPartnerNotFound, id)
	}
	return &partners[0], nil
}

// CreatePartnerRequest describes a new customer. Ref is the caller's own identifier (for example the
// e-commerce user ID) and is not unique in Odoo. Empty optional fields are omitted.
type CreatePartnerRequest struct {
	Name      string `json:"name"`                 // required, not blank
	Email     string `json:"email,omitempty"`      // optional
	Phone     string `json:"phone,omitempty"`      // optional
	Ref       string `json:"ref,omitempty"`        // optional caller identifier, e.g. "user-42"
	Street    string `json:"street,omitempty"`     // optional
	City      string `json:"city,omitempty"`       // optional
	Zip       string `json:"zip,omitempty"`        // optional
	CountryID int    `json:"country_id,omitempty"` // optional res.country ID; >= 0
}

func validateCreatePartner(input CreatePartnerRequest) error {
	if strings.TrimSpace(input.Name) == "" {
		return invalidArgument("name must not be empty")
	}
	if input.CountryID < 0 {
		return invalidArgument("country_id must not be negative")
	}
	return nil
}

// CreatePartner creates one partner and returns its ID. It does not retry. After a transport/decode
// error the outcome may be unknown; search by ref before resubmitting, or use UpsertPartnerByRef.
func (c *Client) CreatePartner(ctx context.Context, input CreatePartnerRequest) (int, error) {
	var (
		err error
		ids []int
	)

	if err = validateCreatePartner(input); err != nil {
		return 0, err
	}
	err = c.Call(ctx, "res.partner", "create", map[string]any{"vals_list": []any{input}}, &ids)
	if err != nil {
		return 0, fmt.Errorf("create partner: %w", err)
	}
	if len(ids) != 1 || ids[0] <= 0 {
		return 0, fmt.Errorf(
			"%w: expected one positive ID from create (partner may already exist)", ErrUnexpectedResponse,
		)
	}
	return ids[0], nil
}

// UpdatePartnerRequest changes selected fields of a partner. Nil fields are left unchanged.
// Name, if set, must not be blank; a pointer to "" clears the other text fields.
type UpdatePartnerRequest struct {
	Name      *string `json:"name,omitempty"`
	Email     *string `json:"email,omitempty"`
	Phone     *string `json:"phone,omitempty"`
	Ref       *string `json:"ref,omitempty"`
	Street    *string `json:"street,omitempty"`
	City      *string `json:"city,omitempty"`
	Zip       *string `json:"zip,omitempty"`
	CountryID *int    `json:"country_id,omitempty"` // res.country ID; must not be negative
}

// UpdatePartner changes one partner. It returns ErrPartnerNotFound for an unknown ID and does not retry.
func (c *Client) UpdatePartner(ctx context.Context, id int, patch UpdatePartnerRequest) error {
	if patch.Name != nil && strings.TrimSpace(*patch.Name) == "" {
		return invalidArgument("name must not be empty")
	}
	if patch.CountryID != nil && *patch.CountryID < 0 {
		return invalidArgument("country_id must not be negative")
	}
	return c.updateRecord(ctx, "res.partner", id, patch, ErrPartnerNotFound)
}

// UpsertPartnerByRef finds the active partner whose ref equals ref and updates it with the non-empty
// fields of input, or creates one when none exists. It returns the partner ID and whether it was created.
// Two matches return ErrAmbiguousPartner and nothing is written. The search is a retryable read; the
// create or write is not retried.
//
// WARNING: this is not safe under concurrency. The search and the create are separate requests and
// Odoo does not enforce a unique ref, so concurrent upserts for the same new ref all miss and all create.
// Measured against Odoo 19: 8 parallel upserts of one ref created 8 partners, after which every upsert of
// that ref returns ErrAmbiguousPartner until the duplicates are merged or removed in Odoo. Callers must
// serialize upserts per ref (an in-process mutex or a distributed lock keyed by ref) or add a unique
// constraint on res.partner.ref in Odoo. Different refs are independent and may run in parallel.
func (c *Client) UpsertPartnerByRef(
	ctx context.Context,
	ref string,
	input CreatePartnerRequest,
) (int, bool, error) {
	var (
		err    error
		found  []Partner
		id     int
		patch  UpdatePartnerRequest
		values map[string]any
	)

	ref = strings.TrimSpace(ref)
	if ref == "" {
		return 0, false, invalidArgument("ref must not be empty")
	}
	input.Ref = ref
	if err = validateCreatePartner(input); err != nil {
		return 0, false, err
	}
	found, err = c.SearchPartners(ctx, PartnerQuery{
		Domain: []any{[]any{"ref", "=", ref}},
		Limit:  2,
	})
	if err != nil {
		return 0, false, err
	}
	switch len(found) {
	case 0:
		id, err = c.CreatePartner(ctx, input)
		return id, true, err
	case 1:
		patch = UpdatePartnerRequest{Name: &input.Name, Ref: &input.Ref}
		for field, text := range map[*(*string)]string{
			&patch.Email: input.Email, &patch.Phone: input.Phone, &patch.Street: input.Street,
			&patch.City: input.City, &patch.Zip: input.Zip,
		} {
			if text != "" {
				t := text
				*field = &t
			}
		}
		if input.CountryID != 0 {
			patch.CountryID = &input.CountryID
		}
		if values, err = patchValues(patch); err != nil {
			return 0, false, err
		}
		return found[0].ID, false, c.writeRecord(ctx, "res.partner", found[0].ID, values)
	default:
		return 0, false, fmt.Errorf("%w: %q", ErrAmbiguousPartner, ref)
	}
}
