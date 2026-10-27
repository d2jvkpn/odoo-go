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

const ordersUsage = `Usage:
  odoo orders list [-config configs/local.yaml] [-domain '[]'] [-limit 100] [-offset 0] [-order 'id desc']
  odoo orders list -all [-limit PAGE_SIZE] ...   (every page, one JSON array)
  odoo orders count [-domain '[]'] [-config configs/local.yaml]
  odoo orders get -id ID [-config configs/local.yaml]
  odoo orders create -file order.json [-config configs/local.yaml]
  odoo orders update -id ID -file patch.json [-config configs/local.yaml]
  odoo orders confirm|cancel -id ID [-yes] [-config configs/local.yaml]

create writes a draft quotation and its lines to the configured Odoo database.
confirm and cancel change an existing order only with -yes; without it nothing is sent.
`

func runOrders(ctx context.Context, args []string, out io.Writer) error {
	var (
		action     string
		flags      *flag.FlagSet
		configPath *string
		query      odoo.SaleOrderQuery
		domain     string
		file       string
		all        bool
		id         int
		input      odoo.CreateSaleOrderRequest
		f          *os.File
		decoder    *json.Decoder
		extra      any
		client     *odoo.Client
		result     any
		createdID  int
		countValue int
		encoder    *json.Encoder
		err        error
	)

	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		_, err = io.WriteString(out, ordersUsage)
		return err
	}
	action = args[0]
	if action == "update" {
		return runOrderUpdate(ctx, args[1:], out)
	}
	if action == "confirm" || action == "cancel" {
		return runOrderTransition(ctx, action, args[1:], out)
	}
	flags = flag.NewFlagSet("odoo orders "+action, flag.ContinueOnError)
	flags.SetOutput(out)
	configPath = flags.String("config", "configs/local.yaml", "path to YAML configuration")
	switch action {
	case "list":
		flags.IntVar(&query.Limit, "limit", 100, "maximum number of orders")
		flags.IntVar(&query.Offset, "offset", 0, "number of orders to skip")
		flags.StringVar(&query.Order, "order", "id desc", "Odoo sort expression")
		flags.StringVar(&domain, "domain", "[]", "Odoo domain as a JSON array")
		flags.BoolVar(&all, "all", false, "fetch every page; -limit is the page size")
	case "count":
		flags.StringVar(&domain, "domain", "[]", "Odoo domain as a JSON array")
	case "get":
		flags.IntVar(&id, "id", 0, "sales order ID")
	case "create":
		flags.StringVar(&file, "file", "", "JSON file containing the order to create")
	default:
		return fmt.Errorf("unknown command %q; use -h for help", action)
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
	switch action {
	case "list":
		if query.Limit <= 0 || query.Offset < 0 {
			return fmt.Errorf("limit must be positive and offset nonnegative")
		}
		if query.Domain, err = parseDomain(domain); err != nil {
			return err
		}
	case "count":
		if query.Domain, err = parseDomain(domain); err != nil {
			return err
		}
	case "get":
		if id <= 0 {
			return fmt.Errorf("id must be positive")
		}
	case "create":
		if file == "" {
			return fmt.Errorf("create requires -file order.json")
		}
		f, err = os.Open(file)
		if err != nil {
			return fmt.Errorf("open order file: %w", err)
		}
		defer f.Close()
		decoder = json.NewDecoder(f)
		decoder.DisallowUnknownFields()
		if err = decoder.Decode(&input); err != nil {
			return fmt.Errorf("decode order file: %w", err)
		}
		if err = decoder.Decode(&extra); err != io.EOF {
			return fmt.Errorf("order file must contain exactly one JSON object")
		}
	}
	client, err = odoo.LoadClient(*configPath)
	if err != nil {
		return err
	}
	switch action {
	case "list":
		if all {
			result, err = collect(client.AllSaleOrders(ctx, query))
		} else {
			result, err = client.SearchSaleOrders(ctx, query)
		}
	case "count":
		countValue, err = client.CountSaleOrders(ctx, query.Domain)
		result = countResult{Count: countValue}
	case "get":
		result, err = client.GetSaleOrder(ctx, id)
	case "create":
		createdID, err = client.CreateSaleOrder(ctx, input)
		result = struct {
			ID int `json:"id"`
		}{ID: createdID}
	}
	if err != nil {
		return err
	}
	encoder = json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	if err = encoder.Encode(result); err != nil {
		return fmt.Errorf("write orders: %w", err)
	}
	return nil
}
