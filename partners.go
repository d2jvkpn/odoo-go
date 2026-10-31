package odoo

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Partner is a res.partner record with the fields of the Odoo base module. Optional text fields,
// including the name of an unnamed address, are empty when unset in Odoo; unset relations have a
// zero ID.
type Partner struct {
	ID              int            `json:"id"`
	Name            string         `json:"name"`
	Email           OptionalString `json:"email"`
	Phone           OptionalString `json:"phone"`
	Ref             OptionalString `json:"ref"` // the caller's own identifier, e.g. "user-42"; not unique in Odoo
	Street          OptionalString `json:"street"`
	Street2         OptionalString `json:"street2"`
	City            OptionalString `json:"city"`
	Zip             OptionalString `json:"zip"`
	State           Many2One       `json:"state_id"`   // res.country.state
	Country         Many2One       `json:"country_id"` // res.country
	Website         OptionalString `json:"website"`
	IsCompany       bool           `json:"is_company"`
	Type            string         `json:"type"`      // contact, invoice, delivery or other
	Parent          Many2One       `json:"parent_id"` // res.partner the contact belongs to
	Function        OptionalString `json:"function"`  // job position
	VAT             OptionalString `json:"vat"`       // tax ID
	CompanyRegistry OptionalString `json:"company_registry"`
	Lang            OptionalString `json:"lang"`        // language code, e.g. "en_US"
	Comment         OptionalString `json:"comment"`     // internal notes, HTML
	CategoryIDs     []int          `json:"category_id"` // res.partner.category tags; empty when none
	User            Many2One       `json:"user_id"`     // res.users salesperson
	Company         Many2One       `json:"company_id"`  // res.company
	Barcode         OptionalString `json:"barcode"`
	Industry        Many2One       `json:"industry_id"` // res.partner.industry
	CreateDate      OptionalString `json:"create_date"` // "YYYY-MM-DD HH:MM:SS" in UTC
	WriteDate       OptionalString `json:"write_date"`  // last change, "YYYY-MM-DD HH:MM:SS" in UTC
}

// partnerFields are the res.partner fields requested by SearchPartners.
var partnerFields = []string{
	"id", "name", "email", "phone", "ref", "street", "street2", "city", "zip", "state_id", "country_id",
	"website", "is_company", "type", "parent_id", "function", "vat", "company_registry", "lang",
	"comment", "category_id", "user_id", "company_id", "barcode", "industry_id", "create_date",
	"write_date",
}

// ReplaceIDs is a many2many value that replaces all linked records. It is sent to Odoo as the
// command [[6, 0, ids]]; in a JSON file it is a plain list of IDs.
type ReplaceIDs []int

// MarshalJSON encodes the IDs as the Odoo "replace all" command.
func (r ReplaceIDs) MarshalJSON() ([]byte, error) {
	return json.Marshal([]any{[]any{6, 0, append([]int{}, r...)}})
}

