package odoo

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"
)

// Pricelist is a product.pricelist record. Searches omit archived pricelists.
type Pricelist struct {
	ID       int      `json:"id"`
	Name     string   `json:"name"`
	Currency Many2One `json:"currency_id"`
	Active   bool     `json:"active"`
}

// PricelistItem is one product.pricelist.item rule. Unset relations have a zero ID.
type PricelistItem struct {
	ID           int            `json:"id"`
	Pricelist    Many2One       `json:"pricelist_id"`
	AppliedOn    string         `json:"applied_on"`
	ProductTmpl  Many2One       `json:"product_tmpl_id"`
	Product      Many2One       `json:"product_id"`
	ComputePrice string         `json:"compute_price"`
	FixedPrice   float64        `json:"fixed_price"`
	MinQuantity  float64        `json:"min_quantity"`
	DateStart    OptionalString `json:"date_start"`
	DateEnd      OptionalString `json:"date_end"`
}

// Odoo values for PricelistItem.AppliedOn.
const (
	PricelistAppliedGlobal   = "3_global"
	PricelistAppliedCategory = "2_product_category"
	PricelistAppliedProduct  = "1_product"
	PricelistAppliedVariant  = "0_product_variant"
)

// Odoo values for PricelistItem.ComputePrice.
const (
	PricelistComputeFixed      = "fixed"
	PricelistComputePercentage = "percentage"
	PricelistComputeFormula    = "formula"
)

var (
	pricelistFields     = []string{"id", "name", "currency_id", "active"}
	pricelistItemFields = []string{
		"id", "pricelist_id", "applied_on", "product_tmpl_id", "product_id", "compute_price",
		"fixed_price", "min_quantity", "date_start", "date_end",
	}
)

// PricelistQuery filters and paginates pricelists. Limit defaults to 100 and Order to "id".
type PricelistQuery struct {
	Domain []any
	Limit  int
	Offset int
	Order  string
}

// PricelistItemQuery filters and paginates rules. Limit defaults to 100 and Order to "id".
type PricelistItemQuery struct {
	Domain []any
	Limit  int
	Offset int
	Order  string
}

