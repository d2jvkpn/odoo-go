package odoo

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/go-playground/validator/v10"
)

// SaleOrder contains a quotation or confirmed sales order.
// Dates use Odoo's UTC "YYYY-MM-DD HH:MM:SS" representation.
type SaleOrder struct {
	ID             int             `json:"id"`
	Name           string          `json:"name"`
	Partner        Many2One        `json:"partner_id"`
	DateOrder      string          `json:"date_order"`
	State          string          `json:"state"`
	AmountUntaxed  float64         `json:"amount_untaxed"`
	AmountTax      float64         `json:"amount_tax"`
	AmountTotal    float64         `json:"amount_total"`
	Currency       Many2One        `json:"currency_id"`
	ClientOrderRef OptionalString  `json:"client_order_ref"`
	InvoiceStatus  OptionalString  `json:"invoice_status"`
	DeliveryStatus OptionalString  `json:"delivery_status"`
	Origin         OptionalString  `json:"origin"`
	Note           OptionalString  `json:"note"`
	LineIDs        []int           `json:"order_line"`
	Lines          []SaleOrderLine `json:"lines,omitempty"`
}

// SaleOrderLine may represent a product, a section or a note.
// Sections and notes have no Product and a nonempty DisplayType.
type SaleOrderLine struct {
	ID            int            `json:"id"`
	Product       Many2One       `json:"product_id"`
	Name          string         `json:"name"`
	Quantity      float64        `json:"product_uom_qty"`
	UnitPrice     float64        `json:"price_unit"`
	PriceSubtotal float64        `json:"price_subtotal"`
	DisplayType   OptionalString `json:"display_type"`
}

// SaleOrderQuery accepts Odoo domain expressions. Limit defaults to 100;
// Offset defaults to 0 and Order to "id desc".
type SaleOrderQuery struct {
	Domain []any
	Limit  int
	Offset int
	Order  string
}

// SearchSaleOrders returns order headers and line IDs, without fetching lines.
func (c *Client) SearchSaleOrders(ctx context.Context, query SaleOrderQuery) ([]SaleOrder, error) {
	var (
		err    error
		orders []SaleOrder
	)

	if query.Limit < 0 || query.Offset < 0 {
		return nil, invalidArgument("limit and offset must not be negative")
	}
	if query.Limit == 0 {
		query.Limit = 100
	}
	if query.Order == "" {
		query.Order = "id desc"
	}
	if query.Domain == nil {
		query.Domain = []any{}
	}
	orders = make([]SaleOrder, 0)
	err = c.CallRead(ctx, "sale.order", "search_read", map[string]any{
		"domain": query.Domain,
		"fields": []string{
			"id", "name", "partner_id", "date_order", "state", "amount_untaxed", "amount_tax",
			"amount_total", "currency_id", "client_order_ref", "invoice_status", "delivery_status",
			"origin", "note", "order_line",
		},
		"limit":  query.Limit,
		"offset": query.Offset,
		"order":  query.Order,
	}, &orders)
	if err != nil {
		return nil, fmt.Errorf("search sales orders: %w", err)
	}
	return orders, nil
}

// GetSaleOrder fetches an order and its lines. These are two separate API
// transactions, so concurrent edits can change the order between reads.
func (c *Client) GetSaleOrder(ctx context.Context, id int) (*SaleOrder, error) {
	var (
		err    error
		orders []SaleOrder
		order  *SaleOrder
	)

	if id <= 0 {
		return nil, invalidArgument("sales order ID must be positive")
	}
	orders, err = c.SearchSaleOrders(ctx, SaleOrderQuery{
		Domain: []any{[]any{"id", "=", id}},
		Limit:  1,
	})
	if err != nil {
		return nil, err
	}
	if len(orders) == 0 {
		return nil, fmt.Errorf("%w: %d", ErrSaleOrderNotFound, id)
	}
	order = &orders[0]
	if len(order.LineIDs) != 0 {
		err = c.CallRead(ctx, "sale.order.line", "read", map[string]any{
			"ids": order.LineIDs,
			"fields": []string{
				"id", "product_id", "name", "product_uom_qty", "price_unit", "price_subtotal", "display_type",
			},
		}, &order.Lines)
		if err != nil {
			return nil, fmt.Errorf("read sales order %d lines: %w", id, err)
		}
	}
	return order, nil
}

