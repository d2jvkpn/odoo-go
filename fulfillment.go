package odoo

import (
	"context"
	"encoding/json"
	"fmt"
)

// StockMove is one product line of a delivery.
type StockMove struct {
	ID      int      `json:"id"`
	Picking Many2One `json:"picking_id"`
	Product Many2One `json:"product_id"`
	// Demand is the quantity to deliver; Quantity is what is reserved, or delivered once the picking is
	// done. Both are in the move's unit of measure.
	Demand   float64  `json:"product_uom_qty"`
	Quantity float64  `json:"quantity"`
	UoM      Many2One `json:"product_uom"`
	State    string   `json:"state"` // "draft", "waiting", "confirmed", "partially_available", "assigned", "done" or "cancel"
}

// StockPicking is a delivery (stock.picking) created for a sales order. It comes from the sale_stock
// and stock modules.
type StockPicking struct {
	ID   int    `json:"id"`
	Name string `json:"name"` // reference such as "WH/OUT/00003"
	// State is "draft", "waiting", "confirmed", "assigned" (ready), "done" or "cancel".
	State         string         `json:"state"`
	Origin        OptionalString `json:"origin"`         // source document, normally the order name
	ScheduledDate string         `json:"scheduled_date"` // UTC "YYYY-MM-DD HH:MM:SS"
	DateDone      OptionalString `json:"date_done"`      // UTC; empty until the picking is done
	Type          Many2One       `json:"picking_type_id"`
	From          Many2One       `json:"location_id"`
	To            Many2One       `json:"location_dest_id"`
	Moves         []StockMove    `json:"moves"`
}

// Invoice is a customer invoice or credit note (account.move) linked to a sales order, from the account
// module.
type Invoice struct {
	ID   int            `json:"id"`
	Name OptionalString `json:"name"` // number such as "INV/2026/00001"; empty while still a draft
	// Type is "out_invoice" for an invoice and "out_refund" for a credit note.
	Type string `json:"move_type"`
	// State is "draft", "posted" or "cancel". PaymentState is "not_paid", "in_payment", "paid",
	// "partial", "reversed" or "invoicing_legacy".
	State         string         `json:"state"`
	PaymentState  string         `json:"payment_state"`
	InvoiceDate   OptionalString `json:"invoice_date"`     // "YYYY-MM-DD"; empty for a draft
	DueDate       OptionalString `json:"invoice_date_due"` // "YYYY-MM-DD"
	AmountUntaxed float64        `json:"amount_untaxed"`
	AmountTax     float64        `json:"amount_tax"`
	AmountTotal   float64        `json:"amount_total"`
	AmountDue     float64        `json:"amount_residual"` // still unpaid, in Currency
	Currency      Many2One       `json:"currency_id"`
}

// Payment is a payment (account.payment) that settles one or more invoices of a sales order.
type Payment struct {
	ID         int            `json:"id"`
	Name       OptionalString `json:"name"`
	State      string         `json:"state"`        // "draft", "in_process", "paid", "canceled" or "rejected"
	Type       string         `json:"payment_type"` // "inbound" (received) or "outbound" (refunded)
	Amount     float64        `json:"amount"`
	Date       string         `json:"date"` // "YYYY-MM-DD"
	Currency   Many2One       `json:"currency_id"`
	Method     Many2One       `json:"payment_method_line_id"`
	Memo       OptionalString `json:"memo"`
	InvoiceIDs []int          `json:"reconciled_invoice_ids"`
}

// orderRelationIDs returns the IDs in the order's relation field (picking_ids or invoice_ids) or
// ErrSaleOrderNotFound. These reads never retry unless Client.Retry allows it.
func (c *Client) orderRelationIDs(ctx context.Context, orderID int, field string) ([]int, error) {
	var (
		err  error
		rows []map[string]json.RawMessage
		ids  []int
	)

	if orderID <= 0 {
		return nil, invalidArgument("sales order ID must be positive")
	}
	err = c.CallRead(ctx, "sale.order", "read", map[string]any{"ids": []int{orderID}, "fields": []string{field}}, &rows)
	if err != nil {
		return nil, fmt.Errorf("read sales order %d %s: %w", orderID, field, err)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("%w: %d", ErrSaleOrderNotFound, orderID)
	}
	if err = json.Unmarshal(rows[0][field], &ids); err != nil {
		return nil, fmt.Errorf("%w: sales order %d %s: %v", ErrUnexpectedResponse, orderID, field, err)
	}
	return ids, nil
}

