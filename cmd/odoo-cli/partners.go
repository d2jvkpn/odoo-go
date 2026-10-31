package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/d2jvkpn/odoo-go"
)

const partnersUsage = `Usage:
  odoo partners list [-domain '[]'] [-limit 100] [-offset 0] [-order id] [-config configs/local.yaml]
  odoo partners get -id ID [-config configs/local.yaml]
  odoo partners create -file partner.json [-config configs/local.yaml]
  odoo partners update -id ID -file patch.json [-config configs/local.yaml]
  odoo partners upsert -ref REF -file partner.json [-config configs/local.yaml]
  odoo partners tags [-config configs/local.yaml]

upsert matches an active partner by ref and updates it, or creates one. Concurrent upserts for the
same ref may create duplicates; serialize them per ref. tags prints every partner tag as
{id, name, display_name}; use the ids in category_id.
`

func runPartners(ctx context.Context, args []string, out io.Writer) error {
	var (
		err        error
		action     string
		flags      *flag.FlagSet
		configPath *string
		query      odoo.PartnerQuery
		domain     string
		file       string
		ref        string
		id         int
		client     *odoo.Client
		result     any
	)

	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		_, err = io.WriteString(out, partnersUsage)
		return err
	}
	action = args[0]
	flags = flag.NewFlagSet("odoo partners "+action, flag.ContinueOnError)
	flags.SetOutput(out)
	configPath = flags.String("config", "configs/local.yaml", "path to YAML configuration")
	switch action {
	case "list":
		flags.IntVar(&query.Limit, "limit", 100, "maximum number of partners")
		flags.IntVar(&query.Offset, "offset", 0, "number of partners to skip")
		flags.StringVar(&query.Order, "order", "id", "Odoo sort expression")
		flags.StringVar(&domain, "domain", "[]", "Odoo domain as a JSON array")
	case "tags":
	case "get":
		flags.IntVar(&id, "id", 0, "partner ID")
	case "create":
		flags.StringVar(&file, "file", "", "JSON file containing the partner")
	case "update":
		flags.IntVar(&id, "id", 0, "partner ID")
		flags.StringVar(&file, "file", "", "JSON file containing the fields to change")
	case "upsert":
		flags.StringVar(&ref, "ref", "", "caller's identifier stored in the partner ref")
		flags.StringVar(&file, "file", "", "JSON file containing the partner")
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

	var (
		create odoo.CreatePartnerRequest
		patch  odoo.UpdatePartnerRequest
	)
	switch action {
	case "list":
		if query.Limit <= 0 || query.Offset < 0 {
			return fmt.Errorf("limit must be positive and offset nonnegative")
		}
		if query.Domain, err = parseDomain(domain); err != nil {
			return err
		}
	case "get", "update":
		if id <= 0 {
			return fmt.Errorf("id must be positive")
		}
	case "upsert":
		if ref == "" {
			return fmt.Errorf("upsert requires -ref")
		}
	}
	switch action {
	case "create", "upsert":
		if err = decodeFile(file, &create); err != nil {
			return err
		}
	case "update":
		if err = decodeFile(file, &patch); err != nil {
			return err
		}
	}
	if client, err = odoo.LoadClient(*configPath); err != nil {
		return err
	}
	switch action {
	case "list":
		result, err = client.SearchPartners(ctx, query)
	case "tags":
		result, err = client.ListPartnerTags(ctx)
	case "get":
		result, err = client.GetPartner(ctx, id)
	case "create":
		var created int
		created, err = client.CreatePartner(ctx, create)
		result = struct {
			ID int `json:"id"`
		}{created}
	case "update":
		err = client.UpdatePartner(ctx, id, patch)
		result = struct {
			ID int `json:"id"`
		}{id}
	case "upsert":
		var (
			upserted int
			created  bool
		)
		upserted, created, err = client.UpsertPartnerByRef(ctx, ref, create)
		result = struct {
			ID      int  `json:"id"`
			Created bool `json:"created"`
		}{upserted, created}
	}
	if err != nil {
		return err
	}
	return printJSON(out, result)
}