// CreateSaleOrderRequest describes a draft quotation. PartnerID references
// res.partner. PricelistID zero uses the customer's default pricelist.
// ClientOrderRef is a customer reference, not a unique/idempotency key. Origin is the free-text source
// document, suited to a channel marker such as "web". Note is Odoo's terms and conditions: it is HTML,
// is not escaped, and is printed on the quotation, so do not put internal data in it.
type CreateSaleOrderRequest struct {
	PartnerID      int                   `json:"partner_id"                 validate:"gt=0"`
	PricelistID    int                   `json:"pricelist_id,omitempty"     validate:"gte=0"`
	ClientOrderRef string                `json:"client_order_ref,omitempty"`
	Origin         string                `json:"origin,omitempty"`
	Note           string                `json:"note,omitempty"`
	Lines          []CreateSaleOrderLine `json:"lines"                      validate:"min=1,dive"`
}

// CreateSaleOrderLine references a product.product variant. Quantity uses the
// product's default unit of measure. Nil UnitPrice lets Odoo compute the price;
// a pointer to zero explicitly creates a free line.
type CreateSaleOrderLine struct {
	ProductID int      `json:"product_id"           validate:"gt=0"`
	Quantity  float64  `json:"product_uom_qty"      validate:"gt=0"`
	UnitPrice *float64 `json:"price_unit,omitempty" validate:"omitempty,gte=0"`
}

// validateCreateSaleOrder checks the request shape and numeric bounds. Odoo-side rules are left to the server.
func validateCreateSaleOrder(input CreateSaleOrderRequest) error {
	var (
		err  error
		errs validator.ValidationErrors
		e    validator.FieldError
		i    int
		line CreateSaleOrderLine
	)

	if err = validate.Struct(input); err != nil {
		if !errors.As(err, &errs) {
			return fmt.Errorf("validate order: %w", err)
		}
		e = errs[0]
		switch e.StructField() {
		case "PartnerID":
			return invalidArgument("partner_id must be positive")
		case "PricelistID":
			return invalidArgument("pricelist_id must not be negative")
		case "Lines":
			return invalidArgument("at least one order line is required")
		case "ProductID":
			return invalidArgument("line %s product_id must be positive", lineNumber(e))
		case "Quantity":
			return invalidArgument("line %s quantity must be finite and positive", lineNumber(e))
		default:
			return invalidArgument("line %s price must be finite and nonnegative", lineNumber(e))
		}
	}
	for i, line = range input.Lines {
		if math.IsInf(line.Quantity, 0) {
			return invalidArgument("line %d quantity must be finite and positive", i+1)
		}
		if line.UnitPrice != nil && math.IsInf(*line.UnitPrice, 0) {
			return invalidArgument("line %d price must be finite and nonnegative", i+1)
		}
	}
	return nil
}

// lineNumber extracts the 1-based line number from a namespace such as "CreateSaleOrderRequest.Lines[0].Quantity".
func lineNumber(e validator.FieldError) string {
	var (
		ns         string
		start, end int
		index      int
		err        error
	)

	ns = e.Namespace()
	start = strings.Index(ns, "[")
	end = strings.Index(ns, "]")
	if start < 0 || end < start {
		return "?"
	}
	if index, err = strconv.Atoi(ns[start+1 : end]); err != nil {
		return "?"
	}
	return strconv.Itoa(index + 1)
}

// CreateSaleOrder creates one draft quotation and its lines in one API
// transaction, returning its ID. It does not confirm the order or retry.
// After a transport/decode error the write outcome may be unknown; check Odoo
// before resubmitting, to avoid creating duplicate orders.
func (c *Client) CreateSaleOrder(ctx context.Context, input CreateSaleOrderRequest) (int, error) {
	var (
		err    error
		lines  []any
		values map[string]any
		ids    []int
		params map[string]any
	)

	if err = validateCreateSaleOrder(input); err != nil {
		return 0, err
	}
	lines = make([]any, 0, len(input.Lines))
	for _, line := range input.Lines {
		lines = append(lines, []any{0, 0, line})
	}
	values = map[string]any{"partner_id": input.PartnerID, "order_line": lines}
	if input.PricelistID != 0 {
		values["pricelist_id"] = input.PricelistID
	}
	if input.ClientOrderRef != "" {
		values["client_order_ref"] = input.ClientOrderRef
	}
	if input.Origin != "" {
		values["origin"] = input.Origin
	}
	if input.Note != "" {
		values["note"] = input.Note
	}
	params = map[string]any{"vals_list": []any{values}}
	if err = c.Call(ctx, "sale.order", "create", params, &ids); err != nil {
		return 0, fmt.Errorf("create sales order: %w", err)
	}
	if len(ids) != 1 || ids[0] <= 0 {
		return 0, fmt.Errorf(
			"%w: expected one positive ID from create (order may already exist)", ErrUnexpectedResponse,
		)
	}
	return ids[0], nil
}

