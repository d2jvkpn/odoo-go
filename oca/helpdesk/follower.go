package helpdesk

import (
	"context"
	"fmt"

	odoo "github.com/d2jvkpn/odoo-go"
)

// TicketFollower is a mail.followers record attached to a helpdesk ticket.
type TicketFollower struct {
	ID         int           `json:"id"`
	Partner    odoo.Many2One `json:"partner_id"`
	SubtypeIDs []int         `json:"subtype_ids"`
}

// validatePositiveIDs requires a non-empty list of positive record IDs.
func validatePositiveIDs(field string, ids []int) error {
	if len(ids) == 0 {
		return invalidArgument("%s must contain at least one ID", field)
	}
	for _, id := range ids {
		if id <= 0 {
			return invalidArgument("%s IDs must be positive", field)
		}
	}
	return nil
}

// validateOptionalPositiveIDs requires every supplied record ID to be positive.
func validateOptionalPositiveIDs(field string, ids []int) error {
	for _, id := range ids {
		if id <= 0 {
			return invalidArgument("%s IDs must be positive", field)
		}
	}
	return nil
}

// ListTicketFollowers returns the partners following a ticket, ordered by follower ID.
func (c *Client) ListTicketFollowers(ctx context.Context, id int) ([]TicketFollower, error) {
	var (
		err       error
		followers []TicketFollower
	)

	if id <= 0 {
		return nil, invalidArgument("ticket ID must be positive")
	}
	if err = c.requireTicket(ctx, id); err != nil {
		return nil, err
	}
	followers = make([]TicketFollower, 0)
	err = c.CallRead(ctx, "mail.followers", "search_read", map[string]any{
		"domain": []any{
			[]any{"res_model", "=", "helpdesk.ticket"},
			[]any{"res_id", "=", id},
		},
		"fields": []string{"id", "partner_id", "subtype_ids"},
		"order":  "id asc",
	}, &followers)
	if err != nil {
		return nil, fmt.Errorf("list ticket %d followers: %w", id, err)
	}
	return followers, nil
}

// SubscribeTicket adds partners as followers of a ticket. An empty subtypeIDs list lets Odoo
// select the model's default subtypes. The write is attempted once and is never retried.
func (c *Client) SubscribeTicket(ctx context.Context, id int, partnerIDs, subtypeIDs []int) error {
	var (
		err    error
		params map[string]any
		result bool
	)

	if id <= 0 {
		return invalidArgument("ticket ID must be positive")
	}
	if err = validatePositiveIDs("partner_ids", partnerIDs); err != nil {
		return err
	}
	if err = validateOptionalPositiveIDs("subtype_ids", subtypeIDs); err != nil {
		return err
	}
	if err = c.requireTicket(ctx, id); err != nil {
		return err
	}
	params = map[string]any{
		"ids":         []int{id},
		"partner_ids": partnerIDs,
	}
	if len(subtypeIDs) > 0 {
		params["subtype_ids"] = subtypeIDs
	}
	err = c.Call(ctx, "helpdesk.ticket", "message_subscribe", params, &result)
	if err != nil {
		return fmt.Errorf("subscribe ticket %d followers: %w", id, err)
	}
	if !result {
		return fmt.Errorf("%w: ticket %d subscription returned false", odoo.ErrUnexpectedResponse, id)
	}
	return nil
}

// UnsubscribeTicket removes partners from a ticket's followers. The write is attempted once and
// is never retried.
func (c *Client) UnsubscribeTicket(ctx context.Context, id int, partnerIDs []int) error {
	var (
		err    error
		result *bool
	)

	if id <= 0 {
		return invalidArgument("ticket ID must be positive")
	}
	if err = validatePositiveIDs("partner_ids", partnerIDs); err != nil {
		return err
	}
	if err = c.requireTicket(ctx, id); err != nil {
		return err
	}
	err = c.Call(ctx, "helpdesk.ticket", "message_unsubscribe", map[string]any{
		"ids":         []int{id},
		"partner_ids": partnerIDs,
	}, &result)
	if err != nil {
		return fmt.Errorf("unsubscribe ticket %d followers: %w", id, err)
	}
	if result != nil && !*result {
		return fmt.Errorf("%w: ticket %d unsubscription returned false", odoo.ErrUnexpectedResponse, id)
	}
	return nil
}
