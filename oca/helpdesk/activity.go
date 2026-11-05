package helpdesk

import (
	"context"
	jsonv2 "encoding/json/v2"
	"fmt"
	"time"

	odoo "github.com/d2jvkpn/odoo-go"
)

// ActivityRequest describes an activity to schedule on a ticket. Zero TypeID and UserID values,
// and empty text or deadline values, let Odoo apply the activity type and server defaults.
type ActivityRequest struct {
	TypeID       int    `json:"activity_type_id,omitempty"`
	UserID       int    `json:"user_id,omitempty"`
	Summary      string `json:"summary,omitempty"`
	Note         string `json:"note,omitempty"`
	DateDeadline string `json:"date_deadline,omitempty"`
}

// UpdateActivityRequest changes selected fields of a ticket activity. Nil fields are unchanged.
type UpdateActivityRequest struct {
	TypeID       *int    `json:"activity_type_id,omitempty"`
	UserID       *int    `json:"user_id,omitempty"`
	Summary      *string `json:"summary,omitempty"`
	Note         *string `json:"note,omitempty"`
	DateDeadline *string `json:"date_deadline,omitempty"`
}

// TicketActivity is a mail.activity attached to a helpdesk ticket.
type TicketActivity struct {
	ID           int           `json:"id"`
	ActivityType odoo.Many2One `json:"activity_type_id"`
	User         odoo.Many2One `json:"user_id"`
	Summary      string        `json:"summary"`
	Note         string        `json:"note"`
	DateDeadline string        `json:"date_deadline"`
	State        string        `json:"state"`
}

// UnmarshalJSON maps Odoo false/null optional activity strings to empty strings.
func (a *TicketActivity) UnmarshalJSON(data []byte) error {
	type plain TicketActivity
	var aux struct {
		plain
		Summary odoo.OptionalString `json:"summary"`
		Note    odoo.OptionalString `json:"note"`
	}

	if err := jsonv2.Unmarshal(data, &aux); err != nil {
		return err
	}
	*a = TicketActivity(aux.plain)
	a.Summary = string(aux.Summary)
	a.Note = string(aux.Note)
	return nil
}

// TicketActivityType is an activity type available globally or for helpdesk tickets.
type TicketActivityType struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Summary  string `json:"summary"`
	Sequence int    `json:"sequence"`
	Icon     string `json:"icon"`
	ResModel string `json:"res_model"`
}

// UnmarshalJSON maps Odoo false/null optional activity type strings to empty strings.
func (a *TicketActivityType) UnmarshalJSON(data []byte) error {
	type plain TicketActivityType
	var aux struct {
		plain
		Summary  odoo.OptionalString `json:"summary"`
		Icon     odoo.OptionalString `json:"icon"`
		ResModel odoo.OptionalString `json:"res_model"`
	}

	if err := jsonv2.Unmarshal(data, &aux); err != nil {
		return err
	}
	*a = TicketActivityType(aux.plain)
	a.Summary = string(aux.Summary)
	a.Icon = string(aux.Icon)
	a.ResModel = string(aux.ResModel)
	return nil
}

// validDate reports whether value is a real calendar date in Odoo's YYYY-MM-DD format.
func validDate(value string) bool {
	var (
		err    error
		parsed time.Time
	)

	if parsed, err = time.Parse(time.DateOnly, value); err != nil {
		return false
	}
	return parsed.Format(time.DateOnly) == value
}

// validateActivityRequest checks the values that can be validated without contacting Odoo.
func validateActivityRequest(input ActivityRequest) error {
	if input.TypeID < 0 {
		return invalidArgument("activity_type_id must not be negative")
	}
	if input.UserID < 0 {
		return invalidArgument("user_id must not be negative")
	}
	if input.DateDeadline != "" && !validDate(input.DateDeadline) {
		return invalidArgument("date_deadline must be a valid date in YYYY-MM-DD format")
	}
	return nil
}

