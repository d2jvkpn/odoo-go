package odoo

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"math"

	"github.com/go-playground/validator/v10"
)

// SaleOrder contains a quotation or confirmed sales order.
// Dates use Odoo's UTC "YYYY-MM-DD HH:MM:SS" representation.
type SaleOrder struct {
	ID        int      `json:"id"`
	Name      string   `json:"name"`       // order number such as "S00042"
	Partner   Many2One `json:"partner_id"` // customer (res.partner)
	DateOrder string   `json:"date_order"` // UTC, e.g. "2026-10-27 08:30:00"
	// State is Odoo's order state: "draft" (quotation), "sent" (quotation sent), "sale" (confirmed)
	// or "cancel". The set depends on the Odoo version.
	State         string   `json:"state"`
	AmountUntaxed float64  `json:"amount_untaxed"` // total before tax, in Currency
	AmountTax     float64  `json:"amount_tax"`     // tax total, in Currency
	AmountTotal   float64  `json:"amount_total"`   // total including tax, in Currency
	Currency      Many2One `json:"currency_id"`
	// ClientOrderRef is the customer reference, for example "web-1001"; not unique.
	ClientOrderRef OptionalString `json:"client_order_ref"`
	// InvoiceStatus is Odoo's invoicing state: "no", "to invoice", "invoiced" or "upselling".
	InvoiceStatus OptionalString `json:"invoice_status"`
	// DeliveryStatus comes from sale_stock: "pending", "started", "partial" or "full"; empty when
	// nothing is to be delivered.
	DeliveryStatus OptionalString  `json:"delivery_status"`
	Origin         OptionalString  `json:"origin"`          // free-text source document, e.g. "web"
	Note           OptionalString  `json:"note"`            // terms and conditions, HTML, e.g. "<p>Delivery within 3 days</p>"
	LineIDs        []int           `json:"order_line"`      // line IDs; always present
	Lines          []SaleOrderLine `json:"lines,omitempty"` // filled only by GetSaleOrder
	// ShippingAddress and InvoiceAddress reference res.partner records (often child contacts of
	// Partner). They are references, not snapshots: editing that partner changes the order's address.
	ShippingAddress Many2One `json:"partner_shipping_id"`
	InvoiceAddress  Many2One `json:"partner_invoice_id"`
}

// SaleOrderLine may represent a product, a section or a note.
// Sections and notes have no Product and a nonempty DisplayType.
type SaleOrderLine struct {
	ID            int            `json:"id"`
	Product       Many2One       `json:"product_id"`      // product.product; zero for sections and notes
	Name          string         `json:"name"`            // line description
	Quantity      float64        `json:"product_uom_qty"` // in the product's default unit of measure
	UnitPrice     float64        `json:"price_unit"`      // per unit, before tax
	Discount      float64        `json:"discount"`        // percent off the unit price, 0 to 100
	PriceSubtotal float64        `json:"price_subtotal"`  // line total before tax, after the discount
	DisplayType   OptionalString `json:"display_type"`    // "line_section" or "line_note"; empty for products
}

// SaleOrderQuery accepts Odoo domain expressions. Limit defaults to 100;
// Offset defaults to 0 and Order to "id desc".
type SaleOrderQuery struct {
	Domain []any  // Odoo domain, for example []any{[]any{"partner_id", "=", 7}}; nil matches all
	Limit  int    // zero means 100
	Offset int    // records to skip
	Order  string // Odoo order clause; empty means "id desc"
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
			"origin", "note", "order_line", "partner_shipping_id", "partner_invoice_id",
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
				"id", "product_id", "name", "product_uom_qty", "price_unit", "discount", "price_subtotal",
				"display_type",
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
//
// Required: PartnerID (> 0) and at least one line. All other fields are optional.
type CreateSaleOrderRequest struct {
	PartnerID   int `json:"partner_id"                 validate:"gt=0"` // required
	PricelistID int `json:"pricelist_id,omitzero"     validate:"gte=0"`
	// PartnerShippingID and PartnerInvoiceID are optional res.partner IDs (typically child contacts of
	// PartnerID with type delivery/invoice); when zero Odoo derives them from PartnerID.
	PartnerShippingID int                   `json:"partner_shipping_id,omitzero" validate:"gte=0"`
	PartnerInvoiceID  int                   `json:"partner_invoice_id,omitzero"  validate:"gte=0"`
	ClientOrderRef    string                `json:"client_order_ref,omitempty"`                  // e.g. "web-1001"
	Origin            string                `json:"origin,omitempty"`                            // e.g. "web"
	Note              string                `json:"note,omitempty"`                              // HTML, e.g. "<p>Delivery within 3 days</p>"
	Lines             []CreateSaleOrderLine `json:"lines"                      validate:"min=1"` // required
}

// Values of SaleOrderLine.DisplayType and CreateSaleOrderLine.DisplayType for lines that are not products.
const (
	LineSection = "line_section" // a heading that groups the lines below it
	LineNote    = "line_note"    // a free-text remark
)

// CreateSaleOrderLine is one line of a new order. It is a product line, a section or a note.
//
// A product line (DisplayType empty) references a product.product variant: ProductID and Quantity are
// required, Quantity uses the product's default unit of measure and may be negative for a return, and Name optionally overrides the line
// description. Nil UnitPrice lets Odoo compute the price; a pointer to zero explicitly creates a free line, and a negative price gives a credit or price-difference line. Discount is a percentage taken off the unit price; nil leaves Odoo's default of 0.
//
// A section or a note (DisplayType LineSection or LineNote) carries only Name, which must not be empty.
// ProductID, Quantity, UnitPrice and Discount must be left unset. Lines keep the order they are given in.
type CreateSaleOrderLine struct {
	DisplayType string   `json:"display_type,omitempty"`   // "", LineSection or LineNote
	Name        string   `json:"name,omitempty"`           // required for sections and notes
	ProductID   int      `json:"product_id,omitzero"`      // required for product lines
	Quantity    float64  `json:"product_uom_qty,omitzero"` // required for product lines, nonzero; negative for a return
	UnitPrice   *float64 `json:"price_unit,omitzero"`      // optional, may be negative
	Discount    *float64 `json:"discount,omitzero"`        // optional percent off the unit price, 0 to 100
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
		case "PartnerShippingID":
			return invalidArgument("partner_shipping_id must not be negative")
		case "PartnerInvoiceID":
			return invalidArgument("partner_invoice_id must not be negative")
		default:
			return fmt.Errorf("validate order: %w", err)
		}
	}
	for i, line = range input.Lines {
		if err = validateOrderLine(i+1, line); err != nil {
			return err
		}
	}
	return nil
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

