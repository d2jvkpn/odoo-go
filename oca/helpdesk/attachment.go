package helpdesk

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	odoo "github.com/d2jvkpn/odoo-go"
)

// TicketAttachment describes an ir.attachment linked directly to a helpdesk ticket. It omits
// public-link and access-token fields so listing attachments does not expose sharing credentials.
type TicketAttachment struct {
	ID         int                 `json:"id"`
	Name       string              `json:"name"`
	MimeType   odoo.OptionalString `json:"mimetype"`
	Size       int                 `json:"file_size"`
	Checksum   odoo.OptionalString `json:"checksum"`
	CreateDate odoo.OptionalString `json:"create_date"`
	WriteDate  odoo.OptionalString `json:"write_date"`
}

type attachmentData []byte

func (data *attachmentData) UnmarshalJSON(raw []byte) error {
	var (
		decoded []byte
		err     error
		value   string
	)

	raw = bytes.TrimSpace(raw)
	if bytes.Equal(raw, []byte("false")) || bytes.Equal(raw, []byte("null")) {
		*data = nil
		return nil
	}
	if err = json.Unmarshal(raw, &value); err != nil {
		return fmt.Errorf("decode attachment data value: %w", err)
	}
	if decoded, err = base64.StdEncoding.DecodeString(value); err != nil {
		return fmt.Errorf("decode attachment base64: %w", err)
	}
	*data = decoded
	return nil
}

var attachmentFields = []string{
	"id", "name", "mimetype", "file_size", "checksum", "create_date", "write_date",
}

// MaxAttachmentSize is the largest upload accepted. The file is base64-encoded into a single JSON
// request, so memory use is a multiple of its size.
const MaxAttachmentSize = 50 << 20

// UploadTicketAttachment creates a binary ir.attachment linked directly to a ticket and returns
// its ID. The write is attempted once because retrying could create a duplicate attachment.
func (c *Client) UploadTicketAttachment(
	ctx context.Context,
	ticketID int,
	name, mimetype string,
	data []byte,
) (int, error) {
	var (
		err error
		ids []int
	)

	if ticketID <= 0 {
		return 0, invalidArgument("ticket ID must be positive")
	}
	if strings.TrimSpace(name) == "" {
		return 0, invalidArgument("attachment name must not be empty")
	}
	if strings.TrimSpace(mimetype) == "" {
		return 0, invalidArgument("attachment mimetype must not be empty")
	}
	if len(data) > MaxAttachmentSize {
		return 0, invalidArgument("attachment exceeds %d bytes", MaxAttachmentSize)
	}
	if err = c.requireTicket(ctx, ticketID); err != nil {
		return 0, err
	}
	err = c.Call(ctx, "ir.attachment", "create", map[string]any{
		"vals_list": []any{map[string]any{
			"name":      name,
			"mimetype":  mimetype,
			"datas":     base64.StdEncoding.EncodeToString(data),
			"type":      "binary",
			"res_model": "helpdesk.ticket",
			"res_id":    ticketID,
		}},
	}, &ids)
	if err != nil {
		return 0, fmt.Errorf("upload attachment to ticket %d: %w", ticketID, err)
	}
	if len(ids) != 1 || ids[0] <= 0 {
		return 0, fmt.Errorf("%w: expected one positive attachment ID", odoo.ErrUnexpectedResponse)
	}
	return ids[0], nil
}

// ListTicketAttachments returns metadata for attachments linked directly to a ticket, oldest
// first. Binary data and sharing credentials are not requested.
func (c *Client) ListTicketAttachments(ctx context.Context, ticketID int) ([]TicketAttachment, error) {
	var (
		attachments []TicketAttachment
		err         error
	)

	if ticketID <= 0 {
		return nil, invalidArgument("ticket ID must be positive")
	}
	if err = c.requireTicket(ctx, ticketID); err != nil {
		return nil, err
	}
	attachments = make([]TicketAttachment, 0)
	err = c.CallRead(ctx, "ir.attachment", "search_read", map[string]any{
		"domain": []any{
			[]any{"res_model", "=", "helpdesk.ticket"},
			[]any{"res_id", "=", ticketID},
		},
		"fields": attachmentFields,
		"order":  "id asc",
	}, &attachments)
	if err != nil {
		return nil, fmt.Errorf("list ticket %d attachments: %w", ticketID, err)
	}
	return attachments, nil
}