// OrderPickings returns the deliveries of a sales order with their product moves, oldest first. An order
// that is not confirmed yet has none. It needs the stock and sale_stock modules and returns
// ErrSaleOrderNotFound for an unknown order. Deliveries and moves are separate reads, so concurrent
// changes can show between them.
func (c *Client) OrderPickings(ctx context.Context, orderID int) ([]StockPicking, error) {
	var (
		err      error
		ids      []int
		pickings = make([]StockPicking, 0)
		moves    []StockMove
		byID     map[int]int
		i        int
		move     StockMove
	)

	if ids, err = c.orderRelationIDs(ctx, orderID, "picking_ids"); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return pickings, nil
	}
	err = c.CallRead(ctx, "stock.picking", "read", map[string]any{
		"ids": ids,
		"fields": []string{
			"id", "name", "state", "origin", "scheduled_date", "date_done", "picking_type_id", "location_id",
			"location_dest_id",
		},
	}, &pickings)
	if err != nil {
		return nil, fmt.Errorf("read sales order %d deliveries: %w", orderID, err)
	}
	err = c.CallRead(ctx, "stock.move", "search_read", map[string]any{
		"domain": []any{[]any{"picking_id", "in", ids}},
		"fields": []string{"id", "picking_id", "product_id", "product_uom_qty", "quantity", "product_uom", "state"},
		"order":  "id",
	}, &moves)
	if err != nil {
		return nil, fmt.Errorf("read sales order %d delivery moves: %w", orderID, err)
	}
	byID = make(map[int]int, len(pickings))
	for i = range pickings {
		pickings[i].Moves = make([]StockMove, 0)
		byID[pickings[i].ID] = i
	}
	for _, move = range moves {
		if i, ok := byID[move.Picking.ID]; ok {
			pickings[i].Moves = append(pickings[i].Moves, move)
		}
	}
	return pickings, nil
}

// OrderInvoices returns the customer invoices and credit notes of a sales order, including drafts and
// cancelled ones. It needs the account module and returns ErrSaleOrderNotFound for an unknown order.
func (c *Client) OrderInvoices(ctx context.Context, orderID int) ([]Invoice, error) {
	var (
		err      error
		ids      []int
		invoices = make([]Invoice, 0)
	)

	if ids, err = c.orderRelationIDs(ctx, orderID, "invoice_ids"); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return invoices, nil
	}
	err = c.CallRead(ctx, "account.move", "read", map[string]any{
		"ids": ids,
		"fields": []string{
			"id", "name", "move_type", "state", "payment_state", "invoice_date", "invoice_date_due",
			"amount_untaxed", "amount_tax", "amount_total", "amount_residual", "currency_id",
		},
	}, &invoices)
	if err != nil {
		return nil, fmt.Errorf("read sales order %d invoices: %w", orderID, err)
	}
	return invoices, nil
}

// OrderPayments returns the payments reconciled with the order's invoices. An order without invoices has
// none. It needs the account module and returns ErrSaleOrderNotFound for an unknown order. Payments
// that are not reconciled with an invoice yet are not linked to the order and are not returned.
func (c *Client) OrderPayments(ctx context.Context, orderID int) ([]Payment, error) {
	var (
		err      error
		ids      []int
		payments = make([]Payment, 0)
	)

	if ids, err = c.orderRelationIDs(ctx, orderID, "invoice_ids"); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return payments, nil
	}
	err = c.CallRead(ctx, "account.payment", "search_read", map[string]any{
		"domain": []any{[]any{"reconciled_invoice_ids", "in", ids}},
		"fields": []string{
			"id", "name", "state", "payment_type", "amount", "date", "currency_id", "payment_method_line_id", "memo",
			"reconciled_invoice_ids",
		},
		"order": "id",
	}, &payments)
	if err != nil {
		return nil, fmt.Errorf("read sales order %d payments: %w", orderID, err)
	}
	return payments, nil
}
