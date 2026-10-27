package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"

	"github.com/d2jvkpn/odoo"
)

const stockUsage = `Usage:
  odoo stock get -ids 1,2,3 [-config configs/local.yaml]

  odoo stock locations [-config configs/local.yaml]
  odoo stock set -product-id ID -location-id ID -qty N [-yes] [-config configs/local.yaml]

get prints on-hand, forecast, and free quantities for up to 200 product.product IDs. Unknown IDs
are omitted.
locations lists the internal stock locations that set can target.
set replaces the counted quantity (0 means sold out) of a product that tracks inventory. It changes
stock, so without -yes it only prints what it would do and sends nothing.
`

func runStock(ctx context.Context, args []string, out io.Writer) error {
	var (
		err        error
		flags      *flag.FlagSet
		configPath *string
		idList     string
		ids        []int
		client     *odoo.Client
		stock      []odoo.ProductStock
	)

	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		_, err = io.WriteString(out, stockUsage)
		return err
	}
	switch args[0] {
	case "locations":
		return runStockLocations(ctx, args[1:], out)
	case "set":
		return runStockSet(ctx, args[1:], out)
	case "get":
	default:
		return fmt.Errorf("unknown command %q; use -h for help", args[0])
	}
	flags = flag.NewFlagSet("odoo stock get", flag.ContinueOnError)
	flags.SetOutput(out)
	configPath = flags.String("config", "configs/local.yaml", "path to YAML configuration")
	flags.StringVar(&idList, "ids", "", "comma-separated product.product IDs")
	if err = flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %v", flags.Args())
	}
	for _, part := range strings.Split(idList, ",") {
		id, convErr := strconv.Atoi(strings.TrimSpace(part))
		if convErr != nil || id <= 0 {
			return fmt.Errorf("-ids must be comma-separated positive integers")
		}
		ids = append(ids, id)
	}
	if client, err = odoo.LoadClient(*configPath); err != nil {
		return err
	}
	if stock, err = client.GetProductStock(ctx, ids); err != nil {
		return err
	}
	return printJSON(out, stock)
}

func runStockLocations(ctx context.Context, args []string, out io.Writer) error {
	var (
		err       error
		flags     = flag.NewFlagSet("odoo stock locations", flag.ContinueOnError)
		config    = flags.String("config", "configs/local.yaml", "path to YAML configuration")
		client    *odoo.Client
		locations []odoo.StockLocation
	)

	flags.SetOutput(out)
	if err = flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %v", flags.Args())
	}
	if client, err = odoo.LoadClient(*config); err != nil {
		return err
	}
	if locations, err = client.ListStockLocations(ctx); err != nil {
		return err
	}
	return printJSON(out, locations)
}

// runStockSet handles `stock set`. Without -yes it validates and reports the action, sends nothing,
// and does not read the configuration, so a mistyped command cannot change stock.
func runStockSet(ctx context.Context, args []string, out io.Writer) error {
	var (
		err        error
		flags      = flag.NewFlagSet("odoo stock set", flag.ContinueOnError)
		config     = flags.String("config", "configs/local.yaml", "path to YAML configuration")
		productID  = flags.Int("product-id", 0, "product.product ID")
		locationID = flags.Int("location-id", 0, "internal stock location ID (see stock locations)")
		qty        = flags.Float64("qty", 0, "quantity on hand to set; 0 means sold out")
		yes        = flags.Bool("yes", false, "actually change stock; without it nothing is sent")
		qtySet     bool
		client     *odoo.Client
	)

	flags.SetOutput(out)
	if err = flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	flags.Visit(func(f *flag.Flag) { qtySet = qtySet || f.Name == "qty" })
	if flags.NArg() != 0 || *productID <= 0 || *locationID <= 0 || !qtySet {
		return fmt.Errorf("set requires -product-id, -location-id and -qty")
	}
	if math.IsNaN(*qty) || math.IsInf(*qty, 0) || *qty < 0 {
		return fmt.Errorf("-qty must be finite and nonnegative")
	}
	if *yes {
		if client, err = odoo.LoadClient(*config); err != nil {
			return err
		}
		if err = client.SetProductStock(ctx, *productID, *locationID, *qty); err != nil {
			return err
		}
	}
	return printJSON(out, struct {
		ProductID  int     `json:"product_id"`
		LocationID int     `json:"location_id"`
		Qty        float64 `json:"qty"`
		Executed   bool    `json:"executed"`
	}{*productID, *locationID, *qty, *yes})
}
