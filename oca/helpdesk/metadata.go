package helpdesk

import (
	"context"
	"fmt"
	"strings"

	odoo "github.com/d2jvkpn/odoo-go"
)

// TicketCategory is a helpdesk.ticket.category record.
type TicketCategory struct {
	ID           int           `json:"id"`
	Name         string        `json:"name"`
	CompleteName string        `json:"complete_name"`
	Parent       odoo.Many2One `json:"parent_id"`
	Company      odoo.Many2One `json:"company_id"`
	Sequence     int           `json:"sequence"`
	Active       bool          `json:"active"`
	ShowInPortal bool          `json:"show_in_portal"`
}

// TicketTag is a helpdesk.ticket.tag record.
type TicketTag struct {
	ID       int           `json:"id"`
	Name     string        `json:"name"`
	Color    int           `json:"color"`
	Company  odoo.Many2One `json:"company_id"`
	Sequence int           `json:"sequence"`
	Active   bool          `json:"active"`
}

// TicketUser is a res.users record returned as a helpdesk team member.
type TicketUser struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// CreateTicketCategoryRequest describes a new ticket category. Name is required. Zero relation
// IDs and Sequence leave Odoo's defaults in place.
type CreateTicketCategoryRequest struct {
	Name         string `json:"name"`
	ParentID     int    `json:"parent_id,omitempty"`
	CompanyID    int    `json:"company_id,omitempty"`
	Sequence     int    `json:"sequence,omitempty"`
	Active       *bool  `json:"active,omitempty"`
	ShowInPortal *bool  `json:"show_in_portal,omitempty"`
}

// UpdateTicketCategoryRequest changes selected category fields. A zero relation ID clears it.
type UpdateTicketCategoryRequest struct {
	Name         *string `json:"name,omitempty"`
	ParentID     *int    `json:"parent_id,omitempty"`
	CompanyID    *int    `json:"company_id,omitempty"`
	Sequence     *int    `json:"sequence,omitempty"`
	Active       *bool   `json:"active,omitempty"`
	ShowInPortal *bool   `json:"show_in_portal,omitempty"`
}

// CreateTicketTagRequest describes a new ticket tag. Name is required.
type CreateTicketTagRequest struct {
	Name   string `json:"name"`
	Color  int    `json:"color,omitempty"`
	Active *bool  `json:"active,omitempty"`
}

// UpdateTicketTagRequest changes selected tag fields.
type UpdateTicketTagRequest struct {
	Name   *string `json:"name,omitempty"`
	Color  *int    `json:"color,omitempty"`
	Active *bool   `json:"active,omitempty"`
}

func validateCreateTicketCategory(input CreateTicketCategoryRequest) error {
	if strings.TrimSpace(input.Name) == "" {
		return invalidArgument("name must not be empty")
	}
	if input.ParentID < 0 {
		return invalidArgument("parent_id must not be negative")
	}
	if input.CompanyID < 0 {
		return invalidArgument("company_id must not be negative")
	}
	if input.Sequence < 0 {
		return invalidArgument("sequence must not be negative")
	}
	return nil
}

func validateUpdateTicketCategory(patch UpdateTicketCategoryRequest) error {
	if patch.Name != nil && strings.TrimSpace(*patch.Name) == "" {
		return invalidArgument("name must not be empty")
	}
	if patch.ParentID != nil && *patch.ParentID < 0 {
		return invalidArgument("parent_id must not be negative")
	}
	if patch.CompanyID != nil && *patch.CompanyID < 0 {
		return invalidArgument("company_id must not be negative")
	}
	if patch.Sequence != nil && *patch.Sequence < 0 {
		return invalidArgument("sequence must not be negative")
	}
	return nil
}

func validateCreateTicketTag(input CreateTicketTagRequest) error {
	if strings.TrimSpace(input.Name) == "" {
		return invalidArgument("name must not be empty")
	}
	if input.Color < 0 {
		return invalidArgument("color must not be negative")
	}
	return nil
}

