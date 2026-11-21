// Command odoo-sync exports products from one Odoo database and imports them into another.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/d2jvkpn/odoo-go"
)

const usage = `Usage:
  odoo-sync check  -in catalog.json
  odoo-sync export -source source.yaml -out catalog.json [-include products|all]
  odoo-sync import -target target.yaml -in catalog.json [-dry-run] [-empty-code reject|skip]
                   [-sale-ok all|only] [-include products|all] [-no-create-deps] [-map-out map.csv]

Connection files are the YAML configuration read by odoo.LoadClient (odoo.base_url, odoo.api_key,
optional odoo.db). Products are matched between databases by default_code only.
`

func main() {
	var (
		ctx  context.Context
		stop context.CancelFunc
		err  error
	)

	ctx, stop = signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err = run(ctx, os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, out io.Writer) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		_, err := io.WriteString(out, usage)
		return err
	}
	switch args[0] {
	case "check":
		return runCheck(args[1:], out)
	case "export":
		return runExport(ctx, args[1:], out)
	case "import":
		return runImport(ctx, args[1:], out)
	default:
		return fmt.Errorf("unknown command %q; use -h for help", args[0])
	}
}

func runCheck(args []string, out io.Writer) error {
	var (
		flags    *flag.FlagSet
		in       *string
		f        *CatalogFile
		problems []string
		variants int
		err      error
	)

	flags = flag.NewFlagSet("odoo-sync check", flag.ContinueOnError)
	flags.SetOutput(out)
	in = flags.String("in", "", "catalog file")
	if err = flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if *in == "" {
		return errors.New("-in is required")
	}
	if f, err = readCatalogFile(*in); err != nil {
		return err
	}
	if problems = f.Validate(); len(problems) > 0 {
		for _, p := range problems {
			fmt.Fprintln(out, "-", p)
		}
		return fmt.Errorf("%d problems found", len(problems))
	}
	for _, t := range f.Templates {
		variants += len(t.Variants)
	}
	fmt.Fprintf(out, "ok: %d templates, %d variants\n", len(f.Templates), variants)
	return nil
}

// checkInclude accepts the sections this version can handle.
func checkInclude(v string) error {
	switch v {
	case "all", "products":
		return nil
	case "boms":
		return errors.New("BOM export and import are not implemented yet; use -include products")
	default:
		return fmt.Errorf("-include must be products or all, got %q", v)
	}
}

func runExport(ctx context.Context, args []string, out io.Writer) error {
	var (
		flags   *flag.FlagSet
		source  *string
		dest    *string
		include *string
		client  *odoo.Client
		f       *CatalogFile
		count   int
		omitted int
		err     error
	)

	flags = flag.NewFlagSet("odoo-sync export", flag.ContinueOnError)
	flags.SetOutput(out)
	source = flags.String("source", "", "connection file of the source Odoo")
	dest = flags.String("out", "", "catalog file to write")
	include = flags.String("include", "all", "sections to export: products or all")
	if err = flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if *source == "" || *dest == "" {
		return errors.New("-source and -out are required")
	}
	if err = checkInclude(*include); err != nil {
		return err
	}
	if client, err = odoo.LoadClient(*source); err != nil {
		return err
	}
	name := strings.TrimSuffix(filepath.Base(*source), filepath.Ext(*source))
	if f, omitted, err = buildExport(ctx, client, name, client.BaseURL, time.Now()); err != nil {
		return err
	}
	if err = writeCatalogFile(*dest, f); err != nil {
		return err
	}
	for _, t := range f.Templates {
		count += len(t.Variants)
	}
	fmt.Fprintf(out, "exported %d templates, %d variants to %s\n", len(f.Templates), count, *dest)
	if omitted > 0 {
		fmt.Fprintf(out, "omitted %d archived variants without default_code\n", omitted)
	}
	return nil
}

func runImport(ctx context.Context, args []string, out io.Writer) error {
	var (
		flags        *flag.FlagSet
		target       *string
		in           *string
		dryRun       *bool
		emptyCode    *string
		saleOK       *string
		include      *string
		noCreateDeps *bool
		mapOut       *string
		client       *odoo.Client
		f            *CatalogFile
		snap         *Snapshot
		plan         *Plan
		res          *Result
		problems     []string
		err          error
	)

	flags = flag.NewFlagSet("odoo-sync import", flag.ContinueOnError)
	flags.SetOutput(out)
	target = flags.String("target", "", "connection file of the target Odoo")
	in = flags.String("in", "", "catalog file to import")
	dryRun = flags.Bool("dry-run", false, "print the plan and write nothing")
	emptyCode = flags.String("empty-code", emptyCodeReject, "variants without default_code: reject or skip")
	saleOK = flags.String("sale-ok", saleOKAll, "which templates to import: all or only (sale_ok)")
	include = flags.String("include", "all", "sections to import: products or all")
	noCreateDeps = flags.Bool("no-create-deps", false, "fail instead of creating missing categories and attributes")
	mapOut = flags.String("map-out", "", "write the source-to-target variant mapping to this CSV file")
	if err = flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if *target == "" || *in == "" {
		return errors.New("-target and -in are required")
	}
	if err = checkInclude(*include); err != nil {
		return err
	}
	if *emptyCode != emptyCodeReject && *emptyCode != emptyCodeSkip {
		return fmt.Errorf("-empty-code must be reject or skip, got %q", *emptyCode)
	}
	if *saleOK != saleOKAll && *saleOK != saleOKOnly {
		return fmt.Errorf("-sale-ok must be all or only, got %q", *saleOK)
	}
	if *dryRun && *mapOut != "" {
		return errors.New("-map-out cannot be used with -dry-run: nothing is created, so there is no mapping")
	}
	if f, err = readCatalogFile(*in); err != nil {
		return err
	}
	if problems = f.Validate(); len(problems) > 0 {
		return reportProblems(out, problems)
	}
	if client, err = odoo.LoadClient(*target); err != nil {
		return err
	}
	if snap, err = loadSnapshot(ctx, client); err != nil {
		return err
	}
	plan, problems = buildPlan(f, snap, importOptions{EmptyCode: *emptyCode, SaleOK: *saleOK, NoCreateDeps: *noCreateDeps})
	if len(problems) > 0 {
		return reportProblems(out, problems)
	}
	if *dryRun {
		printPlan(out, plan)
		return nil
	}
	res, err = applyPlan(ctx, client, snap, plan)
	printResult(out, res)
	if err != nil {
		return err
	}
	if *mapOut != "" {
		if err = writeMapCSV(*mapOut, res.Map); err != nil {
			return err
		}
	}
	if res.Failed > 0 {
		return fmt.Errorf("%d templates failed", res.Failed)
	}
	return nil
}

func reportProblems(out io.Writer, problems []string) error {
	for _, p := range problems {
		fmt.Fprintln(out, "-", p)
	}
	return fmt.Errorf("%d problems found; nothing was written", len(problems))
}

func printResult(out io.Writer, r *Result) {
	for _, w := range r.Warnings {
		fmt.Fprintln(out, "warning:", w)
	}
	for _, f := range r.Failures {
		fmt.Fprintln(out, "failed:", f)
	}
	fmt.Fprintf(out, "created %d, updated %d, unchanged %d, archived %d, skipped %d, failed %d\n",
		r.Created, r.Updated, r.Unchanged, r.Archived, r.Skipped, r.Failed)
}
