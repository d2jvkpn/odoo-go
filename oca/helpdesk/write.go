package helpdesk

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	odoo "github.com/d2jvkpn/odoo-go"
)

// CreateTicketRequest describes a new ticket. Required: Name and Description.
type CreateTicketRequest struct {
	Name         string          `json:"name"`                     // required, not blank
	Description  string          `json:"description"`              // required
	PartnerID    int             `json:"partner_id,omitempty"`     // res.partner; >= 0
	PartnerName  string          `json:"partner_name,omitempty"`   // contact name without a linked partner
	PartnerEmail string          `json:"partner_email,omitempty"`  // contact email
	CompanyID    int             `json:"company_id,omitempty"`     // res.company; >= 0
	SaleOrderIDs odoo.ReplaceIDs `json:"sale_order_ids,omitempty"` // linked sale.order IDs
	TeamID       int             `json:"team_id,omitempty"`        // >= 0
	StageID      int             `json:"stage_id,omitempty"`       // >= 0
	UserID       int             `json:"user_id,omitempty"`        // assigned res.users; >= 0
	Priority     string          `json:"priority,omitempty"`       // "0".."3", empty for default
	ChannelID    int             `json:"channel_id,omitempty"`     // >= 0
	CategoryID   int             `json:"category_id,omitempty"`    // >= 0
	TagIDs       odoo.ReplaceIDs `json:"tag_ids,omitempty"`        // helpdesk.ticket.tag IDs
	ProductID    int             `json:"product_id,omitempty"`     // >= 0
}

// UpdateTicketRequest changes selected fields of a ticket. Nil fields are left unchanged; a
// pointer to 0 clears a relation, and a pointer to an empty SaleOrderIDs/TagIDs removes all links.
type UpdateTicketRequest struct {
	Name         *string          `json:"name,omitempty"`
	Description  *string          `json:"description,omitempty"`
	PartnerID    *int             `json:"partner_id,omitempty"`
	PartnerName  *string          `json:"partner_name,omitempty"`
	PartnerEmail *string          `json:"partner_email,omitempty"`
	CompanyID    *int             `json:"company_id,omitempty"`
	SaleOrderIDs *odoo.ReplaceIDs `json:"sale_order_ids,omitempty"`
	TeamID       *int             `json:"team_id,omitempty"`
	StageID      *int             `json:"stage_id,omitempty"`
	UserID       *int             `json:"user_id,omitempty"`
	Priority     *string          `json:"priority,omitempty"`
	ChannelID    *int             `json:"channel_id,omitempty"`
	CategoryID   *int             `json:"category_id,omitempty"`
	TagIDs       *odoo.ReplaceIDs `json:"tag_ids,omitempty"`
	ProductID    *int             `json:"product_id,omitempty"`
}

// validPriority reports whether p is one of Odoo's ticket priorities.
func validPriority(p string) bool {
	switch p {
	case "0", "1", "2", "3":
		return true
	}
	return false
}

func validateRelationIDs(field string, ids odoo.ReplaceIDs) error {
	for _, id := range ids {
		if id <= 0 {
			return invalidArgument("%s must contain only positive IDs", field)
		}
	}
	return nil
}

// validateCreateTicket checks the request shape; Odoo-side rules are left to the server.
func validateCreateTicket(input CreateTicketRequest) error {
	if strings.TrimSpace(input.Name) == "" {
		return invalidArgument("name must not be empty")
	}
	if strings.TrimSpace(input.Description) == "" {
		return invalidArgument("description must not be empty")
	}
	for _, f := range []struct {
		name string
		id   int
	}{
		{"partner_id", input.PartnerID}, {"company_id", input.CompanyID},
		{"team_id", input.TeamID}, {"stage_id", input.StageID}, {"user_id", input.UserID},
		{"channel_id", input.ChannelID}, {"category_id", input.CategoryID},
		{"product_id", input.ProductID},
	} {
		if f.id < 0 {
			return invalidArgument("%s must not be negative", f.name)
		}
	}
	if input.Priority != "" && !validPriority(input.Priority) {
		return invalidArgument("priority must be 0, 1, 2 or 3")
	}
	if err := validateRelationIDs("sale_order_ids", input.SaleOrderIDs); err != nil {
		return err
	}
	if err := validateRelationIDs("tag_ids", input.TagIDs); err != nil {
		return err
	}
	return nil
}

