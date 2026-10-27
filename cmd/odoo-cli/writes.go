package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/d2jvkpn/odoo"
)

// parseWriteFlags parses -config, -id and optionally -file for a write action.
func parseWriteFlags(name string, args []string, out io.Writer, withFile bool) (config string, id int, file string, err error) {
	var flags = flag.NewFlagSet(name, flag.ContinueOnError)

	flags.SetOutput(out)
	flags.StringVar(&config, "config", "configs/local.yaml", "path to YAML configuration")
	flags.IntVar(&id, "id", 0, "record ID")
	if withFile {
		flags.StringVar(&file, "file", "", "JSON file containing the fields to change")
	}
	if err = flags.Parse(args); err != nil {
		return
	}
	if flags.NArg() != 0 {
		return "", 0, "", fmt.Errorf("unexpected positional arguments: %v", flags.Args())
	}
	if id <= 0 {
		return "", 0, "", fmt.Errorf("id must be positive")
	}
	return
}

// runOrderTransition handles `orders confirm|cancel`. Without -yes it only reports the action and
// sends nothing, so a mistyped command cannot change an order.
func runOrderTransition(ctx context.Context, action string, args []string, out io.Writer) error {
	var (
		err        error
		flags      *flag.FlagSet
		configPath *string
		id         *int
		yes        *bool
		client     *odoo.Client
	)

	flags = flag.NewFlagSet("odoo orders "+action, flag.ContinueOnError)
	flags.SetOutput(out)
	configPath = flags.String("config", "configs/local.yaml", "path to YAML configuration")
	id = flags.Int("id", 0, "sales order ID")
	yes = flags.Bool("yes", false, "actually perform the action; without it nothing is sent")
	if err = flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 || *id <= 0 {
		return fmt.Errorf("%s requires -id ID and takes no positional arguments", action)
	}
	if *yes {
		if client, err = odoo.LoadClient(*configPath); err != nil {
			return err
		}
		if action == "confirm" {
			err = client.ConfirmSaleOrder(ctx, *id)
		} else {
			err = client.CancelSaleOrder(ctx, *id)
		}
		if err != nil {
			return err
		}
	}
	return printJSON(out, struct {
		Action   string `json:"action"`
		ID       int    `json:"id"`
		Executed bool   `json:"executed"`
	}{action, *id, *yes})
}

// runProductWrite handles `products update|archive|unarchive`.
func runProductWrite(ctx context.Context, action string, args []string, out io.Writer) error {
	var (
		err    error
		config string
		id     int
		file   string
		patch  odoo.UpdateProductRequest
		client *odoo.Client
	)

	if config, id, file, err = parseWriteFlags("odoo products "+action, args, out, action == "update"); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if action == "update" {
		if err = decodeFile(file, &patch); err != nil {
			return err
		}
	}
	if client, err = odoo.LoadClient(config); err != nil {
		return err
	}
	switch action {
	case "update":
		if err = client.UpdateProduct(ctx, id, patch); err != nil {
			return err
		}
		return printJSON(out, struct {
			ID int `json:"id"`
		}{id})
	default:
		active := action == "unarchive"
		if err = client.SetProductActive(ctx, id, active); err != nil {
			return err
		}
		return printJSON(out, struct {
			ID     int  `json:"id"`
			Active bool `json:"active"`
		}{id, active})
	}
}

// runCategoryUpdate handles `categories update`.
func runCategoryUpdate(ctx context.Context, args []string, out io.Writer) error {
	var (
		err    error
		flags  *flag.FlagSet
		config *string
		kind   *string
		id     *int
		file   *string
		patch  odoo.UpdateProductCategoryRequest
		client *odoo.Client
	)

	flags = flag.NewFlagSet("odoo categories update", flag.ContinueOnError)
	flags.SetOutput(out)
	config = flags.String("config", "configs/local.yaml", "path to YAML configuration")
	kind = flags.String("type", "internal", "category type: internal or website")
	id = flags.Int("id", 0, "category ID")
	file = flags.String("file", "", "JSON file containing the fields to change")
	if err = flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 || *id <= 0 {
		return fmt.Errorf("update requires -id ID and -file patch.json")
	}
	if *kind != "internal" && *kind != "website" {
		return fmt.Errorf("type must be internal or website")
	}
	if err = decodeFile(*file, &patch); err != nil {
		return err
	}
	if client, err = odoo.LoadClient(*config); err != nil {
		return err
	}
	if err = client.UpdateProductCategory(ctx, odoo.CategoryType(*kind), *id, patch); err != nil {
		return err
	}
	return printJSON(out, struct {
		ID int `json:"id"`
	}{*id})
}

// runOrderUpdate handles `orders update`: change client_order_ref, origin or note of one order.
func runOrderUpdate(ctx context.Context, args []string, out io.Writer) error {
	var (
		err    error
		config string
		id     int
		file   string
		patch  odoo.UpdateSaleOrderRequest
		client *odoo.Client
	)

	if config, id, file, err = parseWriteFlags("odoo orders update", args, out, true); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if err = decodeFile(file, &patch); err != nil {
		return err
	}
	if client, err = odoo.LoadClient(config); err != nil {
		return err
	}
	if err = client.UpdateSaleOrder(ctx, id, patch); err != nil {
		return err
	}
	return printJSON(out, struct {
		ID int `json:"id"`
	}{id})
}
