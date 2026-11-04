package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"

	odoo "github.com/d2jvkpn/odoo-go"
	"github.com/d2jvkpn/odoo-go/oca/helpdesk"
)

const helpdeskUsage = `Usage:
  odoo helpdesk list [--domain '[]'] [--limit 100] [--offset 0] [--order 'id desc'] [--all] [--include-archived]
  odoo helpdesk count [--domain '[]'] [--include-archived]
  odoo helpdesk get --id ID
  odoo helpdesk create --file ticket.json
  odoo helpdesk update --id ID --file patch.json
  odoo helpdesk archive|unarchive|delete --id ID
  odoo helpdesk copy --id ID [--file defaults.json]
  odoo helpdesk stages|teams|channels|categories|tags
  odoo helpdesk members|team-stages --team-id ID
  odoo helpdesk create-category|create-tag --file record.json
  odoo helpdesk update-category|update-tag --id ID --file patch.json
  odoo helpdesk delete-category|delete-tag --id ID
  odoo helpdesk messages --id ID [--limit 100 | --all] [--offset 0] [--order 'id asc'] [--domain '[]']
  odoo helpdesk post --id ID --file message.json
  odoo helpdesk attachments --id ID
  odoo helpdesk upload --id ID --input path [--name filename] [--mimetype type]
  odoo helpdesk download --id ID --attachment-id ID --output path
  odoo helpdesk delete-attachment --id ID --attachment-id ID
  odoo helpdesk followers --id ID
  odoo helpdesk subscribe|unsubscribe --id ID --partner-ids 1,2 [--subtype-ids 1,2]
  odoo helpdesk activities --id ID
  odoo helpdesk activity-types
  odoo helpdesk schedule --id ID --file activity.json
  odoo helpdesk update-activity --id ID --activity-id ID --file patch.json
  odoo helpdesk complete-activity --id ID --activity-id ID [--feedback text]
  odoo helpdesk delete-activity --id ID --activity-id ID

All actions accept --config configs/local.yaml. Use -h after an action for its flags.
list/get accept --include-sale-orders and --include-product for installed optional OCA addons.
ID reads include archived tickets. Write actions execute immediately and are never retried.
delete actions permanently remove records. download requires a new output path and never overwrites.
JSON request fields use Odoo names. post accepts body, subject, internal, parent_id, partner_ids,
and attachment_ids. subtype-ids is only available for subscribe.
`

type helpdeskOptions struct {
	action, config, domain, file, input, output, name, mimetype string
	partnerText, subtypeText, feedback                          string
	id, teamID, attachmentID, activityID                        int
	partnerIDs, subtypeIDs                                      []int
	query                                                       helpdesk.TicketQuery
	all                                                         bool
}

type helpdeskIDResult struct {
	ID int `json:"id"`
}

func runHelpdesk(ctx context.Context, args []string, out io.Writer) error {
	var (
		opts   helpdeskOptions
		flags  *flag.FlagSet
		input  helpdeskInput
		base   *odoo.Client
		result any
		err    error
	)

	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		_, err = io.WriteString(out, helpdeskUsage)
		return err
	}
	opts.action = args[0]
	flags = flag.NewFlagSet("odoo helpdesk "+opts.action, flag.ContinueOnError)
	flags.SetOutput(out)
	flags.StringVar(&opts.config, "config", "configs/local.yaml", "path to YAML configuration")
	if err = helpdeskFlags(flags, &opts); err != nil {
		return err
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
	if err = validateHelpdeskOptions(&opts); err != nil {
		return err
	}
	if input, err = readHelpdeskInput(opts); err != nil {
		return err
	}
	if base, err = odoo.LoadClient(opts.config); err != nil {
		return err
	}
	result, err = executeHelpdesk(ctx, helpdesk.NewClient(base), opts, input)
	if err != nil {
		return err
	}
	return printJSON(out, result)
}

