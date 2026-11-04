package helpdesk

import (
	"context"
	"fmt"
	"iter"
)

// ticketFields are the core helpdesk.ticket fields requested by SearchTickets.
var ticketFields = []string{
	"id", "name", "number", "description", "partner_id", "partner_name", "partner_email", "stage_id",
	"team_id", "user_id", "priority", "channel_id", "category_id", "tag_ids", "company_id", "active",
	"closed", "unattended", "color", "kanban_state", "sequence", "last_stage_update", "assigned_date",
	"closed_date", "message_ids", "create_date", "write_date",
}

var ticketStageFields = []string{
	"id", "name", "description", "sequence", "active", "unattended", "closed", "close_from_portal",
	"mail_template_id", "fold", "company_id", "team_ids",
}

func ticketQueryFields(query TicketQuery) []string {
	var fields []string

	fields = append([]string{}, ticketFields...)
	if query.IncludeSaleOrders {
		fields = append(fields, "sale_order_ids")
	}
	if query.IncludeProduct {
		fields = append(fields, "product_id")
	}
	return fields
}

// withArchivedContext adds the context that disables Odoo's active filter when query asks for
// archived tickets, so search_read and search_count always agree on visibility.
func withArchivedContext(params map[string]any, query TicketQuery) map[string]any {
	if query.IncludeArchived {
		params["context"] = map[string]any{"active_test": false}
	}
	return params
}

// SearchTickets returns helpdesk.ticket records matching the query. Limit defaults to 100 and
// Order to "id desc"; a nil Domain matches every visible ticket.
func (c *Client) SearchTickets(ctx context.Context, query TicketQuery) ([]Ticket, error) {
	var (
		err     error
		params  map[string]any
		tickets []Ticket
	)

	if query.Limit < 0 || query.Offset < 0 {
		return nil, invalidArgument("limit and offset must not be negative")
	}
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
	params = withArchivedContext(map[string]any{
		"domain": query.Domain,
		"fields": ticketQueryFields(query),
		"limit":  query.Limit,
		"offset": query.Offset,
		"order":  query.Order,
	}, query)
	err = c.CallRead(ctx, "helpdesk.ticket", "search_read", params, &tickets)
	if err != nil {
		return nil, fmt.Errorf("search tickets: %w", err)
	}
	return tickets, nil
}

// GetTicket fetches one ticket by ID, including an archived ticket, or ErrTicketNotFound. It reads
// core helpdesk_mgmt fields only; use GetTicketWithQuery to opt into optional addon fields.
func (c *Client) GetTicket(ctx context.Context, id int) (*Ticket, error) {
	return c.GetTicketWithQuery(ctx, id, TicketQuery{})
}

// GetTicketWithQuery fetches one ticket by ID. IncludeSaleOrders and IncludeProduct opt into fields
// provided by optional OCA addons. Domain, Limit, Offset, Order and IncludeArchived are ignored;
// ID reads always include archived records.
func (c *Client) GetTicketWithQuery(ctx context.Context, id int, query TicketQuery) (*Ticket, error) {
	var (
		err     error
		tickets []Ticket
	)

	if id <= 0 {
		return nil, invalidArgument("ticket ID must be positive")
	}
	query.Domain = []any{[]any{"id", "=", id}}
	query.Limit = 1
	query.Offset = 0
	query.Order = ""
	query.IncludeArchived = true
	tickets, err = c.SearchTickets(ctx, query)
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
	return c.CountTicketsWithQuery(ctx, TicketQuery{Domain: domain})
}

// CountTicketsWithQuery returns how many tickets match query.Domain. IncludeArchived disables
// Odoo's active filter; paging, ordering and optional field flags are ignored.
func (c *Client) CountTicketsWithQuery(ctx context.Context, query TicketQuery) (int, error) {
	var (
		err    error
		count  int
		params map[string]any
	)

	if query.Domain == nil {
		query.Domain = []any{}
	}
	params = withArchivedContext(map[string]any{"domain": query.Domain}, query)
	err = c.CallRead(ctx, "helpdesk.ticket", "search_count", params, &count)
	if err != nil {
		return 0, fmt.Errorf("count tickets: %w", err)
	}
	return count, nil
}

// AllTickets iterates over every ticket matching query. Limit is the page size (default 100) and
// Offset the starting position. The first error ends the iteration. When Order is empty,
// "id desc" or "id asc", pages follow an id cursor so concurrent creates or deletes cannot repeat
// or skip tickets (Offset then only applies to the first page); any other Order pages by offset.
func (c *Client) AllTickets(ctx context.Context, query TicketQuery) iter.Seq2[Ticket, error] {
	if query.Limit < 0 || query.Offset < 0 {
		return func(yield func(Ticket, error) bool) {
			yield(Ticket{}, invalidArgument("limit and offset must not be negative"))
		}
	}
	if query.Limit == 0 {
		query.Limit = 100
	}
	return func(yield func(Ticket, error) bool) {
		var (
			err    error
			page   []Ticket
			ticket Ticket
			offset = query.Offset
			base   = query.Domain
			desc   = query.Order == "" || query.Order == "id desc"
			cursor = query.Order == "" || query.Order == "id desc" || query.Order == "id asc"
		)

		for {
			query.Offset = offset
			page, err = c.SearchTickets(ctx, query)
			if err != nil {
				yield(Ticket{}, err)
				return
			}
			for _, ticket = range page {
				if !yield(ticket, nil) {
					return
				}
			}
			if len(page) < query.Limit {
				return
			}
			if cursor {
				op := ">"
				if desc {
					op = "<"
				}
				query.Domain = append(append([]any{}, base...), []any{"id", op, page[len(page)-1].ID})
				offset = 0
			} else {
				offset += len(page)
			}
		}
	}
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
		"fields": ticketStageFields,
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
		"fields": []string{
			"id", "name", "complete_name", "sequence", "active", "user_id", "user_ids", "category_ids",
			"company_id", "alias_id", "color", "show_in_portal", "parent_id",
		},
		"order": "sequence",
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
