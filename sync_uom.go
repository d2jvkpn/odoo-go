package odoo

import (
	"context"
	"math"
	"strings"
)

// SyncUoM is an Odoo 19 unit of measure and its conversion relative to another unit.
// A unit without RelativeTo is a reference unit and has RelativeFactor 1.
type SyncUoM struct {
	ID             int      `json:"id"`
	Name           string   `json:"name"`
	RelativeTo     Many2One `json:"relative_uom_id"`
	RelativeFactor float64  `json:"relative_factor"`
	Active         bool     `json:"active"`
}

// ListSyncUoMs returns all units, including archived ones, ordered by ID.
func (c *Client) ListSyncUoMs(ctx context.Context) ([]SyncUoM, error) {
	return readAll[SyncUoM](
		ctx, c, "uom.uom", []string{"id", "name", "relative_uom_id", "relative_factor", "active"}, nil, true,
	)
}

// CreateSyncUoM creates a unit with the given reference and conversion factor.
// Writes are not retried.
func (c *Client) CreateSyncUoM(ctx context.Context, name string, relativeTo int, factor float64) (int, error) {
	var (
		ids []int
		err error
	)

	if strings.TrimSpace(name) == "" || relativeTo < 0 || math.IsNaN(factor) || math.IsInf(factor, 0) || factor <= 0 {
		return 0, invalidArgument("unit name, reference, or factor is invalid")
	}
	if relativeTo == 0 && factor != 1 {
		return 0, invalidArgument("a reference unit must have factor 1")
	}
	values := struct {
		Name           string  `json:"name"`
		RelativeTo     int     `json:"relative_uom_id,omitzero"`
		RelativeFactor float64 `json:"relative_factor"`
	}{name, relativeTo, factor}
	if ids, err = c.createRecords(ctx, "uom.uom", []any{values}); err != nil {
		return 0, err
	}
	return ids[0], nil
}