func validateUpdateTicketTag(patch UpdateTicketTagRequest) error {
	if patch.Name != nil && strings.TrimSpace(*patch.Name) == "" {
		return invalidArgument("name must not be empty")
	}
	if patch.Color != nil && *patch.Color < 0 {
		return invalidArgument("color must not be negative")
	}
	return nil
}

func (c *Client) createTicketMetadata(ctx context.Context, model, noun string, input any) (int, error) {
	var (
		err error
		ids []int
	)

	err = c.Call(ctx, model, "create", map[string]any{"vals_list": []any{input}}, &ids)
	if err != nil {
		return 0, fmt.Errorf("create ticket %s: %w", noun, err)
	}
	if len(ids) != 1 || ids[0] <= 0 {
		return 0, fmt.Errorf("%w: expected one positive ID from ticket %s create", odoo.ErrUnexpectedResponse, noun)
	}
	return ids[0], nil
}

func (c *Client) writeTicketMetadata(
	ctx context.Context,
	model, noun string,
	id int,
	vals map[string]any,
) error {
	var err error

	err = c.Call(ctx, model, "write", map[string]any{"ids": []int{id}, "vals": vals}, nil)
	if err != nil {
		return fmt.Errorf("update ticket %s %d: %w", noun, id, err)
	}
	return nil
}

func (c *Client) deleteTicketMetadata(ctx context.Context, model, noun string, id int) error {
	var err error

	if id <= 0 {
		return invalidArgument("ticket %s ID must be positive", noun)
	}
	err = c.Call(ctx, model, "unlink", map[string]any{"ids": []int{id}}, nil)
	if err != nil {
		return fmt.Errorf("delete ticket %s %d: %w", noun, id, err)
	}
	return nil
}

// ListTicketCategories returns active ticket categories ordered by sequence and ID.
func (c *Client) ListTicketCategories(ctx context.Context) ([]TicketCategory, error) {
	var (
		categories []TicketCategory
		err        error
	)

	categories = make([]TicketCategory, 0)
	err = c.CallRead(ctx, "helpdesk.ticket.category", "search_read", map[string]any{
		"domain": []any{},
		"fields": []string{
			"id", "name", "complete_name", "parent_id", "company_id", "sequence", "active", "show_in_portal",
		},
		"order": "sequence, id",
	}, &categories)
	if err != nil {
		return nil, fmt.Errorf("list ticket categories: %w", err)
	}
	return categories, nil
}

// ListTicketTags returns active ticket tags ordered by sequence and ID.
func (c *Client) ListTicketTags(ctx context.Context) ([]TicketTag, error) {
	var (
		err  error
		tags []TicketTag
	)

	tags = make([]TicketTag, 0)
	err = c.CallRead(ctx, "helpdesk.ticket.tag", "search_read", map[string]any{
		"domain": []any{},
		"fields": []string{"id", "name", "color", "company_id", "sequence", "active"},
		"order":  "sequence, id",
	}, &tags)
	if err != nil {
		return nil, fmt.Errorf("list ticket tags: %w", err)
	}
	return tags, nil
}

// CreateTicketCategory creates one ticket category and returns its ID. It never retries.
func (c *Client) CreateTicketCategory(ctx context.Context, input CreateTicketCategoryRequest) (int, error) {
	var err error

	if err = validateCreateTicketCategory(input); err != nil {
		return 0, err
	}
	return c.createTicketMetadata(ctx, "helpdesk.ticket.category", "category", input)
}

// UpdateTicketCategory changes selected fields of one ticket category. It never retries.
func (c *Client) UpdateTicketCategory(ctx context.Context, id int, patch UpdateTicketCategoryRequest) error {
	var (
		err  error
		vals map[string]any
	)

	if id <= 0 {
		return invalidArgument("ticket category ID must be positive")
	}
	if err = validateUpdateTicketCategory(patch); err != nil {
		return err
	}
	if vals, err = patchValues(patch); err != nil {
		return err
	}
	return c.writeTicketMetadata(ctx, "helpdesk.ticket.category", "category", id, vals)
}

