package helpdesk

import (
	"context"
	"fmt"
)

// ticketFields are the helpdesk.ticket fields requested by SearchTickets.
var ticketFields = []string{
	"id", "name", "number", "description", "partner_id", "sale_order_ids", "stage_id",
	"team_id", "user_id", "priority", "channel_id", "category_id", "tag_ids", "product_id",
	"closed", "message_ids", "create_date", "write_date",
}

// SearchTickets returns helpdesk.ticket records matching the query. Limit defaults to 100 and
// Order to "id desc"; a nil Domain matches every visible ticket.
func (c *Client) SearchTickets(ctx context.Context, query TicketQuery) ([]Ticket, error) {
	var (
		err     error
		tickets []Ticket
	)

	if query.Limit == 0 {
		query.Limit = 100
	}
	if query.Order == "" {
		query.Order = "id desc"
	}
	if query.Domain == nil {
		query.Domain = []any{}
	}
	tickets = make([]Ticket, 0)
	err = c.CallRead(ctx, "helpdesk.ticket", "search_read", map[string]any{
		"domain": query.Domain,
		"fields": ticketFields,
		"limit":  query.Limit,
		"offset": query.Offset,
		"order":  query.Order,
	}, &tickets)
	if err != nil {
		return nil, fmt.Errorf("search tickets: %w", err)
	}
	return tickets, nil
}

// GetTicket fetches one ticket by ID, or ErrTicketNotFound.
func (c *Client) GetTicket(ctx context.Context, id int) (*Ticket, error) {
	var (
		err     error
		tickets []Ticket
	)

	if id <= 0 {
		return nil, invalidArgument("ticket ID must be positive")
	}
	tickets, err = c.SearchTickets(ctx, TicketQuery{
		Domain: []any{[]any{"id", "=", id}},
		Limit:  1,
	})
	if err != nil {
		return nil, err
	}
	if len(tickets) == 0 {
		return nil, fmt.Errorf("%w: %d", ErrTicketNotFound, id)
	}
	return &tickets[0], nil
}

// CountTickets returns how many tickets match domain; a nil domain counts every visible ticket.
func (c *Client) CountTickets(ctx context.Context, domain []any) (int, error) {
	var (
		err   error
		count int
	)

	if domain == nil {
		domain = []any{}
	}
	err = c.CallRead(ctx, "helpdesk.ticket", "search_count", map[string]any{
		"domain": domain,
	}, &count)
	if err != nil {
		return 0, fmt.Errorf("count tickets: %w", err)
	}
	return count, nil
}

// ListTicketStages returns every helpdesk.ticket.stage, ordered by sequence.
func (c *Client) ListTicketStages(ctx context.Context) ([]TicketStage, error) {
	var (
		err    error
		stages []TicketStage
	)

	stages = make([]TicketStage, 0)
	err = c.CallRead(ctx, "helpdesk.ticket.stage", "search_read", map[string]any{
		"domain": []any{},
		"fields": []string{"id", "name", "sequence", "closed", "fold"},
		"order":  "sequence",
	}, &stages)
	if err != nil {
		return nil, fmt.Errorf("list ticket stages: %w", err)
	}
	return stages, nil
}

// ListTicketTeams returns every helpdesk.ticket.team, ordered by sequence.
func (c *Client) ListTicketTeams(ctx context.Context) ([]TicketTeam, error) {
	var (
		err   error
		teams []TicketTeam
	)

	teams = make([]TicketTeam, 0)
	err = c.CallRead(ctx, "helpdesk.ticket.team", "search_read", map[string]any{
		"domain": []any{},
		"fields": []string{"id", "name"},
		"order":  "sequence",
	}, &teams)
	if err != nil {
		return nil, fmt.Errorf("list ticket teams: %w", err)
	}
	return teams, nil
}

// ListTicketChannels returns every helpdesk.ticket.channel.
func (c *Client) ListTicketChannels(ctx context.Context) ([]TicketChannel, error) {
	var (
		err      error
		channels []TicketChannel
	)

	channels = make([]TicketChannel, 0)
	err = c.CallRead(ctx, "helpdesk.ticket.channel", "search_read", map[string]any{
		"domain": []any{},
		"fields": []string{"id", "name"},
	}, &channels)
	if err != nil {
		return nil, fmt.Errorf("list ticket channels: %w", err)
	}
	return channels, nil
}
