package helpdesk

import (
	"context"
	jsonv2 "encoding/json/v2"
	"fmt"
	"strings"

	odoo "github.com/d2jvkpn/odoo-go"
)

// TicketMessage is a mail.message in a ticket's chatter thread. Body is HTML. Author is the
// res.partner who sent it; EmailFrom is the raw From header (empty for internal/system messages).
// MessageType identifies the source (for example, comment, email or notification).
// Comments and internal notes both use "comment"; Subtype distinguishes their visibility.
type TicketMessage struct {
	ID            int                 `json:"id"`
	Body          string              `json:"body"`         // HTML content
	Subject       odoo.OptionalString `json:"subject"`      // optional subject
	MessageType   string              `json:"message_type"` // comment | email | notification | email_outgoing
	Subtype       odoo.Many2One       `json:"subtype_id"`   // mail.message.subtype
	Author        odoo.Many2One       `json:"author_id"`    // res.partner; zero for system messages
	EmailFrom     odoo.OptionalString `json:"email_from"`   // raw From header
	Date          string              `json:"date"`         // UTC "YYYY-MM-DD HH:MM:SS"
	Parent        odoo.Many2One       `json:"parent_id"`    // parent mail.message; zero when none
	AttachmentIDs []int               `json:"attachment_ids"`
	PartnerIDs    []int               `json:"partner_ids"`
	IsInternal    bool                `json:"is_internal"`
}

// MessageQuery controls ticket message filtering and pagination. A zero Limit returns all
// matching messages, and a nil Domain applies no additional filters.
type MessageQuery struct {
	Limit  int
	Offset int
	Order  string
	Domain []any
}

// PostTicketMessageRequest describes a comment or internal note posted to a ticket. Body may be
// empty only when at least one attachment is included.
type PostTicketMessageRequest struct {
	Body          string `json:"body"`
	Subject       string `json:"subject,omitempty"`
	Internal      bool   `json:"internal,omitzero"`
	ParentID      int    `json:"parent_id,omitzero"`
	PartnerIDs    []int  `json:"partner_ids,omitempty"`
	AttachmentIDs []int  `json:"attachment_ids,omitempty"`
}

// UnmarshalJSON decodes a message, mapping an unset body to the empty string.
func (m *TicketMessage) UnmarshalJSON(data []byte) error {
	type plain TicketMessage
	var aux struct {
		plain
		Body odoo.OptionalString `json:"body"`
	}

	if err := jsonv2.Unmarshal(data, &aux); err != nil {
		return err
	}
	*m = TicketMessage(aux.plain)
	m.Body = string(aux.Body)
	return nil
}

// messageFields are the mail.message fields requested by ListTicketMessages.
var messageFields = []string{
	"id", "body", "subject", "message_type", "subtype_id", "author_id", "email_from", "date",
	"parent_id", "attachment_ids", "partner_ids", "is_internal",
}

// ListTicketMessages returns the ticket's chatter thread, oldest first. It reads mail.message
// records whose model is helpdesk.ticket and res_id is id. It fails with ErrTicketNotFound when
// the ticket does not exist.
func (c *Client) ListTicketMessages(ctx context.Context, id int) ([]TicketMessage, error) {
	return c.SearchTicketMessages(ctx, id, MessageQuery{})
}

// SearchTicketMessages returns messages in a ticket's chatter thread. Limit zero means no limit;
// Order defaults to "id asc". Domain filters are added to the mandatory ticket ownership domain.
func (c *Client) SearchTicketMessages(
	ctx context.Context,
	id int,
	query MessageQuery,
) ([]TicketMessage, error) {
	var (
		domain   []any
		err      error
		messages []TicketMessage
		params   map[string]any
	)

	if id <= 0 {
		return nil, invalidArgument("ticket ID must be positive")
	}
	if query.Limit < 0 {
		return nil, invalidArgument("message limit must not be negative")
	}
	if query.Offset < 0 {
		return nil, invalidArgument("message offset must not be negative")
	}
	if err = c.requireTicket(ctx, id); err != nil {
		return nil, err
	}
	if query.Order == "" {
		query.Order = "id asc"
	}
	domain = make([]any, 0, 2+len(query.Domain))
	domain = append(domain,
		[]any{"model", "=", "helpdesk.ticket"},
		[]any{"res_id", "=", id},
	)
	domain = append(domain, query.Domain...)
	params = map[string]any{
		"domain": domain,
		"fields": messageFields,
		"order":  query.Order,
	}
	if query.Limit > 0 {
		params["limit"] = query.Limit
	}
	if query.Offset > 0 {
		params["offset"] = query.Offset
	}
	messages = make([]TicketMessage, 0)
	err = c.CallRead(ctx, "mail.message", "search_read", params, &messages)
	if err != nil {
		return nil, fmt.Errorf("search ticket %d messages: %w", id, err)
	}
	return messages, nil
}

// PostTicketMessage adds a message to the ticket's thread. Body is HTML when the API user is an
// internal Odoo user; Odoo escapes it for portal users. When internal is true the message is an internal note (subtype mail.mt_note) invisible to the customer; otherwise it is a
// customer-visible comment (mail.mt_comment). It does not retry; a transport error leaves the
// write outcome unknown.
func (c *Client) PostTicketMessage(ctx context.Context, id int, body string, internal bool) error {
	var (
		err    error
		params map[string]any
	)

	params, err = c.prepareTicketMessage(ctx, id, PostTicketMessageRequest{
		Body: body, Internal: internal,
	})
	if err != nil {
		return err
	}
	err = c.Call(ctx, "helpdesk.ticket", "message_post", params, nil)
	if err != nil {
		return fmt.Errorf("post message on ticket %d: %w", id, err)
	}
	return nil
}

