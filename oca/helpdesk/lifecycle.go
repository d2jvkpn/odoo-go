package helpdesk

import (
	"context"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"fmt"

	odoo "github.com/d2jvkpn/odoo-go"
)

func (c *Client) requireTicket(ctx context.Context, id int) error {
	var (
		err    error
		exists bool
	)

	if id <= 0 {
		return invalidArgument("ticket ID must be positive")
	}
	if exists, err = c.ticketExists(ctx, id); err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("%w: %d", ErrTicketNotFound, id)
	}
	return nil
}

// SetTicketActive archives or restores one ticket. Archived tickets are included in the
// existence check. The write is attempted once and is never retried.
func (c *Client) SetTicketActive(ctx context.Context, id int, active bool) error {
	var err error

	if err = c.requireTicket(ctx, id); err != nil {
		return err
	}
	return c.writeTicket(ctx, id, map[string]any{"active": active})
}

// DeleteTicket permanently deletes one ticket. Archived tickets are included in the existence
// check. The write is attempted once and is never retried.
func (c *Client) DeleteTicket(ctx context.Context, id int) error {
	var err error

	if err = c.requireTicket(ctx, id); err != nil {
		return err
	}
	err = c.Call(ctx, "helpdesk.ticket", "unlink", map[string]any{"ids": []int{id}}, nil)
	if err != nil {
		return fmt.Errorf("delete ticket %d: %w", id, err)
	}
	return nil
}

// CopyTicket duplicates one ticket and applies patch as Odoo copy defaults. An empty patch makes
// an unmodified copy. Archived source tickets are supported. The write is attempted once.
func (c *Client) CopyTicket(ctx context.Context, id int, patch UpdateTicketRequest) (int, error) {
	var (
		err    error
		copyID int
		raw    jsontext.Value
		ids    []int
		vals   map[string]any
	)

	if id <= 0 {
		return 0, invalidArgument("ticket ID must be positive")
	}
	if err = validateUpdateTicket(patch); err != nil {
		return 0, err
	}
	vals = map[string]any{}
	if patch != (UpdateTicketRequest{}) {
		if vals, err = patchValues(patch); err != nil {
			return 0, err
		}
	}
	if err = c.requireTicket(ctx, id); err != nil {
		return 0, err
	}
	err = c.Call(ctx, "helpdesk.ticket", "copy", map[string]any{
		"ids":     []int{id},
		"default": vals,
	}, &raw)
	if err != nil {
		return 0, fmt.Errorf("copy ticket %d: %w", id, err)
	}
	// Odoo 19 returns the new record as a one-element ID list; accept a bare ID as well.
	if jsonv2.Unmarshal(raw, &copyID) != nil {
		if jsonv2.Unmarshal(raw, &ids) != nil || len(ids) != 1 {
			return 0, fmt.Errorf("%w: expected one ID from ticket copy", odoo.ErrUnexpectedResponse)
		}
		copyID = ids[0]
	}
	if copyID <= 0 {
		return 0, fmt.Errorf("%w: expected a positive ID from ticket copy", odoo.ErrUnexpectedResponse)
	}
	return copyID, nil
}
