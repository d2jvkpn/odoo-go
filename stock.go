package odoo

import (
	"context"
	"fmt"
	"math"
)

// maxStockIDs bounds one GetProductStock request.
const maxStockIDs = 200

// ProductStock reports on-hand quantities of one product.product variant in its default unit of
// measure. QtyAvailable is on hand, VirtualAvailable includes incoming and outgoing moves, and FreeQty
// is on hand minus reservations. Requires Odoo's stock module.
type ProductStock struct {
	ID               int     `json:"id"`
	QtyAvailable     float64 `json:"qty_available"`
	VirtualAvailable float64 `json:"virtual_available"`
	FreeQty          float64 `json:"free_qty"`
}

// GetProductStock returns stock for the given product.product IDs in one request, at most 200 IDs. IDs
// that do not exist are omitted from the result rather than reported as errors; archived products are
// included. An empty list returns an empty result without a request.
func (c *Client) GetProductStock(ctx context.Context, ids []int) ([]ProductStock, error) {
	var (
		err   error
		stock []ProductStock
	)

	if len(ids) > maxStockIDs {
		return nil, invalidArgument("at most %d product IDs per request", maxStockIDs)
	}
	for _, id := range ids {
		if id <= 0 {
			return nil, invalidArgument("product ID must be positive")
		}
	}
	stock = make([]ProductStock, 0, len(ids))
	if len(ids) == 0 {
		return stock, nil
	}
	err = c.CallRead(ctx, "product.product", "search_read", map[string]any{
		"domain":  []any{[]any{"id", "in", ids}},
		"fields":  []string{"id", "qty_available", "virtual_available", "free_qty"},
		"order":   "id",
		"context": map[string]any{"active_test": false},
	}, &stock)
	if err != nil {
		return nil, fmt.Errorf("get product stock: %w", err)
	}
	return stock, nil
}

// StockLocation is an internal stock.location, for example a warehouse's main stock location.
type StockLocation struct {
	ID   int    `json:"id"`
	Name string `json:"complete_name"`
}

// ListStockLocations returns the internal stock locations, the valid targets for SetProductStock.
func (c *Client) ListStockLocations(ctx context.Context) ([]StockLocation, error) {
	var (
		err       error
		locations []StockLocation
	)

	locations = make([]StockLocation, 0)
	err = c.CallRead(ctx, "stock.location", "search_read", map[string]any{
		"domain": []any{[]any{"usage", "=", "internal"}},
		"fields": []string{"id", "complete_name"},
		"order":  "id",
	}, &locations)
	if err != nil {
		return nil, fmt.Errorf("list stock locations: %w", err)
	}
	return locations, nil
}

// SetProductStock sets the on-hand quantity of a product at a location to qty, replacing whatever was
// counted there (it is an inventory adjustment, not an increment). qty must be finite and nonnegative;
// zero means sold out. The product must track inventory (IsStorable), otherwise ErrProductNotStorable is
// returned and nothing is written; an unknown product returns ErrProductNotFound.
//
// It makes two writes, a stock.quant create and action_apply_inventory, and never retries either. If the
// second fails, the first has left a pending count on the quant named in the error: it changes no stock
// until applied, and a later SetProductStock for the same product and location overwrites it. After a
// transport error the outcome is unknown, so read the stock with GetProductStock before resending.
func (c *Client) SetProductStock(ctx context.Context, productID, locationID int, qty float64) error {
	var (
		err      error
		products []struct {
			IsStorable bool `json:"is_storable"`
		}
		quantIDs []int
		adjust   = map[string]any{"inventory_mode": true}
	)

	if productID <= 0 || locationID <= 0 {
		return invalidArgument("product and location IDs must be positive")
	}
	if math.IsNaN(qty) || math.IsInf(qty, 0) || qty < 0 {
		return invalidArgument("quantity must be finite and nonnegative")
	}
	err = c.CallRead(ctx, "product.product", "search_read", map[string]any{
		"domain":  []any{[]any{"id", "=", productID}},
		"fields":  []string{"is_storable"},
		"limit":   1,
		"context": map[string]any{"active_test": false},
	}, &products)
	if err != nil {
		return fmt.Errorf("read product %d: %w", productID, err)
	}
	if len(products) == 0 {
		return fmt.Errorf("%w: %d", ErrProductNotFound, productID)
	}
	if !products[0].IsStorable {
		return fmt.Errorf("%w: %d", ErrProductNotStorable, productID)
	}
	err = c.Call(ctx, "stock.quant", "create", map[string]any{
		"vals_list": []any{map[string]any{
			"product_id": productID, "location_id": locationID, "inventory_quantity": qty,
		}},
		"context": adjust,
	}, &quantIDs)
	if err != nil {
		return fmt.Errorf("set stock of product %d: %w", productID, err)
	}
	if len(quantIDs) != 1 || quantIDs[0] <= 0 {
		return fmt.Errorf("%w: expected one positive quant ID from create", ErrUnexpectedResponse)
	}
	err = c.Call(ctx, "stock.quant", "action_apply_inventory", map[string]any{
		"ids": quantIDs, "context": adjust,
	}, nil)
	if err != nil {
		return fmt.Errorf("apply stock of product %d (pending count left on quant %d): %w",
			productID, quantIDs[0], err)
	}
	return nil
}
