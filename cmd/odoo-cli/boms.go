package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/d2jvkpn/odoo-go"
)

const bomsUsage = `Usage:
  odoo boms list [-archived] [-config configs/local.yaml]
  odoo boms get -id ID [-config configs/local.yaml]

list prints every bill of materials with its lines; -archived includes archived ones.
Needs the mrp module on the Odoo database.
`

func runBOMs(ctx context.Context, args []string, out io.Writer) error {
	var (
		flags      *flag.FlagSet
		configPath *string
		archived   bool
		id         int
		client     *odoo.Client
		err        error
	)

	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		_, err = io.WriteString(out, bomsUsage)
		return err
	}
	if args[0] != "list" && args[0] != "get" {
		return fmt.Errorf("unknown boms command %q; use -h for help", args[0])
	}
	flags = flag.NewFlagSet("odoo boms "+args[0], flag.ContinueOnError)
	flags.SetOutput(out)
	configPath = flags.String("config", "configs/local.yaml", "path to YAML configuration")
	if args[0] == "list" {
		flags.BoolVar(&archived, "archived", false, "include archived bills of materials")
	} else {
		flags.IntVar(&id, "id", 0, "mrp.bom ID")
	}
	if err = flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %v", flags.Args())
	}
	if args[0] == "get" && id <= 0 {
		return fmt.Errorf("id must be greater than zero")
	}
	if client, err = odoo.LoadClient(*configPath); err != nil {
		return err
	}
	if args[0] == "get" {
		var bom *odoo.BOM
		if bom, err = client.GetBOM(ctx, id); err != nil {
			return fmt.Errorf("query bill of materials: %w", err)
		}
		return writeJSON(out, bom, "bill of materials")
	}
	var boms []odoo.BOM
	if boms, err = client.ListBOMs(ctx, archived); err != nil {
		return fmt.Errorf("query bills of materials: %w", err)
	}
	return writeJSON(out, boms, "bills of materials")
}
