// Command odoo-sync copies products, bills of materials and partners from one Odoo database to another.
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
  odoo-sync products export -source source.yaml -out products.json
  odoo-sync products check  -in products.json [-target target.yaml] [-empty-code reject|skip]
                            [-sale-ok all|only] [-no-create-deps]
  odoo-sync products import -target target.yaml -in products.json [-dry-run] [-empty-code reject|skip]
                            [-sale-ok all|only] [-no-create-deps] [-map-out map.csv]
  odoo-sync boms export|check|import   same flags as products, without -sale-ok and -map-out
  odoo-sync partners export -source source.yaml -out partners.json [--infer-country-from-state]
  odoo-sync partners check  -in partners.json [-target target.yaml] [-empty-key reject|skip]
                            [-no-create-deps]
  odoo-sync partners import -target target.yaml -in partners.json [-dry-run]
                            [-empty-key reject|skip] [-no-create-deps]

Connection files are the YAML configuration read by odoo.LoadClient (odoo.base_url, odoo.api_key,
optional odoo.db). Products are matched between databases by default_code only. Import products
before boms: a BOM needs its products to exist in the target.
Partners are matched by ref, then vat, then email; children by parent, type and name.
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
	case "products", "boms":
		return runCatalog(ctx, args[0], args[1:], out)
	case "partners":
		return runPartners(ctx, args[1:], out)
	default:
		return fmt.Errorf("unknown command %q; use -h for help", args[0])
	}
}

// runCatalog runs one command of the products or boms group.
func runCatalog(ctx context.Context, section string, args []string, out io.Writer) error {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		_, err := io.WriteString(out, usage)
		return err
	}
	if len(args) == 0 {
		return fmt.Errorf("odoo-sync %s: missing command; use export, check or import", section)
	}
	switch args[0] {
	case "check":
		return runCheck(ctx, section, args[1:], out)
	case "export":
		return runExport(ctx, section, args[1:], out)
	case "import":
		return runImport(ctx, section, args[1:], out)
	default:
		return fmt.Errorf("unknown command %q for %s; use -h for help", args[0], section)
	}
}

func runCheck(ctx context.Context, section string, args []string, out io.Writer) error {
	var (
		flags        *flag.FlagSet
		in           *string
		target       *string
		emptyCode    *string
		saleOK       *string
		noCreateDeps *bool
		f            *CatalogFile
		client       *odoo.Client
		snap         *Snapshot
		opt          importOptions
		problems     []string
		variants     int
		err          error
	)

	flags = flag.NewFlagSet("odoo-sync "+section+" check", flag.ContinueOnError)
	flags.SetOutput(out)
	in = flags.String("in", "", "catalog file")
	target = flags.String("target", "", "optional target connection file for import preflight")
	emptyCode = flags.String("empty-code", emptyCodeReject, "variants without default_code: reject or skip")
	saleOK = flags.String("sale-ok", saleOKAll, "which templates to check: all or only (sale_ok)")
	noCreateDeps = flags.Bool("no-create-deps", false, "fail if a target dependency is missing; requires -target")
	if err = flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if *in == "" {
		return errors.New("-in is required")
	}
	if *emptyCode != emptyCodeReject && *emptyCode != emptyCodeSkip {
		return fmt.Errorf("-empty-code must be reject or skip, got %q", *emptyCode)
	}
	if *saleOK != saleOKAll && *saleOK != saleOKOnly {
		return fmt.Errorf("-sale-ok must be all or only, got %q", *saleOK)
	}
	if *noCreateDeps && *target == "" {
		return errors.New("-no-create-deps requires -target")
	}
	if f, err = readCatalogFile(*in); err != nil {
		return err
	}
	if problems = f.Validate(); len(problems) > 0 {
		return reportProblems(out, problems)
	}
	if !f.HasSection(section) {
		return fmt.Errorf("the file has no %s section", section)
	}
	opt = importOptions{
		EmptyCode: *emptyCode, SaleOK: *saleOK, NoCreateDeps: *noCreateDeps,
		NoProducts: section == "boms", BOMs: section == "boms" && len(f.BOMs) > 0,
	}
	if problems = catalogPolicyProblems(f, section, opt); len(problems) > 0 {
		return reportProblems(out, problems)
	}
	if *target != "" {
		if client, err = odoo.LoadClient(*target); err != nil {
			return err
		}
		if snap, err = loadSnapshot(ctx, client, opt.BOMs); err != nil {
			return err
		}
		if _, problems = buildPlan(f, snap, opt); len(problems) > 0 {
			return reportProblems(out, problems)
		}
	}
	for _, t := range f.Templates {
		variants += len(t.Variants)
	}
	if *target == "" {
		fmt.Fprintf(out, "ok: %d templates, %d variants, %d boms (file checks only; use -target for import preflight)\n",
			len(f.Templates), variants, len(f.BOMs))
	} else {
		fmt.Fprintf(out, "ok: %d templates, %d variants, %d boms (target preflight passed; nothing was written)\n",
			len(f.Templates), variants, len(f.BOMs))
	}
	return nil
}

