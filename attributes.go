package odoo

import (
	"context"
	"fmt"
)

// maxAttributeValueIDs bounds one search_read of attribute values.
const maxAttributeValueIDs = 200

// ProductAttributeValue is one attribute choice of a template, such as "Weight: 500g". A variant
// lists its choices in Product.ValueIDs.
type ProductAttributeValue struct {
	ID         int      `json:"id"`           // product.template.attribute.value ID
	Attribute  Many2One `json:"attribute_id"` // the attribute, for example "Weight"
	Value      string   `json:"name"`         // the chosen value, for example "500g"
	PriceExtra float64  `json:"price_extra"`  // extra price of choosing this value
}

// ProductAttributeValues returns the attribute values with the given IDs (Product.ValueIDs), keyed
// by ID. Unknown IDs are absent from the map. It reads at most 200 IDs per request internally, so
// any number of IDs is accepted; duplicates are read once.
func (c *Client) ProductAttributeValues(ctx context.Context, ids []int) (map[int]ProductAttributeValue, error) {
	var (
		out   = make(map[int]ProductAttributeValue, len(ids))
		seen  = make(map[int]bool, len(ids))
		uniq  = make([]int, 0, len(ids))
		rows  []ProductAttributeValue
		row   ProductAttributeValue
		id    int
		start int
		end   int
		err   error
	)

	for _, id = range ids {
		if id <= 0 {
			return nil, invalidArgument("attribute value ID must be positive")
		}
		if !seen[id] {
			seen[id] = true
			uniq = append(uniq, id)
		}
	}
	for start = 0; start < len(uniq); start += maxAttributeValueIDs {
		end = min(start+maxAttributeValueIDs, len(uniq))
		rows = nil
		err = c.CallRead(ctx, "product.template.attribute.value", "search_read", map[string]any{
			"domain":  []any{[]any{"id", "in", uniq[start:end]}},
			"fields":  []string{"id", "attribute_id", "name", "price_extra"},
			"limit":   end - start,
			"order":   "id",
			"context": map[string]any{"active_test": false},
		}, &rows)
		if err != nil {
			return nil, fmt.Errorf("read product attribute values: %w", err)
		}
		for _, row = range rows {
			out[row.ID] = row
		}
	}
	return out, nil
}
