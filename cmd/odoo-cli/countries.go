package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/d2jvkpn/odoo-go"
)

const countriesUsage = `Usage:
  odoo countries list [-config configs/local.yaml]

list prints every country as {id, name, code}, where code is the ISO 3166-1 alpha-2 code. Use the id
as country_id in partner files; ids differ between Odoo databases.
`

func runCountries(ctx context.Context, args []string, out io.Writer) error {
	var (
		err        error
		flags      *flag.FlagSet
		configPath *string
		client     *odoo.Client
		countries  []odoo.Country
		encoder    *json.Encoder
	)

	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		_, err = io.WriteString(out, countriesUsage)
		return err
	}
	if args[0] != "list" {
		return fmt.Errorf("unknown command %q; use -h for help", args[0])
	}
	flags = flag.NewFlagSet("odoo countries list", flag.ContinueOnError)
	flags.SetOutput(out)
	configPath = flags.String("config", "configs/local.yaml", "path to YAML configuration")
	if err = flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %v", flags.Args())
	}
	if client, err = odoo.LoadClient(*configPath); err != nil {
		return err
	}
	if countries, err = client.ListCountries(ctx); err != nil {
		return err
	}
	encoder = json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	if err = encoder.Encode(countries); err != nil {
		return fmt.Errorf("write countries: %w", err)
	}
	return nil
}
