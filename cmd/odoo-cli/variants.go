package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/d2jvkpn/odoo-go"
)

const variantsUsage = `Usage:
  odoo products variants list -template ID [-config configs/local.yaml]
  odoo products variants add -template ID -attribute NAME -value V [-value V]... [-config configs/local.yaml]
  odoo products variants price -value-id ID -extra AMOUNT [-config configs/local.yaml]

list prints the variants of a product.template (the "product_tmpl_id" of a product).
add creates a product.attribute with the given values and attaches it to the template; Odoo then generates
the variants and the command prints them. Writes are never retried.
price sets the extra price of one attribute value (an entry of a variant's "product_template_variant_value_ids").
`

// runProductVariants handles `products variants`.
func runProductVariants(ctx context.Context, args []string, out io.Writer) error {
	var (
		err      error
		flags    *flag.FlagSet
		config   *string
		template *int
		valueID  *int
		attr     *string
		extra    *float64
		values   []string
		client   *odoo.Client
		variants []odoo.Product
	)

	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		_, err = io.WriteString(out, variantsUsage)
		return err
	}
	if args[0] != "list" && args[0] != "add" && args[0] != "price" {
		return fmt.Errorf("unknown products variants command %q; use -h for help", args[0])
	}
	flags = flag.NewFlagSet("odoo products variants "+args[0], flag.ContinueOnError)
	flags.SetOutput(out)
	config = flags.String("config", "configs/local.yaml", "path to YAML configuration")
	template = flags.Int("template", 0, "product.template ID")
	valueID = flags.Int("value-id", 0, "product.template.attribute.value ID")
	attr = flags.String("attribute", "", "new attribute name, for example Weight")
	extra = flags.Float64("extra", 0, "extra price of the value")
	flags.Func("value", "attribute value, for example 500g (repeatable)", func(v string) error {
		values = append(values, v)
		return nil
	})
	if err = flags.Parse(args[1:]); err != nil {
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
	switch args[0] {
	case "list":
		if variants, err = client.ProductVariants(ctx, *template); err != nil {
			return err
		}
	case "add":
		if variants, err = client.AddProductVariants(ctx, *template, *attr, values); err != nil {
			return err
		}
	default:
		if err = client.SetVariantPriceExtra(ctx, *valueID, *extra); err != nil {
			return err
		}
		return printJSON(out, struct {
			ValueID    int     `json:"value_id"`
			PriceExtra float64 `json:"price_extra"`
		}{*valueID, *extra})
	}
	return writeJSON(out, variants, "variants")
}
