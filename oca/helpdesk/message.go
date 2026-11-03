package helpdesk

import (
	"context"
	"fmt"
	"strings"

	odoo "github.com/d2jvkpn/odoo-go"
)

// TicketMessage is a mail.message in a ticket's chatter thread. Body is HTML. Author is the
// res.partner who sent it; EmailFrom is the raw From header (empty for internal/system messages).
// MessageType is Odoo's message_type: "comment" (customer-visible), "note" (internal), or
// "notification"/"email_outgoing" (system-generated).
type TicketMessage struct {
	ID          int                 `json:"id"`
	Body        string              `json:"body"`         // HTML content
	Subject     odoo.OptionalString `json:"subject"`      // optional subject
	MessageType string              `json:"message_type"` // comment | note | notification | email_outgoing
	Subtype     odoo.Many2One       `json:"subtype_id"`   // mail.message.subtype
	Author      odoo.Many2One       `json:"author_id"`    // res.partner; zero for system messages
	EmailFrom   odoo.OptionalString `json:"email_from"`   // raw From header
	Date        string              `json:"date"`         // UTC "YYYY-MM-DD HH:MM:SS"
	Parent      odoo.Many2One       `json:"parent_id"`    // parent mail.message; zero when none
}

// messageFields are the mail.message fields requested by ListTicketMessages.
var messageFields = []string{
	"id", "body", "subject", "message_type", "subtype_id", "author_id", "email_from", "date",
	"parent_id",
}

// ListTicketMessages returns the ticket's chatter thread, oldest first. It reads mail.message
// records whose model is helpdesk.ticket and res_id is id. It fails with ErrTicketNotFound when
// the ticket does not exist.
func (c *Client) ListTicketMessages(ctx context.Context, id int) ([]TicketMessage, error) {
	var (
		err      error
		messages []TicketMessage
	)

	if id <= 0 {
		return nil, invalidArgument("ticket ID must be positive")
	}
	if _, err = c.GetTicket(ctx, id); err != nil {
		return nil, err
	}
	messages = make([]TicketMessage, 0)
	err = c.CallRead(ctx, "mail.message", "search_read", map[string]any{
		"domain": []any{[]any{"model", "=", "helpdesk.ticket"}, []any{"res_id", "=", id}},
		"fields": messageFields,
		"order":  "id asc",
	}, &messages)
	if err != nil {
		return nil, fmt.Errorf("list ticket %d messages: %w", id, err)
	}
	return messages, nil
}

// PostTicketMessage adds a message to the ticket's thread. Body is HTML. When internal is true the
// message is an internal note (subtype mail.mt_note) invisible to the customer; otherwise it is a
// customer-visible comment (mail.mt_comment). It does not retry; a transport error leaves the
// write outcome unknown.
func (c *Client) PostTicketMessage(ctx context.Context, id int, body string, internal bool) error {
	var (
		err    error
		exists bool
	)

	if id <= 0 {
		return invalidArgument("ticket ID must be positive")
	}
	if strings.TrimSpace(body) == "" {
		return invalidArgument("body must not be empty")
	}
	if exists, err = c.ticketExists(ctx, id); err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("%w: %d", ErrTicketNotFound, id)
	}
	subtype := "mail.mt_comment"
	if internal {
		subtype = "mail.mt_note"
	}
	err = c.Call(ctx, "helpdesk.ticket", "message_post", map[string]any{
		"ids":           []int{id},
		"body":          body,
		"subtype_xmlid": subtype,
	}, nil)
	if err != nil {
		return fmt.Errorf("post message on ticket %d: %w", id, err)
	}
	return nil
}