func runExport(ctx context.Context, section string, args []string, out io.Writer) error {
	var (
		flags   *flag.FlagSet
		source  *string
		dest    *string
		client  *odoo.Client
		f       *CatalogFile
		count   int
		uncoded int
		omitted int
		err     error
	)

	flags = flag.NewFlagSet("odoo-sync "+section+" export", flag.ContinueOnError)
	flags.SetOutput(out)
	source = flags.String("source", "", "connection file of the source Odoo")
	dest = flags.String("out", "", "catalog file to write")
	if err = flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if *source == "" || *dest == "" {
		return errors.New("-source and -out are required")
	}
	if client, err = odoo.LoadClient(*source); err != nil {
		return err
	}
	name := strings.TrimSuffix(filepath.Base(*source), filepath.Ext(*source))
	if f, omitted, err = buildExport(ctx, client, name, client.BaseURL, time.Now(), section); err != nil {
		return err
	}
	if err = writeCatalogFile(*dest, f); err != nil {
		return err
	}
	for _, t := range f.Templates {
		count += len(t.Variants)
		for _, v := range t.Variants {
			if v.Active && v.DefaultCode == "" {
				uncoded++
			}
		}
	}
	fmt.Fprintf(out, "exported %d templates, %d variants, %d boms to %s\n", len(f.Templates), count, len(f.BOMs), *dest)
	if omitted > 0 {
		fmt.Fprintf(out, "omitted %d archived variants without default_code\n", omitted)
	}
	if uncoded > 0 {
		if uncoded == 1 {
			fmt.Fprintln(out, "warning: 1 active variant without default_code was exported; products import rejects it by default")
		} else {
			fmt.Fprintf(out, "warning: %d active variants without default_code were exported; products import rejects them by default\n", uncoded)
		}
	}
	return nil
}

func runImport(ctx context.Context, section string, args []string, out io.Writer) error {
	var (
		flags        *flag.FlagSet
		target       *string
		in           *string
		dryRun       *bool
		emptyCode    *string
		saleOK       *string
		noCreateDeps *bool
		mapOut       *string
		client       *odoo.Client
		f            *CatalogFile
		snap         *Snapshot
		opt          importOptions
		products     bool
		plan         *Plan
		res          *Result
		problems     []string
		err          error
	)

	flags = flag.NewFlagSet("odoo-sync "+section+" import", flag.ContinueOnError)
	flags.SetOutput(out)
	target = flags.String("target", "", "connection file of the target Odoo")
	in = flags.String("in", "", "catalog file to import")
	dryRun = flags.Bool("dry-run", false, "print the plan and write nothing")
	emptyCode = flags.String("empty-code", emptyCodeReject, "variants without default_code: reject or skip")
	saleOK = flags.String("sale-ok", saleOKAll, "which templates to import: all or only (sale_ok)")
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
	if *emptyCode != emptyCodeReject && *emptyCode != emptyCodeSkip {
		return fmt.Errorf("-empty-code must be reject or skip, got %q", *emptyCode)
	}
	if *saleOK != saleOKAll && *saleOK != saleOKOnly {
		return fmt.Errorf("-sale-ok must be all or only, got %q", *saleOK)
	}
	if *dryRun && *mapOut != "" {
		return errors.New("-map-out cannot be used with -dry-run: nothing is created, so there is no mapping")
	}
	if section == "boms" && *mapOut != "" {
		return errors.New("-map-out needs products in the import")
	}
	if f, err = readCatalogFile(*in); err != nil {
		return err
	}
	if problems = f.Validate(); len(problems) > 0 {
		return reportProblems(out, problems)
	}
	if products, opt.BOMs, err = importSections(f, section); err != nil {
		return err
	}
	opt.NoProducts = !products
	opt.BOMs = opt.BOMs && len(f.BOMs) > 0 // nothing to reconcile, so the target's BOMs are not read
	if client, err = odoo.LoadClient(*target); err != nil {
		return err
	}
	if snap, err = loadSnapshot(ctx, client, opt.BOMs); err != nil {
		return err
	}
	opt.EmptyCode, opt.SaleOK, opt.NoCreateDeps = *emptyCode, *saleOK, *noCreateDeps
	plan, problems = buildPlan(f, snap, opt)
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
	switch {
	case res.Failed > 0 && res.BOMFailed > 0:
		return fmt.Errorf("%d templates and %d boms failed", res.Failed, res.BOMFailed)
	case res.Failed > 0:
		return fmt.Errorf("%d templates failed", res.Failed)
	case res.BOMFailed > 0:
		return fmt.Errorf("%d boms failed", res.BOMFailed)
	}
	return nil
}

// importSections says which section the command imports: the file must hold it.
func importSections(f *CatalogFile, section string) (products, boms bool, err error) {
	if !f.HasSection(section) {
		return false, false, fmt.Errorf("the file has no %s section; export it with odoo-sync %s export", section, section)
	}
	return section == "products", section == "boms", nil
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
	if r.BOMsRan {
		fmt.Fprintf(out, "boms: created %d, archived %d, code updated %d, unchanged %d, failed %d\n",
			r.BOMCreated, r.BOMArchived, r.BOMUpdated, r.BOMUnchanged, r.BOMFailed)
	}
}