// validateUpdateActivity checks a patch before any ownership lookup or write.
func validateUpdateActivity(patch UpdateActivityRequest) error {
	if patch.TypeID != nil && *patch.TypeID <= 0 {
		return invalidArgument("activity_type_id must be positive")
	}
	if patch.UserID != nil && *patch.UserID <= 0 {
		return invalidArgument("user_id must be positive")
	}
	if patch.DateDeadline != nil && !validDate(*patch.DateDeadline) {
		return invalidArgument("date_deadline must be a valid date in YYYY-MM-DD format")
	}
	return nil
}

// ScheduleTicketActivity schedules one activity through helpdesk.ticket.activity_schedule. It
// does not retry the write and returns the single mail.activity ID returned by JSON-2.
func (c *Client) ScheduleTicketActivity(ctx context.Context, id int, input ActivityRequest) (int, error) {
	var (
		err    error
		ids    []int
		params map[string]any
	)

	if id <= 0 {
		return 0, invalidArgument("ticket ID must be positive")
	}
	if err = validateActivityRequest(input); err != nil {
		return 0, err
	}
	if err = c.requireTicket(ctx, id); err != nil {
		return 0, err
	}
	params = map[string]any{"ids": []int{id}}
	if input.TypeID > 0 {
		params["activity_type_id"] = input.TypeID
	}
	if input.UserID > 0 {
		params["user_id"] = input.UserID
	}
	if input.Summary != "" {
		params["summary"] = input.Summary
	}
	if input.Note != "" {
		params["note"] = input.Note
	}
	if input.DateDeadline != "" {
		params["date_deadline"] = input.DateDeadline
	}
	err = c.Call(ctx, "helpdesk.ticket", "activity_schedule", params, &ids)
	if err != nil {
		return 0, fmt.Errorf("schedule activity on ticket %d: %w", id, err)
	}
	if len(ids) != 1 || ids[0] <= 0 {
		return 0, fmt.Errorf("%w: expected one positive activity ID", odoo.ErrUnexpectedResponse)
	}
	return ids[0], nil
}

// ListTicketActivities returns the ticket's active activities ordered by deadline and ID.
func (c *Client) ListTicketActivities(ctx context.Context, id int) ([]TicketActivity, error) {
	var (
		activities []TicketActivity
		err        error
	)

	if id <= 0 {
		return nil, invalidArgument("ticket ID must be positive")
	}
	if err = c.requireTicket(ctx, id); err != nil {
		return nil, err
	}
	activities = make([]TicketActivity, 0)
	err = c.CallRead(ctx, "mail.activity", "search_read", map[string]any{
		"domain": []any{
			[]any{"res_model", "=", "helpdesk.ticket"},
			[]any{"res_id", "=", id},
		},
		"fields": []string{
			"id", "activity_type_id", "user_id", "summary", "note", "date_deadline", "state",
		},
		"order": "date_deadline asc, id asc",
	}, &activities)
	if err != nil {
		return nil, fmt.Errorf("list ticket %d activities: %w", id, err)
	}
	return activities, nil
}

// ticketOwnsActivity verifies both the activity model and ticket ID before a child mutation.
func (c *Client) ticketOwnsActivity(ctx context.Context, ticketID, activityID int) (bool, error) {
	var (
		activities []struct {
			ID int `json:"id"`
		}
		err error
	)

	err = c.CallRead(ctx, "mail.activity", "search_read", map[string]any{
		"domain": []any{
			[]any{"id", "=", activityID},
			[]any{"res_model", "=", "helpdesk.ticket"},
			[]any{"res_id", "=", ticketID},
		},
		"fields": []string{"id"},
		"limit":  1,
	}, &activities)
	if err != nil {
		return false, fmt.Errorf("check ticket %d activity %d: %w", ticketID, activityID, err)
	}
	return len(activities) == 1 && activities[0].ID == activityID, nil
}

// requireTicketActivity rejects an activity that is not owned by the supplied ticket.
func (c *Client) requireTicketActivity(ctx context.Context, ticketID, activityID int) error {
	var (
		err   error
		owned bool
	)

	if owned, err = c.ticketOwnsActivity(ctx, ticketID, activityID); err != nil {
		return err
	}
	if !owned {
		return fmt.Errorf("activity %d does not belong to ticket %d", activityID, ticketID)
	}
	return nil
}

