package odoo

import (
	"context"
	"fmt"
	"strings"
)

// PartnerTag is a res.partner.category record. ID is what Partner.CategoryIDs and the category_id
// request fields refer to; DisplayName includes the parent tags, such as "Retail / VIP".
type PartnerTag struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
}

// ListPartnerTags returns every active partner tag, ordered by name, in one request.
func (c *Client) ListPartnerTags(ctx context.Context) ([]PartnerTag, error) {
	var (
		err  error
		tags []PartnerTag
	)

	tags = make([]PartnerTag, 0)
	err = c.CallRead(ctx, "res.partner.category", "search_read", map[string]any{
		"domain": []any{},
		"fields": []string{"id", "name", "display_name"},
		"order":  "name",
	}, &tags)
	if err != nil {
		return nil, fmt.Errorf("list partner tags: %w", err)
	}
	return tags, nil
}

// CreatePartnerTag creates a partner tag and returns its ID. It does not retry; after an error list
// the tags again before resubmitting.
func (c *Client) CreatePartnerTag(ctx context.Context, name string) (int, error) {
	var (
		err error
		ids []int
	)

	if strings.TrimSpace(name) == "" {
		return 0, invalidArgument("name must not be empty")
	}
	err = c.Call(ctx, "res.partner.category", "create", map[string]any{
		"vals_list": []any{map[string]any{"name": name}},
	}, &ids)
	if err != nil {
		return 0, fmt.Errorf("create partner tag: %w", err)
	}
	if len(ids) != 1 || ids[0] <= 0 {
		return 0, fmt.Errorf("%w: expected one positive ID from create", ErrUnexpectedResponse)
	}
	return ids[0], nil
}
