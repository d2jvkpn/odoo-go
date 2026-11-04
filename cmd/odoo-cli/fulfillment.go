package main

import (
	"context"
	"errors"
	"flag"
	"io"

	"github.com/d2jvkpn/odoo-go"
)

// runOrderFulfillment handles the read-only `orders pickings|invoices|payments`.
func runOrderFulfillment(ctx context.Context, action string, args []string, out io.Writer) error {
	var (
		err    error
		config string
		id     int
		client *odoo.Client
		result any
	)

	if config, id, _, err = parseWriteFlags("odoo orders "+action, args, out, false); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if client, err = odoo.LoadClient(config); err != nil {
		return err
	}
	switch action {
	case "pickings":
		result, err = client.OrderPickings(ctx, id)
	case "invoices":
		result, err = client.OrderInvoices(ctx, id)
	default:
		result, err = client.OrderPayments(ctx, id)
	}
	if err != nil {
		return err
	}
	return writeJSON(out, result, action)
}