// validDiscount reports whether v is a percentage from 0 to 100; NaN fails both comparisons.
func validDiscount(v float64) bool { return v >= 0 && v <= 100 }

func finiteNonZero(v float64) bool { return finite(v) && v != 0 }

// validateOrderLine checks one line, numbered from 1 in messages, according to its DisplayType.
func validateOrderLine(n int, line CreateSaleOrderLine) error {
	switch line.DisplayType {
	case "":
		if line.ProductID <= 0 {
			return invalidArgument("line %d product_id must be positive", n)
		}
		if !finiteNonZero(line.Quantity) {
			return invalidArgument("line %d quantity must be finite and nonzero", n)
		}
		if line.UnitPrice != nil && !finite(*line.UnitPrice) {
			return invalidArgument("line %d price must be finite", n)
		}
		if line.Discount != nil && !validDiscount(*line.Discount) {
			return invalidArgument("line %d discount must be between 0 and 100", n)
		}
	case LineSection, LineNote:
		if line.Name == "" {
			return invalidArgument("line %d %s needs a name", n, line.DisplayType)
		}
		if line.ProductID != 0 || line.Quantity != 0 || line.UnitPrice != nil ||
			line.Discount != nil {
			return invalidArgument("line %d %s must not set product_id, quantity, price or discount", n, line.DisplayType)
		}
	default:
		return invalidArgument("line %d display_type must be empty, %q or %q", n, LineSection, LineNote)
	}
	return nil
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
	if input.PartnerShippingID != 0 {
		values["partner_shipping_id"] = input.PartnerShippingID
	}
	if input.PartnerInvoiceID != 0 {
		values["partner_invoice_id"] = input.PartnerInvoiceID
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
	ClientOrderRef *string `json:"client_order_ref,omitzero"` // customer reference
	Origin         *string `json:"origin,omitzero"`           // source document
	Note           *string `json:"note,omitzero"`             // terms and conditions, HTML
	// Lines adds, edits or removes order lines; nil leaves the lines alone.
	Lines *SaleOrderLineChanges `json:"order_line,omitzero"`
}

// SaleOrderLineChanges edits the lines of an existing order. At least one of the three lists must be
// non-empty. Odoo may refuse line edits once the order is confirmed or locked; that error passes through as
// an *HTTPError.
type SaleOrderLineChanges struct {
	Add    []CreateSaleOrderLine `json:"add,omitempty"`    // new product lines, sections and notes, appended after the existing lines
	Update []UpdateSaleOrderLine `json:"update,omitempty"` // changes to existing lines, by line ID
	Remove []int                 `json:"remove,omitempty"` // IDs of lines to delete
}

// UpdateSaleOrderLine changes one existing line of the order. ID is required and at least one other field
// must be set. A line's product and its section or note type cannot be changed; remove it and add a new one.
// A pointer to zero UnitPrice makes the line free; Quantity may be negative but not zero.
type UpdateSaleOrderLine struct {
	ID        int      `json:"id"`
	Name      *string  `json:"name,omitzero"`            // description, heading or note text; not empty
	Quantity  *float64 `json:"product_uom_qty,omitzero"` // product lines only; finite and nonzero, negative for a return
	UnitPrice *float64 `json:"price_unit,omitzero"`      // product lines only; finite, negative for a credit
	Discount  *float64 `json:"discount,omitzero"`        // product lines only; percent, 0 to 100; a pointer to 0 clears it
}

// MarshalJSON encodes the changes as the Odoo one2many command list for order_line:
// [1, id, values] updates a line, [2, id] deletes it and [0, 0, values] creates one.
func (c SaleOrderLineChanges) MarshalJSON() ([]byte, error) {
	var commands = make([]any, 0, len(c.Update)+len(c.Remove)+len(c.Add))

	for _, line := range c.Update {
		commands = append(commands, []any{1, line.ID, struct {
			Name      *string  `json:"name,omitzero"`
			Quantity  *float64 `json:"product_uom_qty,omitzero"`
			UnitPrice *float64 `json:"price_unit,omitzero"`
			Discount  *float64 `json:"discount,omitzero"`
		}{line.Name, line.Quantity, line.UnitPrice, line.Discount}})
	}
	for _, lineID := range c.Remove {
		commands = append(commands, []any{2, lineID})
	}
	for _, line := range c.Add {
		commands = append(commands, []any{0, 0, line})
	}
	return jsonv2.Marshal(commands)
}

// validate checks the shape of the changes. Whether the line IDs belong to the order is checked against Odoo.
func (c SaleOrderLineChanges) validate() error {
	var (
		err  error
		seen = map[int]bool{}
	)

	if len(c.Add)+len(c.Update)+len(c.Remove) == 0 {
		return invalidArgument("line changes must add, update or remove at least one line")
	}
	for i, line := range c.Add {
		if err = validateOrderLine(i+1, line); err != nil {
			return fmt.Errorf("added %w", err)
		}
	}
	for i, line := range c.Update {
		if line.ID <= 0 {
			return invalidArgument("updated line %d ID must be positive", i+1)
		}
		if seen[line.ID] {
			return invalidArgument("line %d is changed more than once", line.ID)
		}
		seen[line.ID] = true
		if line.Name == nil && line.Quantity == nil && line.UnitPrice == nil && line.Discount == nil {
			return invalidArgument("updated line %d must set at least one field", line.ID)
		}
		if line.Name != nil && *line.Name == "" {
			return invalidArgument("updated line %d name must not be empty", line.ID)
		}
		if line.Quantity != nil && !finiteNonZero(*line.Quantity) {
			return invalidArgument("updated line %d quantity must be finite and nonzero", line.ID)
		}
		if line.UnitPrice != nil && !finite(*line.UnitPrice) {
			return invalidArgument("updated line %d price must be finite", line.ID)
		}
		if line.Discount != nil && !validDiscount(*line.Discount) {
			return invalidArgument("updated line %d discount must be between 0 and 100", line.ID)
		}
	}
	for _, lineID := range c.Remove {
		if lineID <= 0 {
			return invalidArgument("removed line ID must be positive")
		}
		if seen[lineID] {
			return invalidArgument("line %d is changed more than once", lineID)
		}
		seen[lineID] = true
	}
	return nil
}

// UpdateSaleOrder changes the customer reference, source document, note or lines of one order. It returns
// ErrSaleOrderNotFound for an unknown ID and does not retry. When lines are changed, every line ID must
// belong to the order, otherwise ErrInvalidArgument is returned and nothing is written. Odoo may refuse the
// change on a locked or cancelled order; that error passes through as an *HTTPError.
func (c *Client) UpdateSaleOrder(ctx context.Context, id int, patch UpdateSaleOrderRequest) error {
	var (
		err  error
		vals map[string]any
		rows []struct {
			LineIDs []int `json:"order_line"`
		}
		own     = map[int]bool{}
		lineIDs []int
	)

	if patch.Lines == nil {
		return c.updateRecord(ctx, "sale.order", id, patch, ErrSaleOrderNotFound)
	}
	if id <= 0 {
		return invalidArgument("sale.order ID must be positive")
	}
	if err = patch.Lines.validate(); err != nil {
		return err
	}
	if vals, err = patchValues(patch); err != nil {
		return err
	}
	err = c.CallRead(ctx, "sale.order", "search_read", map[string]any{
		"domain": []any{[]any{"id", "=", id}},
		"fields": []string{"order_line"},
		"limit":  1,
	}, &rows)
	if err != nil {
		return fmt.Errorf("read sales order %d lines: %w", id, err)
	}
	if len(rows) == 0 {
		return fmt.Errorf("%w: %d", ErrSaleOrderNotFound, id)
	}
	for _, lineID := range rows[0].LineIDs {
		own[lineID] = true
	}
	for _, line := range patch.Lines.Update {
		lineIDs = append(lineIDs, line.ID)
	}
	lineIDs = append(lineIDs, patch.Lines.Remove...)
	for _, lineID := range lineIDs {
		if !own[lineID] {
			return invalidArgument("line %d does not belong to sales order %d", lineID, id)
		}
	}
	return c.writeRecord(ctx, "sale.order", id, vals)
}