// DownloadTicketAttachment returns decoded binary content for an attachment owned by the supplied
// ticket. An Odoo false datas value represents empty content.
func (c *Client) DownloadTicketAttachment(
	ctx context.Context,
	ticketID, attachmentID int,
) ([]byte, error) {
	var (
		err  error
		rows []struct {
			ID   int            `json:"id"`
			Data attachmentData `json:"datas"`
		}
	)

	if ticketID <= 0 {
		return nil, invalidArgument("ticket ID must be positive")
	}
	if attachmentID <= 0 {
		return nil, invalidArgument("attachment ID must be positive")
	}
	if err = c.requireTicket(ctx, ticketID); err != nil {
		return nil, err
	}
	err = c.CallRead(ctx, "ir.attachment", "search_read", map[string]any{
		"domain": attachmentDomain(ticketID, attachmentID),
		"fields": []string{"id", "datas"},
		"limit":  1,
		"context": map[string]any{
			"bin_size": false,
		},
	}, &rows)
	if err != nil {
		return nil, fmt.Errorf("download ticket %d attachment %d: %w", ticketID, attachmentID, err)
	}
	if len(rows) != 1 || rows[0].ID != attachmentID {
		return nil, fmt.Errorf("attachment %d does not belong to ticket %d", attachmentID, ticketID)
	}
	return []byte(rows[0].Data), nil
}

// DeleteTicketAttachment permanently removes an attachment after verifying that it belongs to the
// supplied ticket. The unlink write is attempted once.
func (c *Client) DeleteTicketAttachment(ctx context.Context, ticketID, attachmentID int) error {
	var (
		deleted bool
		err     error
		matches bool
	)

	if ticketID <= 0 {
		return invalidArgument("ticket ID must be positive")
	}
	if attachmentID <= 0 {
		return invalidArgument("attachment ID must be positive")
	}
	if err = c.requireTicket(ctx, ticketID); err != nil {
		return err
	}
	if matches, err = c.ticketAttachmentExists(ctx, ticketID, attachmentID); err != nil {
		return err
	}
	if !matches {
		return fmt.Errorf("attachment %d does not belong to ticket %d", attachmentID, ticketID)
	}
	if err = c.Call(ctx, "ir.attachment", "unlink", map[string]any{
		"ids": []int{attachmentID},
	}, &deleted); err != nil {
		return fmt.Errorf("delete ticket %d attachment %d: %w", ticketID, attachmentID, err)
	}
	if !deleted {
		return fmt.Errorf("%w: attachment unlink returned false", odoo.ErrUnexpectedResponse)
	}
	return nil
}

func attachmentDomain(ticketID, attachmentID int) []any {
	return []any{
		[]any{"id", "=", attachmentID},
		[]any{"res_model", "=", "helpdesk.ticket"},
		[]any{"res_id", "=", ticketID},
	}
}

func (c *Client) ticketAttachmentExists(ctx context.Context, ticketID, attachmentID int) (bool, error) {
	var (
		err  error
		rows []struct {
			ID int `json:"id"`
		}
	)

	err = c.CallRead(ctx, "ir.attachment", "search_read", map[string]any{
		"domain": attachmentDomain(ticketID, attachmentID),
		"fields": []string{"id"},
		"limit":  1,
	}, &rows)
	if err != nil {
		return false, fmt.Errorf("check ticket %d attachment %d: %w", ticketID, attachmentID, err)
	}
	return len(rows) == 1 && rows[0].ID == attachmentID, nil
}
