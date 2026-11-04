package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/d2jvkpn/odoo-go"
)

const pickingsUsage = `Usage:
  odoo pickings validate -id ID [-backorder] [-yes] [-config configs/local.yaml]

validate confirms that a ready delivery (see "odoo orders pickings -id ORDER_ID") was shipped: stock is
reduced and the delivery becomes done. This cannot be undone except by a return in Odoo, so without -yes
nothing is sent. The reserved quantity is validated as is; if it is below the demand the command fails
unless -backorder is given, which creates a backorder for the rest.
`

// runPickings handles `pickings validate`.
func runPickings(ctx context.Context, args []string, out io.Writer) error {
	var (
		err        error
		flags      *flag.FlagSet
		configPath *string
		id         *int
		backorder  *bool
		yes        *bool
		client     *odoo.Client
		picking    *odoo.StockPicking
	)

	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		_, err = io.WriteString(out, pickingsUsage)
		return err
	}
	if args[0] != "validate" {
		return fmt.Errorf("unknown pickings command %q; use -h for help", args[0])
	}
	flags = flag.NewFlagSet("odoo pickings validate", flag.ContinueOnError)
	flags.SetOutput(out)
	configPath = flags.String("config", "configs/local.yaml", "path to YAML configuration")
	id = flags.Int("id", 0, "delivery (stock.picking) ID")
	backorder = flags.Bool("backorder", false, "create a backorder when the reserved quantity is below the demand")
	yes = flags.Bool("yes", false, "actually validate; without it nothing is sent")
	if err = flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 || *id <= 0 {
		return fmt.Errorf("validate requires -id ID and takes no positional arguments")
	}
	if !*yes {
		return printJSON(out, struct {
			Action   string `json:"action"`
			ID       int    `json:"id"`
			Executed bool   `json:"executed"`
		}{"validate", *id, false})
	}
	if client, err = odoo.LoadClient(*configPath); err != nil {
		return err
	}
	if picking, err = client.ValidatePicking(ctx, *id, odoo.ValidatePickingOptions{Backorder: *backorder}); err != nil {
		return err
	}
	return writeJSON(out, picking, "picking")
}
