package odoo

import (
	"context"
	"fmt"
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
