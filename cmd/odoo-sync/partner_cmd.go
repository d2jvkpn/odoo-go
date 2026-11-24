package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/d2jvkpn/odoo-go"
)

// runPartners runs one command of the partners group.
func runPartners(ctx context.Context, args []string, out io.Writer) error {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		_, err := io.WriteString(out, usage)
		return err
	}
	if len(args) == 0 {
		return errors.New("odoo-sync partners: missing command; use export, check or import")
	}
	switch args[0] {
	case "export":
		return runPartnersExport(ctx, args[1:], out)
	case "check":
		return runPartnersCheck(ctx, args[1:], out)
	case "import":
		return runPartnersImport(ctx, args[1:], out)
	default:
		return fmt.Errorf("unknown command %q for partners; use -h for help", args[0])
	}
}

func runPartnersExport(ctx context.Context, args []string, out io.Writer) error {
	var (
		flags        = flag.NewFlagSet("odoo-sync partners export", flag.ContinueOnError)
		source       = flags.String("source", "", "connection file of the source Odoo")
		dest         = flags.String("out", "", "partners file to write")
		inferCountry = flags.Bool("infer-country-from-state", false, "fill a missing country from the state record")
		client       *odoo.Client
		f            *PartnersFile
		warnings     []string
		err          error
	)

	flags.SetOutput(out)
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
	if f, warnings, err = buildPartnerExport(ctx, client, *source, client.BaseURL, time.Now(), *inferCountry); err != nil {
		return err
	}
	if err = writePartnersFile(*dest, f); err != nil {
		return err
	}
	for _, w := range warnings {
		fmt.Fprintln(out, "warning:", w)
	}
	fmt.Fprintf(out, "exported %d partners to %s\n", len(f.Partners), *dest)
	return nil
}

func runPartnersCheck(ctx context.Context, args []string, out io.Writer) error {
	var (
		flags        = flag.NewFlagSet("odoo-sync partners check", flag.ContinueOnError)
		in           = flags.String("in", "", "partners file")
		target       = flags.String("target", "", "optional target connection file for import preflight")
		emptyKey     = flags.String("empty-key", emptyKeyReject, "top-level partners without a key: reject or skip")
		noCreateDeps = flags.Bool("no-create-deps", false, "fail if a target tag is missing; requires -target")
		f            *PartnersFile
		client       *odoo.Client
		snap         *PartnerSnapshot
		problems     []string
		err          error
	)

	flags.SetOutput(out)
	if err = flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if *in == "" {
		return errors.New("-in is required")
	}
	if *emptyKey != emptyKeyReject && *emptyKey != emptyKeySkip {
		return fmt.Errorf("-empty-key must be reject or skip, got %q", *emptyKey)
	}
	if *noCreateDeps && *target == "" {
		return errors.New("-no-create-deps requires -target")
	}
	if f, err = readPartnersFile(*in); err != nil {
		return err
	}
	if problems = f.Validate(); len(problems) > 0 {
		return reportProblems(out, problems)
	}
	printPartnerStats(out, f)
	fmt.Fprintln(out, "\nWarnings")
	if problems = partnerPolicyProblems(f, *emptyKey); len(problems) > 0 {
		return reportProblems(out, problems)
	}
	if *target != "" {
		if client, err = odoo.LoadClient(*target); err != nil {
			return err
		}
		if snap, err = loadPartnerSnapshot(ctx, client, f); err != nil {
			return err
		}
		if _, problems = buildPartnerPlan(f, snap, partnerOptions{
			EmptyKey: *emptyKey, NoCreateDeps: *noCreateDeps,
		}); len(problems) > 0 {
			return reportProblems(out, problems)
		}
	}
	if *target == "" {
		fmt.Fprintf(out, "ok: %d partners (file checks only; use -target for import preflight)\n", len(f.Partners))
	} else {
		fmt.Fprintf(out, "ok: %d partners (target preflight passed; nothing was written)\n", len(f.Partners))
	}
	return nil
}

func runPartnersImport(ctx context.Context, args []string, out io.Writer) error {
	var (
		flags        = flag.NewFlagSet("odoo-sync partners import", flag.ContinueOnError)
		target       = flags.String("target", "", "connection file of the target Odoo")
		in           = flags.String("in", "", "partners file to import")
		dryRun       = flags.Bool("dry-run", false, "print the plan and write nothing")
		emptyKey     = flags.String("empty-key", emptyKeyReject, "top-level partners without ref, vat and email: reject or skip")
		noCreateDeps = flags.Bool("no-create-deps", false, "fail instead of creating missing tags")
		client       *odoo.Client
		f            *PartnersFile
		snap         *PartnerSnapshot
		plan         *PartnerPlan
		res          *PartnerResult
		problems     []string
		err          error
	)

	flags.SetOutput(out)
	if err = flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if *target == "" || *in == "" {
		return errors.New("-target and -in are required")
	}
	if *emptyKey != emptyKeyReject && *emptyKey != emptyKeySkip {
		return fmt.Errorf("-empty-key must be reject or skip, got %q", *emptyKey)
	}
	if f, err = readPartnersFile(*in); err != nil {
		return err
	}
	if problems = f.Validate(); len(problems) > 0 {
		return reportProblems(out, problems)
	}
	if client, err = odoo.LoadClient(*target); err != nil {
		return err
	}
	if snap, err = loadPartnerSnapshot(ctx, client, f); err != nil {
		return err
	}
	if plan, problems = buildPartnerPlan(f, snap, partnerOptions{EmptyKey: *emptyKey, NoCreateDeps: *noCreateDeps}); len(problems) > 0 {
		return reportProblems(out, problems)
	}
	if *dryRun {
		printPartnerPlan(out, plan)
		return nil
	}
	res = applyPartnerPlan(ctx, client, snap, plan)
	for _, w := range res.Warnings {
		fmt.Fprintln(out, "warning:", w)
	}
	for _, failure := range res.Failures {
		fmt.Fprintln(out, "failed:", failure)
	}
	fmt.Fprintf(out, "created %d, updated %d, unchanged %d, skipped %d, archived %d, restored %d, failed %d\n",
		res.Created, res.Updated, res.Unchanged, res.Skipped, res.Archived, res.Restored, res.Failed)
	if res.Failed > 0 {
		return fmt.Errorf("%d partner operations failed", res.Failed)
	}
	return nil
}