func searchPricelistRecords[T any](
	ctx context.Context, c *Client, model string, fields []string, domain []any, limit, offset int, order string,
) ([]T, error) {
	var (
		rows []T
		err  error
	)
	if limit < 0 || offset < 0 {
		return nil, invalidArgument("limit and offset must not be negative")
	}
	if limit == 0 {
		limit = 100
	}
	if order == "" {
		order = "id"
	}
	if domain == nil {
		domain = []any{}
	}
	rows = make([]T, 0)
	err = c.CallRead(ctx, model, "search_read", map[string]any{
		"domain": domain, "fields": fields, "limit": limit, "offset": offset, "order": order,
	}, &rows)
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// SearchPricelists returns product.pricelist records matching query.
func (c *Client) SearchPricelists(ctx context.Context, query PricelistQuery) ([]Pricelist, error) {
	var (
		lists []Pricelist
		err   error
	)
	lists, err = searchPricelistRecords[Pricelist](
		ctx, c, "product.pricelist", pricelistFields, query.Domain, query.Limit, query.Offset, query.Order,
	)
	if err != nil {
		return nil, fmt.Errorf("search pricelists: %w", err)
	}
	return lists, nil
}

// GetPricelist retrieves one visible pricelist by ID, or ErrPricelistNotFound.
func (c *Client) GetPricelist(ctx context.Context, id int) (*Pricelist, error) {
	var (
		lists []Pricelist
		err   error
	)
	if id <= 0 {
		return nil, invalidArgument("pricelist ID must be positive")
	}
	lists, err = c.SearchPricelists(ctx, PricelistQuery{Domain: []any{[]any{"id", "=", id}}, Limit: 1})
	if err != nil {
		return nil, err
	}
	if len(lists) == 0 {
		return nil, fmt.Errorf("%w: %d", ErrPricelistNotFound, id)
	}
	return &lists[0], nil
}

// SearchPricelistItems returns all kinds of product.pricelist.item rules matching query.
func (c *Client) SearchPricelistItems(ctx context.Context, query PricelistItemQuery) ([]PricelistItem, error) {
	var (
		items []PricelistItem
		err   error
	)
	items, err = searchPricelistRecords[PricelistItem](
		ctx, c, "product.pricelist.item", pricelistItemFields, query.Domain, query.Limit, query.Offset, query.Order,
	)
	if err != nil {
		return nil, fmt.Errorf("search pricelist items: %w", err)
	}
	return items, nil
}

// CountPricelistItems counts rules matching domain; nil matches all rules.
func (c *Client) CountPricelistItems(ctx context.Context, domain []any) (int, error) {
	return c.count(ctx, "product.pricelist.item", domain)
}

// PartnerPricelists returns each visible partner's property_product_pricelist keyed by partner ID.
// An unset relation is a zero Many2One. IDs absent from the result were not visible to the API user.
func (c *Client) PartnerPricelists(ctx context.Context, ids []int) (map[int]Many2One, error) {
	var (
		out  = make(map[int]Many2One, len(ids))
		rows []struct {
			ID        int      `json:"id"`
			Pricelist Many2One `json:"property_product_pricelist"`
		}
		err error
	)
	for _, id := range ids {
		if id <= 0 {
			return nil, invalidArgument("partner ID must be positive")
		}
	}
	if len(ids) == 0 {
		return out, nil
	}
	rows = make([]struct {
		ID        int      `json:"id"`
		Pricelist Many2One `json:"property_product_pricelist"`
	}, 0)
	err = c.CallRead(ctx, "res.partner", "search_read", map[string]any{
		"domain": []any{[]any{"id", "in", ids}},
		"fields": []string{"id", "property_product_pricelist"},
		"limit":  len(ids),
	}, &rows)
	if err != nil {
		return nil, fmt.Errorf("read partner pricelists: %w", err)
	}
	for _, row := range rows {
		out[row.ID] = row.Pricelist
	}
	return out, nil
}

// CreatePricelistRequest describes a new price list. CurrencyID zero uses Odoo's company currency.
type CreatePricelistRequest struct {
	Name       string `json:"name"`
	CurrencyID int    `json:"currency_id,omitzero"`
}

// CreatePricelist creates a product.pricelist and returns its ID. It does not retry.
// A transport error can leave the outcome unknown; check Odoo before resubmitting.
func (c *Client) CreatePricelist(ctx context.Context, input CreatePricelistRequest) (int, error) {
	if strings.TrimSpace(input.Name) == "" {
		return 0, invalidArgument("name must not be empty")
	}
	if input.CurrencyID < 0 {
		return 0, invalidArgument("currency_id must not be negative")
	}
	return c.createPricelistRecord(ctx, "product.pricelist", input)
}

// CreateFixedPricelistItemRequest creates a fixed price for one product template or variant.
// Exactly one of ProductTemplateID or ProductID must be positive. Dates use Odoo's UTC
// "YYYY-MM-DD HH:MM:SS" representation; empty dates leave the rule open-ended.
type CreateFixedPricelistItemRequest struct {
	PricelistID       int     `json:"pricelist_id"`
	ProductTemplateID int     `json:"product_tmpl_id,omitzero"`
	ProductID         int     `json:"product_id,omitzero"`
	FixedPrice        float64 `json:"fixed_price"`
	MinQuantity       float64 `json:"min_quantity,omitzero"`
	DateStart         string  `json:"date_start,omitempty"`
	DateEnd           string  `json:"date_end,omitempty"`
}

// CreateFixedPricelistItem creates one fixed price rule. It does not retry.
// A transport error can leave the outcome unknown; check Odoo before resubmitting.
func (c *Client) CreateFixedPricelistItem(ctx context.Context, input CreateFixedPricelistItemRequest) (int, error) {
	var (
		appliedOn string
		start     time.Time
		end       time.Time
		err       error
		values    struct {
			CreateFixedPricelistItemRequest
			AppliedOn    string `json:"applied_on"`
			ComputePrice string `json:"compute_price"`
		}
	)
	if input.PricelistID <= 0 {
		return 0, invalidArgument("pricelist_id must be positive")
	}
	if input.ProductTemplateID < 0 || input.ProductID < 0 ||
		(input.ProductTemplateID == 0) == (input.ProductID == 0) {
		return 0, invalidArgument("exactly one product_tmpl_id or product_id must be positive")
	}
	if !validNonnegativeFinite(input.FixedPrice) || !validNonnegativeFinite(input.MinQuantity) {
		return 0, invalidArgument("fixed_price and min_quantity must be finite and nonnegative")
	}
	if input.DateStart != "" {
		if start, err = time.Parse("2006-01-02 15:04:05", input.DateStart); err != nil {
			return 0, invalidArgument("date_start must be YYYY-MM-DD HH:MM:SS")
		}
	}
	if input.DateEnd != "" {
		if end, err = time.Parse("2006-01-02 15:04:05", input.DateEnd); err != nil {
			return 0, invalidArgument("date_end must be YYYY-MM-DD HH:MM:SS")
		}
	}
	if !start.IsZero() && !end.IsZero() && start.After(end) {
		return 0, invalidArgument("date_start must not be after date_end")
	}
	appliedOn = PricelistAppliedProduct
	if input.ProductID != 0 {
		appliedOn = PricelistAppliedVariant
	}
	values.CreateFixedPricelistItemRequest = input
	values.AppliedOn = appliedOn
	values.ComputePrice = PricelistComputeFixed
	return c.createPricelistRecord(ctx, "product.pricelist.item", values)
}

func validNonnegativeFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0
}

func (c *Client) createPricelistRecord(ctx context.Context, model string, input any) (int, error) {
	var (
		ids []int
		err error
	)
	err = c.Call(ctx, model, "create", map[string]any{"vals_list": []any{input}}, &ids)
	if err != nil {
		return 0, fmt.Errorf("create %s: %w", model, err)
	}
	if len(ids) != 1 || ids[0] <= 0 {
		return 0, fmt.Errorf("%w: expected one positive ID from create %s", ErrUnexpectedResponse, model)
	}
	return ids[0], nil
}