// UnmarshalJSON decodes a plain list of IDs.
func (r *ReplaceIDs) UnmarshalJSON(data []byte) error {
	var ids []int

	if err := json.Unmarshal(data, &ids); err != nil {
		return err
	}
	*r = ids
	return nil
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
	if p.CategoryIDs == nil {
		p.CategoryIDs = []int{}
	}
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
		"fields": partnerFields,
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

// CreatePartnerRequest describes a new partner. Ref is the caller's own identifier (for example the
// e-commerce user ID) and is not unique in Odoo. Empty optional fields are omitted.
type CreatePartnerRequest struct {
	Name            string     `json:"name"`                       // required, not blank
	Email           string     `json:"email,omitempty"`            // optional
	Phone           string     `json:"phone,omitempty"`            // optional
	Ref             string     `json:"ref,omitempty"`              // optional caller identifier, e.g. "user-42"
	Street          string     `json:"street,omitempty"`           // optional
	Street2         string     `json:"street2,omitempty"`          // optional
	City            string     `json:"city,omitempty"`             // optional
	Zip             string     `json:"zip,omitempty"`              // optional
	StateID         int        `json:"state_id,omitempty"`         // optional res.country.state ID; >= 0
	CountryID       int        `json:"country_id,omitempty"`       // optional res.country ID; >= 0
	Website         string     `json:"website,omitempty"`          // optional
	IsCompany       bool       `json:"is_company,omitempty"`       // true for a company, false for a person
	Type            string     `json:"type,omitempty"`             // optional: contact, invoice, delivery or other
	ParentID        int        `json:"parent_id,omitempty"`        // optional res.partner ID; >= 0
	Function        string     `json:"function,omitempty"`         // optional job position
	VAT             string     `json:"vat,omitempty"`              // optional tax ID
	CompanyRegistry string     `json:"company_registry,omitempty"` // optional
	Lang            string     `json:"lang,omitempty"`             // optional installed language code, e.g. "en_US"
	Comment         string     `json:"comment,omitempty"`          // optional internal notes
	CategoryIDs     ReplaceIDs `json:"category_id,omitempty"`      // optional res.partner.category IDs
	UserID          int        `json:"user_id,omitempty"`          // optional res.users ID of the salesperson; >= 0
	CompanyID       int        `json:"company_id,omitempty"`       // optional res.company ID; >= 0
	Barcode         string     `json:"barcode,omitempty"`          // optional
	IndustryID      int        `json:"industry_id,omitempty"`      // optional res.partner.industry ID; >= 0
}

// validPartnerType reports whether t is an Odoo partner address type.
func validPartnerType(t string) bool {
	switch t {
	case "contact", "invoice", "delivery", "other":
		return true
	}
	return false
}

// validateRelationIDs rejects negative IDs in ids (keyed by field name) and non-positive tag IDs.
func validateRelationIDs(ids map[string]int, categories []int) error {
	for field, id := range ids {
		if id < 0 {
			return invalidArgument("%s must not be negative", field)
		}
	}
	for _, id := range categories {
		if id <= 0 {
			return invalidArgument("category_id entries must be positive")
		}
	}
	return nil
}

func validateCreatePartner(input CreatePartnerRequest) error {
	if strings.TrimSpace(input.Name) == "" {
		return invalidArgument("name must not be empty")
	}
	if input.Type != "" && !validPartnerType(input.Type) {
		return invalidArgument("type must be contact, invoice, delivery or other")
	}
	return validateRelationIDs(map[string]int{
		"state_id": input.StateID, "country_id": input.CountryID, "parent_id": input.ParentID,
		"user_id": input.UserID, "company_id": input.CompanyID, "industry_id": input.IndustryID,
	}, input.CategoryIDs)
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
// Name, if set, must not be blank; a pointer to "" clears the other text fields, a pointer to 0 clears
// a relation, and a pointer to an empty CategoryIDs removes all tags.
type UpdatePartnerRequest struct {
	Name            *string     `json:"name,omitempty"`
	Email           *string     `json:"email,omitempty"`
	Phone           *string     `json:"phone,omitempty"`
	Ref             *string     `json:"ref,omitempty"`
	Street          *string     `json:"street,omitempty"`
	Street2         *string     `json:"street2,omitempty"`
	City            *string     `json:"city,omitempty"`
	Zip             *string     `json:"zip,omitempty"`
	StateID         *int        `json:"state_id,omitempty"`   // res.country.state ID; must not be negative
	CountryID       *int        `json:"country_id,omitempty"` // res.country ID; must not be negative
	Website         *string     `json:"website,omitempty"`
	IsCompany       *bool       `json:"is_company,omitempty"`
	Type            *string     `json:"type,omitempty"`      // contact, invoice, delivery or other
	ParentID        *int        `json:"parent_id,omitempty"` // res.partner ID; must not be negative
	Function        *string     `json:"function,omitempty"`
	VAT             *string     `json:"vat,omitempty"`
	CompanyRegistry *string     `json:"company_registry,omitempty"`
	Lang            *string     `json:"lang,omitempty"`
	Comment         *string     `json:"comment,omitempty"`
	CategoryIDs     *ReplaceIDs `json:"category_id,omitempty"` // replaces all tags
	UserID          *int        `json:"user_id,omitempty"`     // res.users ID; must not be negative
	CompanyID       *int        `json:"company_id,omitempty"`  // res.company ID; must not be negative
	Barcode         *string     `json:"barcode,omitempty"`
	IndustryID      *int        `json:"industry_id,omitempty"` // res.partner.industry ID; must not be negative
}

// UpdatePartner changes one partner. It returns ErrPartnerNotFound for an unknown ID and does not retry.
func (c *Client) UpdatePartner(ctx context.Context, id int, patch UpdatePartnerRequest) error {
	var (
		categories []int
		ids        = map[string]int{}
	)

	if patch.Name != nil && strings.TrimSpace(*patch.Name) == "" {
		return invalidArgument("name must not be empty")
	}
	if patch.Type != nil && !validPartnerType(*patch.Type) {
		return invalidArgument("type must be contact, invoice, delivery or other")
	}
	for field, value := range map[string]*int{
		"state_id": patch.StateID, "country_id": patch.CountryID, "parent_id": patch.ParentID,
		"user_id": patch.UserID, "company_id": patch.CompanyID, "industry_id": patch.IndustryID,
	} {
		if value != nil {
			ids[field] = *value
		}
	}
	if patch.CategoryIDs != nil {
		categories = *patch.CategoryIDs
	}
	if err := validateRelationIDs(ids, categories); err != nil {
		return err
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
		for field, text := range map[**string]string{
			&patch.Email: input.Email, &patch.Phone: input.Phone, &patch.Street: input.Street,
			&patch.Street2: input.Street2, &patch.City: input.City, &patch.Zip: input.Zip,
			&patch.Website: input.Website, &patch.Type: input.Type, &patch.Function: input.Function,
			&patch.VAT: input.VAT, &patch.CompanyRegistry: input.CompanyRegistry, &patch.Lang: input.Lang,
			&patch.Comment: input.Comment, &patch.Barcode: input.Barcode,
		} {
			if text != "" {
				t := text
				*field = &t
			}
		}
		for field, number := range map[**int]int{
			&patch.StateID: input.StateID, &patch.CountryID: input.CountryID, &patch.ParentID: input.ParentID,
			&patch.UserID: input.UserID, &patch.CompanyID: input.CompanyID, &patch.IndustryID: input.IndustryID,
		} {
			if number != 0 {
				n := number
				*field = &n
			}
		}
		if input.IsCompany {
			patch.IsCompany = &input.IsCompany
		}
		if len(input.CategoryIDs) > 0 {
			patch.CategoryIDs = &input.CategoryIDs
		}
		if values, err = patchValues(patch); err != nil {
			return 0, false, err
		}
		return found[0].ID, false, c.writeRecord(ctx, "res.partner", found[0].ID, values)
	default:
		return 0, false, fmt.Errorf("%w: %q", ErrAmbiguousPartner, ref)
	}
}
