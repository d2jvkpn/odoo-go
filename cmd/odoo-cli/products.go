package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/d2jvkpn/odoo"
)

const productsUsage = `Usage:
  odoo products list [-config configs/local.yaml] [-limit 100] [-offset 0] [-order id]
                     [-domain '[["sale_ok","=",true]]'] [-all]
  odoo products count [-domain '[["sale_ok","=",true]]'] [-config configs/local.yaml]
  odoo products get -id ID [-config configs/local.yaml]
  odoo products create -file product.json [-config configs/local.yaml]
  odoo products update -id ID -file patch.json [-config configs/local.yaml]
  odoo products archive|unarchive -id ID [-config configs/local.yaml]

With -all, list fetches every page into one JSON array and -limit is the page size.
create writes a product to the configured Odoo database and prints the new product.product ID.
`

const saleableDomain = `[["sale_ok","=",true]]`

// productListOptions are the parsed flags of "products list".
type productListOptions struct {
	Query odoo.ProductQuery
	All   bool
}

func runProducts(ctx context.Context, args []string, out io.Writer) error {
	var (
		flags      *flag.FlagSet
		configPath *string
		opts       productListOptions
		domain     string
		file       string
		id         int
		err        error
	)

	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		_, err = io.WriteString(out, productsUsage)
		return err
	}
	if args[0] == "update" || args[0] == "archive" || args[0] == "unarchive" {
		return runProductWrite(ctx, args[0], args[1:], out)
	}
	if args[0] != "list" && args[0] != "get" && args[0] != "count" && args[0] != "create" {
		return fmt.Errorf("unknown products command %q; use -h for help", args[0])
	}
	flags = flag.NewFlagSet("odoo products "+args[0], flag.ContinueOnError)
	flags.SetOutput(out)
	configPath = flags.String("config", "configs/local.yaml", "path to YAML configuration")
	switch args[0] {
	case "list":
		flags.IntVar(&opts.Query.Limit, "limit", 100, "maximum number of products (page size with -all)")
		flags.IntVar(&opts.Query.Offset, "offset", 0, "number of products to skip")
		flags.StringVar(&opts.Query.Order, "order", "id", "Odoo sort expression")
		flags.StringVar(&domain, "domain", saleableDomain, "Odoo domain as a JSON array")
		flags.BoolVar(&opts.All, "all", false, "fetch every page")
	case "count":
		flags.StringVar(&domain, "domain", saleableDomain, "Odoo domain as a JSON array")
	case "get":
		flags.IntVar(&id, "id", 0, "product.product ID")
	case "create":
		flags.StringVar(&file, "file", "", "JSON file containing the product to create")
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
	switch args[0] {
	case "get":
		return getProduct(ctx, *configPath, id, out)
	case "count":
		return countProducts(ctx, *configPath, domain, out)
	case "create":
		return createProduct(ctx, *configPath, file, out)
	}
	if opts.Query.Domain, err = parseDomain(domain); err != nil {
		return err
	}
	return listProducts(ctx, *configPath, opts, out)
}

func listProducts(ctx context.Context, configPath string, opts productListOptions, out io.Writer) error {
	var (
		client   *odoo.Client
		products []odoo.Product
		err      error
	)

	if opts.Query.Limit <= 0 || opts.Query.Offset < 0 {
		return fmt.Errorf("limit must be greater than zero and offset nonnegative")
	}
	if client, err = odoo.LoadClient(configPath); err != nil {
		return err
	}
	if opts.All {
		products, err = collect(client.AllProducts(ctx, opts.Query))
	} else {
		products, err = client.SearchProducts(ctx, opts.Query)
	}
	if err != nil {
		return fmt.Errorf("query products: %w", err)
	}
	return writeJSON(out, products, "products")
}

func countProducts(ctx context.Context, configPath, domainText string, out io.Writer) error {
	var (
		client *odoo.Client
		domain []any
		total  int
		err    error
	)

	if domain, err = parseDomain(domainText); err != nil {
		return err
	}
	if client, err = odoo.LoadClient(configPath); err != nil {
		return err
	}
	if total, err = client.CountProducts(ctx, domain); err != nil {
		return fmt.Errorf("count products: %w", err)
	}
	return writeJSON(out, countResult{Count: total}, "count")
}

func getProduct(ctx context.Context, configPath string, id int, out io.Writer) error {
	var (
		client  *odoo.Client
		product *odoo.Product
		err     error
	)

	if id <= 0 {
		return fmt.Errorf("id must be greater than zero")
	}
	if client, err = odoo.LoadClient(configPath); err != nil {
		return err
	}
	if product, err = client.GetProduct(ctx, id); err != nil {
		return fmt.Errorf("query product: %w", err)
	}
	return writeJSON(out, product, "product")
}

func createProduct(ctx context.Context, configPath, file string, out io.Writer) error {
	var (
		f       *os.File
		decoder *json.Decoder
		input   odoo.CreateProductRequest
		extra   any
		client  *odoo.Client
		id      int
		err     error
	)

	if file == "" {
		return fmt.Errorf("create requires -file product.json")
	}
	if f, err = os.Open(file); err != nil {
		return fmt.Errorf("open product file: %w", err)
	}
	defer f.Close()
	decoder = json.NewDecoder(f)
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&input); err != nil {
		return fmt.Errorf("decode product file: %w", err)
	}
	if err = decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("product file must contain exactly one JSON object")
	}
	if client, err = odoo.LoadClient(configPath); err != nil {
		return err
	}
	if id, err = client.CreateProduct(ctx, input); err != nil {
		return err
	}
	return writeJSON(out, struct {
		ID int `json:"id"`
	}{ID: id}, "product")
}

// writeJSON prints value as indented JSON.
func writeJSON(out io.Writer, value any, what string) error {
	var encoder *json.Encoder = json.NewEncoder(out)

	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return fmt.Errorf("write %s: %w", what, err)
	}
	return nil
}