// validateUpdateTicket checks the patch shape.
func validateUpdateTicket(patch UpdateTicketRequest) error {
	if patch.Name != nil && strings.TrimSpace(*patch.Name) == "" {
		return invalidArgument("name must not be empty")
	}
	if patch.Priority != nil && *patch.Priority != "" && !validPriority(*patch.Priority) {
		return invalidArgument("priority must be 0, 1, 2 or 3")
	}
	for _, f := range []struct {
		name string
		id   *int
	}{
		{"partner_id", patch.PartnerID}, {"company_id", patch.CompanyID},
		{"team_id", patch.TeamID}, {"stage_id", patch.StageID}, {"user_id", patch.UserID},
		{"channel_id", patch.ChannelID}, {"category_id", patch.CategoryID},
		{"product_id", patch.ProductID},
	} {
		if f.id != nil && *f.id < 0 {
			return invalidArgument("%s must not be negative", f.name)
		}
	}
	if patch.SaleOrderIDs != nil {
		if err := validateRelationIDs("sale_order_ids", *patch.SaleOrderIDs); err != nil {
			return err
		}
	}
	if patch.TagIDs != nil {
		if err := validateRelationIDs("tag_ids", *patch.TagIDs); err != nil {
			return err
		}
	}
	return nil
}

// patchValues encodes a patch struct into Odoo write values. A zero value for a *_id relation is
// sent as false; a patch that sets nothing is rejected so no empty write is ever sent.
func patchValues(patch any) (map[string]any, error) {
	var (
		err  error
		data []byte
		vals map[string]any
	)

	if data, err = json.Marshal(patch); err != nil {
		return nil, fmt.Errorf("encode patch: %w", err)
	}
	if err = json.Unmarshal(data, &vals); err != nil {
		return nil, fmt.Errorf("encode patch: %w", err)
	}
	if len(vals) == 0 {
		return nil, invalidArgument("patch must set at least one field")
	}
	for field, value := range vals {
		if number, ok := value.(float64); ok && number == 0 && strings.HasSuffix(field, "_id") {
			vals[field] = false
		}
	}
	return vals, nil
}

// ticketExists reports whether id exists in helpdesk.ticket.
func (c *Client) ticketExists(ctx context.Context, id int) (bool, error) {
	var (
		err  error
		rows []struct {
			ID int `json:"id"`
		}
	)

	err = c.CallRead(ctx, "helpdesk.ticket", "search_read", map[string]any{
		"domain": []any{[]any{"id", "=", id}},
		"fields": []string{"id"},
		"limit":  1,
		"context": map[string]any{
			"active_test": false,
		},
	}, &rows)
	if err != nil {
		return false, fmt.Errorf("check ticket %d: %w", id, err)
	}
	return len(rows) == 1, nil
}

// writeTicket writes vals to one ticket. It does not retry and does not check existence.
func (c *Client) writeTicket(ctx context.Context, id int, vals map[string]any) error {
	var err error

	err = c.Call(ctx, "helpdesk.ticket", "write", map[string]any{
		"ids":  []int{id},
		"vals": vals,
	}, nil)
	if err != nil {
		return fmt.Errorf("update ticket %d: %w", id, err)
	}
	return nil
}

// CreateTicket creates one ticket and returns its ID. It does not retry. After a transport/decode
// error the write outcome may be unknown; search by name before resubmitting, to avoid duplicates.
func (c *Client) CreateTicket(ctx context.Context, input CreateTicketRequest) (int, error) {
	var (
		err error
		ids []int
	)

	if err = validateCreateTicket(input); err != nil {
		return 0, err
	}
	err = c.Call(ctx, "helpdesk.ticket", "create", map[string]any{
		"vals_list": []any{input},
	}, &ids)
	if err != nil {
		return 0, fmt.Errorf("create ticket: %w", err)
	}
	if len(ids) != 1 || ids[0] <= 0 {
		return 0, fmt.Errorf(
			"%w: expected one positive ID from create (ticket may already exist)", odoo.ErrUnexpectedResponse,
		)
	}
	return ids[0], nil
}

// UpdateTicket changes selected fields of one ticket. It returns ErrTicketNotFound for an unknown
// ID and does not retry.
func (c *Client) UpdateTicket(ctx context.Context, id int, patch UpdateTicketRequest) error {
	var (
		err  error
		vals map[string]any
	)

	if id <= 0 {
		return invalidArgument("ticket ID must be positive")
	}
	if err = validateUpdateTicket(patch); err != nil {
		return err
	}
	if vals, err = patchValues(patch); err != nil {
		return err
	}
	if err = c.requireTicket(ctx, id); err != nil {
		return err
	}
	return c.writeTicket(ctx, id, vals)
}