func helpdeskFlags(flags *flag.FlagSet, opts *helpdeskOptions) error {
	switch opts.action {
	case "list", "messages":
		flags.IntVar(&opts.query.Limit, "limit", 100, "maximum records per page")
		flags.IntVar(&opts.query.Offset, "offset", 0, "records to skip")
		flags.StringVar(&opts.query.Order, "order", "", "Odoo sort expression")
		flags.StringVar(&opts.domain, "domain", "[]", "Odoo domain as a JSON array")
		if opts.action == "list" {
			flags.BoolVar(&opts.all, "all", false, "fetch all pages; limit becomes the page size")
		} else {
			flags.BoolVar(&opts.all, "all", false, "fetch every message; limit is ignored")
			flags.IntVar(&opts.id, "id", 0, "ticket ID")
		}
	case "count":
		flags.StringVar(&opts.domain, "domain", "[]", "Odoo domain as a JSON array")
	case "stages", "teams", "channels", "categories", "tags", "activity-types":
	case "members", "team-stages":
		flags.IntVar(&opts.teamID, "team-id", 0, "helpdesk team ID")
	case "create", "create-category", "create-tag":
		flags.StringVar(&opts.file, "file", "", "JSON request file")
	case "get", "archive", "unarchive", "delete", "attachments", "followers", "activities",
		"delete-category", "delete-tag":
		flags.IntVar(&opts.id, "id", 0, "record ID")
	case "update", "copy", "post", "schedule", "update-category", "update-tag":
		flags.IntVar(&opts.id, "id", 0, "record ID")
		flags.StringVar(&opts.file, "file", "", "JSON request file (optional for copy)")
	case "subscribe", "unsubscribe":
		flags.IntVar(&opts.id, "id", 0, "ticket ID")
		flags.StringVar(&opts.partnerText, "partner-ids", "", "comma-separated partner IDs")
		if opts.action == "subscribe" {
			flags.StringVar(&opts.subtypeText, "subtype-ids", "", "comma-separated message subtype IDs")
		}
	case "upload":
		flags.IntVar(&opts.id, "id", 0, "ticket ID")
		flags.StringVar(&opts.input, "input", "", "local file to upload")
		flags.StringVar(&opts.name, "name", "", "attachment name (default: input filename)")
		flags.StringVar(&opts.mimetype, "mimetype", "", "MIME type (default: detected from contents)")
	case "download", "delete-attachment":
		flags.IntVar(&opts.id, "id", 0, "ticket ID")
		flags.IntVar(&opts.attachmentID, "attachment-id", 0, "attachment ID belonging to the ticket")
		if opts.action == "download" {
			flags.StringVar(&opts.output, "output", "", "new file to write; existing paths are rejected")
		}
	case "update-activity", "complete-activity", "delete-activity":
		flags.IntVar(&opts.id, "id", 0, "ticket ID")
		flags.IntVar(&opts.activityID, "activity-id", 0, "activity ID belonging to the ticket")
		if opts.action == "update-activity" {
			flags.StringVar(&opts.file, "file", "", "JSON patch file")
		}
		if opts.action == "complete-activity" {
			flags.StringVar(&opts.feedback, "feedback", "", "completion feedback")
		}
	default:
		return fmt.Errorf("unknown helpdesk action %q; use -h for help", opts.action)
	}
	if opts.action == "list" || opts.action == "count" {
		flags.BoolVar(&opts.query.IncludeArchived, "include-archived", false, "include inactive tickets")
	}
	if opts.action == "list" || opts.action == "get" {
		flags.BoolVar(&opts.query.IncludeSaleOrders, "include-sale-orders", false, "read helpdesk_mgmt_sale fields")
		flags.BoolVar(&opts.query.IncludeProduct, "include-product", false, "read helpdesk_product fields")
	}
	return nil
}

func validateHelpdeskOptions(opts *helpdeskOptions) error {
	var err error

	switch opts.action {
	case "list", "count", "stages", "teams", "channels", "categories", "tags", "activity-types",
		"create", "create-category", "create-tag":
	case "members", "team-stages":
		if opts.teamID <= 0 {
			return fmt.Errorf("team-id must be positive")
		}
	default:
		if opts.id <= 0 {
			return fmt.Errorf("id must be positive")
		}
	}
	if opts.action == "list" || opts.action == "messages" {
		if (opts.query.Limit <= 0 && !(opts.all && opts.action == "messages")) || opts.query.Offset < 0 {
			return fmt.Errorf("limit must be positive and offset nonnegative")
		}
	}
	if opts.action == "list" || opts.action == "messages" || opts.action == "count" {
		if opts.query.Domain, err = parseDomain(opts.domain); err != nil {
			return err
		}
	}
	if opts.action == "download" || opts.action == "delete-attachment" {
		if opts.attachmentID <= 0 {
			return fmt.Errorf("attachment-id must be positive")
		}
	}
	if opts.action == "update-activity" || opts.action == "complete-activity" || opts.action == "delete-activity" {
		if opts.activityID <= 0 {
			return fmt.Errorf("activity-id must be positive")
		}
	}
	if opts.action == "subscribe" || opts.action == "unsubscribe" {
		if opts.partnerIDs, err = helpdeskIDs(opts.partnerText); err != nil {
			return fmt.Errorf("partner-ids: %w", err)
		}
		if opts.subtypeText != "" {
			if opts.subtypeIDs, err = helpdeskIDs(opts.subtypeText); err != nil {
				return fmt.Errorf("subtype-ids: %w", err)
			}
		}
	}
	return nil
}

func helpdeskIDs(value string) ([]int, error) {
	var (
		ids []int
		id  int
		err error
	)
	for _, item := range strings.Split(value, ",") {
		if id, err = strconv.Atoi(strings.TrimSpace(item)); err != nil || id <= 0 {
			return nil, fmt.Errorf("IDs must be positive integers separated by commas")
		}
		ids = append(ids, id)
	}
	return ids, nil
}
