// Command odoo queries products and categories and manages sales quotations.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"iter"
	"os"
	"os/signal"
)

const usage = `Usage:
  odoo [-config configs/local.yaml] products list|get|count|create|update|archive|unarchive [options]
  odoo [-config configs/local.yaml] orders list|get|count|create|update|confirm|cancel [options]
  odoo [-config configs/local.yaml] categories list|get|count|create|update [options]
  odoo [-config configs/local.yaml] partners list|get|create|update|upsert [options]
  odoo [-config configs/local.yaml] stock get|locations|set [options]

Use -h after a command to see its options.
-config may appear before the command group or after the action.
An action-level -config overrides the global value.
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
	var (
		flags      *flag.FlagSet
		configPath *string
		command    func(context.Context, []string, io.Writer) error
		subArgs    []string
		err        error
	)

	flags = flag.NewFlagSet("odoo", flag.ContinueOnError)
	flags.SetOutput(out)
	configPath = flags.String("config", "configs/local.yaml", "path to YAML configuration")
	flags.Usage = func() { fmt.Fprint(out, usage) }
	if err = flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	args = flags.Args()
	if len(args) == 0 {
		_, err = io.WriteString(out, usage)
		return err
	}
	switch args[0] {
	case "products":
		command = runProducts
	case "orders":
		command = runOrders
	case "categories":
		command = runCategories
	case "partners":
		command = runPartners
	case "stock":
		command = runStock
	default:
		return fmt.Errorf("unknown command %q; use -h for help", args[0])
	}
	subArgs = args[1:]
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "config" && len(subArgs) != 0 {
			subArgs = append([]string{subArgs[0], "-config", *configPath}, subArgs[1:]...)
		}
	})
	return command(ctx, subArgs, out)
}

// countResult is the JSON output of the count commands.
type countResult struct {
	Count int `json:"count"`
}

// collect drains a paginated iterator into a slice, stopping at the first error.
func collect[T any](seq iter.Seq2[T, error]) ([]T, error) {
	var (
		items = make([]T, 0)
		item  T
		err   error
	)

	for item, err = range seq {
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

// parseDomain decodes a Odoo domain given as a JSON array.
func parseDomain(text string) ([]any, error) {
	var (
		domain []any
		err    error
	)

	if err = json.Unmarshal([]byte(text), &domain); err != nil || domain == nil {
		return nil, fmt.Errorf("domain must be a JSON array")
	}
	return domain, nil
}
