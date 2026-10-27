package odoo

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// patchValues encodes a patch struct into Odoo write values. Fields tagged omitempty and left nil are
// dropped, and a zero value for a *_id relation is sent as false; a patch that sets nothing is rejected so no empty write is ever sent.
func patchValues(patch any) (map[string]any, error) {
	var (
		err  error
		data []byte
		vals map[string]any
	)

	if data, err = json.Marshal(patch); err != nil {
		return nil, fmt.Errorf("encode patch: %w", err)
	}
	if err = json.Unmarshal(data, &vals); err != nil {
		return nil, fmt.Errorf("encode patch: %w", err)
	}
	if len(vals) == 0 {
		return nil, invalidArgument("patch must set at least one field")
	}
	for field, value := range vals {
		// A zero relation ID means "clear it". Odoo expects false; sending 0 corrupts parent paths.
		if number, ok := value.(float64); ok && number == 0 && strings.HasSuffix(field, "_id") {
			vals[field] = false
		}
	}
	return vals, nil
}

// recordExists reports whether id exists in model. Archived records count, so they can be unarchived.
func (c *Client) recordExists(ctx context.Context, model string, id int) (bool, error) {
	var (
		err  error
		rows []struct {
			ID int `json:"id"`
		}
	)

	err = c.CallRead(ctx, model, "search_read", map[string]any{
		"domain":  []any{[]any{"id", "=", id}},
		"fields":  []string{"id"},
		"limit":   1,
		"context": map[string]any{"active_test": false},
	}, &rows)
	if err != nil {
		return false, fmt.Errorf("check %s %d: %w", model, id, err)
	}
	return len(rows) == 1, nil
}

// writeRecord writes vals to one record. It does not retry and does not check that the record exists.
func (c *Client) writeRecord(ctx context.Context, model string, id int, vals map[string]any) error {
	var err error

	err = c.Call(ctx, model, "write", map[string]any{"ids": []int{id}, "vals": vals}, nil)
	if err != nil {
		return fmt.Errorf("update %s %d: %w", model, id, err)
	}
	return nil
}

// updateRecord validates the patch, checks that the record exists (Odoo's write succeeds silently on a
// missing ID), then writes it. It does not retry.
func (c *Client) updateRecord(
	ctx context.Context,
	model string,
	id int,
	patch any,
	notFound error,
) error {
	var (
		err    error
		vals   map[string]any
		exists bool
	)

	if id <= 0 {
		return invalidArgument("%s ID must be positive", model)
	}
	if vals, err = patchValues(patch); err != nil {
		return err
	}
	if exists, err = c.recordExists(ctx, model, id); err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("%w: %d", notFound, id)
	}
	return c.writeRecord(ctx, model, id, vals)
}