// DeleteTicketCategory permanently deletes one ticket category. It never retries.
func (c *Client) DeleteTicketCategory(ctx context.Context, id int) error {
	return c.deleteTicketMetadata(ctx, "helpdesk.ticket.category", "category", id)
}

// CreateTicketTag creates one ticket tag and returns its ID. It never retries.
func (c *Client) CreateTicketTag(ctx context.Context, input CreateTicketTagRequest) (int, error) {
	var err error

	if err = validateCreateTicketTag(input); err != nil {
		return 0, err
	}
	return c.createTicketMetadata(ctx, "helpdesk.ticket.tag", "tag", input)
}

// UpdateTicketTag changes selected fields of one ticket tag. It never retries.
func (c *Client) UpdateTicketTag(ctx context.Context, id int, patch UpdateTicketTagRequest) error {
	var (
		err  error
		vals map[string]any
	)

	if id <= 0 {
		return invalidArgument("ticket tag ID must be positive")
	}
	if err = validateUpdateTicketTag(patch); err != nil {
		return err
	}
	if vals, err = patchValues(patch); err != nil {
		return err
	}
	return c.writeTicketMetadata(ctx, "helpdesk.ticket.tag", "tag", id, vals)
}

// DeleteTicketTag permanently deletes one ticket tag. It never retries.
func (c *Client) DeleteTicketTag(ctx context.Context, id int) error {
	return c.deleteTicketMetadata(ctx, "helpdesk.ticket.tag", "tag", id)
}

// ListTicketTeamMembers returns the users assigned to one helpdesk team.
func (c *Client) ListTicketTeamMembers(ctx context.Context, teamID int) ([]TicketUser, error) {
	var (
		err   error
		users []TicketUser
	)

	if teamID <= 0 {
		return nil, invalidArgument("ticket team ID must be positive")
	}
	users = make([]TicketUser, 0)
	err = c.CallRead(ctx, "res.users", "search_read", map[string]any{
		"domain": []any{[]any{"helpdesk_team_ids", "=", teamID}},
		"fields": []string{"id", "name"},
		"order":  "name, id",
	}, &users)
	if err != nil {
		return nil, fmt.Errorf("list ticket team %d members: %w", teamID, err)
	}
	return users, nil
}

// ListTicketTeamStages returns the active stages applicable to a team. It matches OCA's
// _get_applicable_stages company and team rules.
func (c *Client) ListTicketTeamStages(ctx context.Context, teamID int) ([]TicketStage, error) {
	var (
		companyIDs []any
		err        error
		stages     []TicketStage
		teams      []struct {
			ID      int           `json:"id"`
			Company odoo.Many2One `json:"company_id"`
		}
	)

	if teamID <= 0 {
		return nil, invalidArgument("ticket team ID must be positive")
	}
	err = c.CallRead(ctx, "helpdesk.ticket.team", "search_read", map[string]any{
		"domain":  []any{[]any{"id", "=", teamID}},
		"fields":  []string{"id", "company_id"},
		"limit":   1,
		"context": map[string]any{"active_test": false},
	}, &teams)
	if err != nil {
		return nil, fmt.Errorf("get ticket team %d: %w", teamID, err)
	}
	if len(teams) == 0 {
		return nil, fmt.Errorf("ticket team %d not found", teamID)
	}
	companyIDs = []any{false}
	if teams[0].Company.ID > 0 {
		companyIDs = append(companyIDs, teams[0].Company.ID)
	}
	stages = make([]TicketStage, 0)
	err = c.CallRead(ctx, "helpdesk.ticket.stage", "search_read", map[string]any{
		"domain": []any{
			[]any{"company_id", "in", companyIDs},
			"|",
			[]any{"team_ids", "=", false},
			[]any{"team_ids", "=", teamID},
		},
		"fields": ticketStageFields,
		"order":  "sequence",
	}, &stages)
	if err != nil {
		return nil, fmt.Errorf("list ticket team %d stages: %w", teamID, err)
	}
	return stages, nil
}