// saleOrderState reads the order's state, or ErrSaleOrderNotFound.
func (c *Client) saleOrderState(ctx context.Context, id int) (string, error) {
	var (
		err  error
		rows []struct {
			State string `json:"state"`
		}
	)

	if id <= 0 {
		return "", invalidArgument("sales order ID must be positive")
	}
	err = c.CallRead(ctx, "sale.order", "search_read", map[string]any{
		"domain": []any{[]any{"id", "=", id}},
		"fields": []string{"state"},
		"limit":  1,
	}, &rows)
	if err != nil {
		return "", fmt.Errorf("read sales order %d state: %w", id, err)
	}
	if len(rows) == 0 {
		return "", fmt.Errorf("%w: %d", ErrSaleOrderNotFound, id)
	}
	return rows[0].State, nil
}

// ConfirmSaleOrder turns a draft or sent quotation into a confirmed order (action_confirm). It checks
// the state first and returns ErrInvalidOrderState without writing when the order is not a quotation;
// the gap between that check and the write is accepted and Odoo's own validation is authoritative.
// It does not retry. After a transport error the outcome is unknown: call GetSaleOrder and inspect
// State before trying again.
func (c *Client) ConfirmSaleOrder(ctx context.Context, id int) error {
	var (
		err   error
		state string
	)

	if state, err = c.saleOrderState(ctx, id); err != nil {
		return err
	}
	if state != "draft" && state != "sent" {
		return fmt.Errorf("%w: cannot confirm order %d in state %q", ErrInvalidOrderState, id, state)
	}
	if err = c.Call(ctx, "sale.order", "action_confirm", map[string]any{"ids": []int{id}}, nil); err != nil {
		return fmt.Errorf("confirm sales order %d: %w", id, err)
	}
	return nil
}

// CancelSaleOrder cancels an order that is not already cancelled (action_cancel, with Odoo's
// cancellation wizard suppressed). Odoo may still refuse, for example for delivered or invoiced orders;
// that error passes through as an *HTTPError. It does not retry; see ConfirmSaleOrder for the
// uncertain-outcome rule.
func (c *Client) CancelSaleOrder(ctx context.Context, id int) error {
	var (
		err   error
		state string
	)

	if state, err = c.saleOrderState(ctx, id); err != nil {
		return err
	}
	if state == "cancel" {
		return fmt.Errorf("%w: order %d is already cancelled", ErrInvalidOrderState, id)
	}
	err = c.Call(ctx, "sale.order", "action_cancel", map[string]any{
		"ids":     []int{id},
		"context": map[string]any{"disable_cancel_warning": true},
	}, nil)
	if err != nil {
		return fmt.Errorf("cancel sales order %d: %w", id, err)
	}
	return nil
}

// UpdateSaleOrderRequest changes selected text fields of an existing order. Nil fields are left
// unchanged; a pointer to "" clears the field. Note is HTML and is printed on the quotation.
type UpdateSaleOrderRequest struct {
	ClientOrderRef *string `json:"client_order_ref,omitempty"`
	Origin         *string `json:"origin,omitempty"`
	Note           *string `json:"note,omitempty"`
}

// UpdateSaleOrder changes the customer reference, source document, or note of one order. It returns
// ErrSaleOrderNotFound for an unknown ID and does not retry. Odoo may refuse the change on a locked or
// cancelled order; that error passes through as an *HTTPError.
func (c *Client) UpdateSaleOrder(ctx context.Context, id int, patch UpdateSaleOrderRequest) error {
	return c.updateRecord(ctx, "sale.order", id, patch, ErrSaleOrderNotFound)
}