// PostTicketMessageWithOptions posts a comment or internal note and returns the created
// mail.message ID. Odoo JSON-2 encodes the returned recordset as an array of IDs.
func (c *Client) PostTicketMessageWithOptions(
	ctx context.Context,
	id int,
	input PostTicketMessageRequest,
) (int, error) {
	var (
		err    error
		ids    []int
		params map[string]any
	)

	params, err = c.prepareTicketMessage(ctx, id, input)
	if err != nil {
		return 0, err
	}
	if err = c.Call(ctx, "helpdesk.ticket", "message_post", params, &ids); err != nil {
		return 0, fmt.Errorf("post message on ticket %d: %w", id, err)
	}
	if len(ids) != 1 || ids[0] <= 0 {
		return 0, fmt.Errorf("%w: expected one positive message ID", odoo.ErrUnexpectedResponse)
	}
	return ids[0], nil
}

func (c *Client) prepareTicketMessage(
	ctx context.Context,
	id int,
	input PostTicketMessageRequest,
) (map[string]any, error) {
	var (
		err     error
		params  map[string]any
		subtype string
	)

	if id <= 0 {
		return nil, invalidArgument("ticket ID must be positive")
	}
	if strings.TrimSpace(input.Body) == "" && len(input.AttachmentIDs) == 0 {
		return nil, invalidArgument("body or attachment_ids must not be empty")
	}
	if input.ParentID < 0 {
		return nil, invalidArgument("parent_id must not be negative")
	}
	if err = validateMessageIDs("partner_ids", input.PartnerIDs); err != nil {
		return nil, err
	}
	if err = validateMessageIDs("attachment_ids", input.AttachmentIDs); err != nil {
		return nil, err
	}
	if err = c.requireTicket(ctx, id); err != nil {
		return nil, err
	}
	if input.ParentID > 0 {
		if err = c.checkMessageOwnership(ctx, id, input.ParentID); err != nil {
			return nil, err
		}
	}
	if len(input.AttachmentIDs) > 0 {
		if err = c.checkMessageAttachmentOwnership(ctx, id, input.AttachmentIDs); err != nil {
			return nil, err
		}
	}
	subtype = "mail.mt_comment"
	if input.Internal {
		subtype = "mail.mt_note"
	}
	params = map[string]any{
		"ids":           []int{id},
		"body":          input.Body,
		"body_is_html":  true,
		"message_type":  "comment",
		"subtype_xmlid": subtype,
	}
	if input.Subject != "" {
		params["subject"] = input.Subject
	}
	if input.ParentID > 0 {
		params["parent_id"] = input.ParentID
	}
	if len(input.PartnerIDs) > 0 {
		params["partner_ids"] = input.PartnerIDs
	}
	if len(input.AttachmentIDs) > 0 {
		params["attachment_ids"] = input.AttachmentIDs
	}
	return params, nil
}

func validateMessageIDs(field string, ids []int) error {
	var seen map[int]struct{}

	seen = make(map[int]struct{}, len(ids))
	for _, id := range ids {
		if id <= 0 {
			return invalidArgument("%s must contain only positive IDs", field)
		}
		if _, ok := seen[id]; ok {
			return invalidArgument("%s must not contain duplicate IDs", field)
		}
		seen[id] = struct{}{}
	}
	return nil
}

func (c *Client) checkMessageOwnership(ctx context.Context, ticketID, messageID int) error {
	var (
		err  error
		rows []struct {
			ID int `json:"id"`
		}
	)

	err = c.CallRead(ctx, "mail.message", "search_read", map[string]any{
		"domain": []any{
			[]any{"id", "=", messageID},
			[]any{"model", "=", "helpdesk.ticket"},
			[]any{"res_id", "=", ticketID},
		},
		"fields": []string{"id"},
		"limit":  1,
	}, &rows)
	if err != nil {
		return fmt.Errorf("check parent message %d: %w", messageID, err)
	}
	if len(rows) != 1 || rows[0].ID != messageID {
		return fmt.Errorf("parent message %d does not belong to ticket %d", messageID, ticketID)
	}
	return nil
}

func (c *Client) checkMessageAttachmentOwnership(ctx context.Context, ticketID int, ids []int) error {
	var (
		err  error
		rows []struct {
			ID int `json:"id"`
		}
		seen map[int]struct{}
	)

	err = c.CallRead(ctx, "ir.attachment", "search_read", map[string]any{
		"domain": []any{
			[]any{"id", "in", ids},
			[]any{"res_model", "=", "helpdesk.ticket"},
			[]any{"res_id", "=", ticketID},
		},
		"fields": []string{"id"},
	}, &rows)
	if err != nil {
		return fmt.Errorf("check ticket %d attachments: %w", ticketID, err)
	}
	seen = make(map[int]struct{}, len(rows))
	for _, row := range rows {
		seen[row.ID] = struct{}{}
	}
	for _, id := range ids {
		if _, ok := seen[id]; !ok {
			return fmt.Errorf("attachment %d does not belong to ticket %d", id, ticketID)
		}
	}
	return nil
}
