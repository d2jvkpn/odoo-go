package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/d2jvkpn/odoo-go"
)

const categoriesUsage = `Usage:
  odoo categories list [-type internal|website] [-parent-id ID] [-domain '[]'] [-limit N] [-offset 0] [-order id]
  odoo categories list -all ...   (every page, one JSON array; -limit is the page size)
  odoo categories count [-type internal|website] [-parent-id ID] [-domain '[]']
  odoo categories get -id ID [-type internal|website]
  odoo categories create -file category.json [-type internal|website]
  odoo categories update -id ID -file patch.json [-type internal|website]

Both commands accept -config configs/local.yaml.
The default limit is 1000 for internal categories and 100 for website categories.
create writes a category to the configured Odoo database and prints its new ID.
For list, omit -parent-id for all levels; use 0 for roots or a positive ID for direct children.
`

func runCategories(ctx context.Context, args []string, out io.Writer) error {
	var (
		action         string
		flags          *flag.FlagSet
		configPath     *string
		kind           *string
		query          odoo.ProductCategoryQuery
		domain, file   string
		id, parentID   int
		limitSpecified bool
		all            bool
		countValue     int
		client         *odoo.Client
		result         any
		encoder        *json.Encoder
		err            error
	)

	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		_, err = io.WriteString(out, categoriesUsage)
		return err
	}
	action = args[0]
	if action == "update" {
		return runCategoryUpdate(ctx, args[1:], out)
	}
	flags = flag.NewFlagSet("odoo categories "+action, flag.ContinueOnError)
	flags.SetOutput(out)
	configPath = flags.String("config", "configs/local.yaml", "path to YAML configuration")
	kind = flags.String("type", "internal", "category type: internal or website")
	switch action {
	case "list":
		flags.IntVar(
			&query.Limit, "limit", 0,
			"maximum number of categories (default: internal 1000, website 100)",
		)
		flags.IntVar(&query.Offset, "offset", 0, "number of categories to skip")
		flags.StringVar(&query.Order, "order", "id", "Odoo sort expression")
		flags.StringVar(&domain, "domain", "[]", "Odoo domain as a JSON array")
		flags.IntVar(&parentID, "parent-id", 0, "parent ID: 0 for roots; omit for all levels")
		flags.BoolVar(&all, "all", false, "fetch every page; -limit is the page size")
	case "count":
		flags.StringVar(&domain, "domain", "[]", "Odoo domain as a JSON array")
		flags.IntVar(&parentID, "parent-id", 0, "parent ID: 0 for roots; omit for all levels")
	case "get":
		flags.IntVar(&id, "id", 0, "category ID")
	case "create":
		flags.StringVar(&file, "file", "", "JSON file containing the category to create")
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
	query.Type = odoo.CategoryType(*kind)
	if query.Type != odoo.InternalCategory && query.Type != odoo.WebsiteCategory {
		return fmt.Errorf("type must be internal or website")
	}
	if action == "list" || action == "count" {
		flags.Visit(func(f *flag.Flag) {
			switch f.Name {
			case "parent-id":
				query.ParentID = &parentID
			case "limit":
				limitSpecified = true
			}
		})
		if query.Limit < 0 ||
			(limitSpecified && query.Limit == 0) ||
			query.Offset < 0 ||
			parentID < 0 {
			return fmt.Errorf("limit must be positive, offset and parent-id nonnegative")
		}
		if query.Domain, err = parseDomain(domain); err != nil {
			return err
		}
	} else if action == "create" {
		return createCategory(ctx, *configPath, query.Type, file, out)
	} else if id <= 0 {
		return fmt.Errorf("id must be positive")
	}
	client, err = odoo.LoadClient(*configPath)
	if err != nil {
		return err
	}
	switch {
	case action == "list" && all:
		result, err = collect(client.AllProductCategories(ctx, query))
	case action == "list":
		result, err = client.SearchProductCategories(ctx, query)
	case action == "count":
		countValue, err = client.CountProductCategories(ctx, query)
		result = countResult{Count: countValue}
	default:
		result, err = client.GetProductCategory(ctx, query.Type, id)
	}
	if err != nil {
		return err
	}
	encoder = json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	if err = encoder.Encode(result); err != nil {
		return fmt.Errorf("write categories: %w", err)
	}
	return nil
}

func createCategory(
	ctx context.Context,
	configPath string,
	kind odoo.CategoryType,
	file string,
	out io.Writer,
) error {
	var (
		f       *os.File
		decoder *json.Decoder
		input   odoo.CreateProductCategoryRequest
		extra   any
		client  *odoo.Client
		id      int
		err     error
	)

	if file == "" {
		return fmt.Errorf("create requires -file category.json")
	}
	if f, err = os.Open(file); err != nil {
		return fmt.Errorf("open category file: %w", err)
	}
	defer f.Close()
	decoder = json.NewDecoder(f)
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&input); err != nil {
		return fmt.Errorf("decode category file: %w", err)
	}
	if err = decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("category file must contain exactly one JSON object")
	}
	if client, err = odoo.LoadClient(configPath); err != nil {
		return err
	}
	if id, err = client.CreateProductCategory(ctx, kind, input); err != nil {
		return err
	}
	return writeJSON(out, struct {
		ID int `json:"id"`
	}{ID: id}, "category")
}