// UpdateTicketActivity changes selected fields after verifying the activity belongs to the ticket.
// It does not retry the write.
func (c *Client) UpdateTicketActivity(
	ctx context.Context,
	ticketID, activityID int,
	patch UpdateActivityRequest,
) error {
	var (
		err    error
		result bool
		vals   map[string]any
	)

	if ticketID <= 0 {
		return invalidArgument("ticket ID must be positive")
	}
	if activityID <= 0 {
		return invalidArgument("activity ID must be positive")
	}
	if err = validateUpdateActivity(patch); err != nil {
		return err
	}
	if vals, err = patchValues(patch); err != nil {
		return err
	}
	if err = c.requireTicketActivity(ctx, ticketID, activityID); err != nil {
		return err
	}
	err = c.Call(ctx, "mail.activity", "write", map[string]any{
		"ids":  []int{activityID},
		"vals": vals,
	}, &result)
	if err != nil {
		return fmt.Errorf("update ticket %d activity %d: %w", ticketID, activityID, err)
	}
	if !result {
		return fmt.Errorf("%w: activity %d update returned false", odoo.ErrUnexpectedResponse, activityID)
	}
	return nil
}

// CompleteTicketActivity completes an activity with optional feedback. Odoo owns any configured
// follow-up scheduling. The write is attempted once and is never retried.
func (c *Client) CompleteTicketActivity(
	ctx context.Context,
	ticketID, activityID int,
	feedback string,
) error {
	var err error

	if ticketID <= 0 {
		return invalidArgument("ticket ID must be positive")
	}
	if activityID <= 0 {
		return invalidArgument("activity ID must be positive")
	}
	if err = c.requireTicketActivity(ctx, ticketID, activityID); err != nil {
		return err
	}
	err = c.Call(ctx, "mail.activity", "action_feedback", map[string]any{
		"ids":      []int{activityID},
		"feedback": feedback,
	}, nil)
	if err != nil {
		return fmt.Errorf("complete ticket %d activity %d: %w", ticketID, activityID, err)
	}
	return nil
}

// DeleteTicketActivity deletes an activity after verifying that it belongs to the ticket. It does
// not retry the write.
func (c *Client) DeleteTicketActivity(ctx context.Context, ticketID, activityID int) error {
	var (
		err    error
		result bool
	)

	if ticketID <= 0 {
		return invalidArgument("ticket ID must be positive")
	}
	if activityID <= 0 {
		return invalidArgument("activity ID must be positive")
	}
	if err = c.requireTicketActivity(ctx, ticketID, activityID); err != nil {
		return err
	}
	err = c.Call(ctx, "mail.activity", "unlink", map[string]any{
		"ids": []int{activityID},
	}, &result)
	if err != nil {
		return fmt.Errorf("delete ticket %d activity %d: %w", ticketID, activityID, err)
	}
	if !result {
		return fmt.Errorf("%w: activity %d deletion returned false", odoo.ErrUnexpectedResponse, activityID)
	}
	return nil
}

// ListTicketActivityTypes returns activity types available globally or for helpdesk tickets.
func (c *Client) ListTicketActivityTypes(ctx context.Context) ([]TicketActivityType, error) {
	var (
		err   error
		types []TicketActivityType
	)

	types = make([]TicketActivityType, 0)
	err = c.CallRead(ctx, "mail.activity.type", "search_read", map[string]any{
		"domain": []any{
			"|",
			[]any{"res_model", "=", false},
			[]any{"res_model", "=", "helpdesk.ticket"},
		},
		"fields": []string{"id", "name", "summary", "sequence", "icon", "res_model"},
		"order":  "sequence asc, id asc",
	}, &types)
	if err != nil {
		return nil, fmt.Errorf("list ticket activity types: %w", err)
	}
	return types, nil
}
