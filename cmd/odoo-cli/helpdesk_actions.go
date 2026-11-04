package main

import (
	"context"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"

	"github.com/d2jvkpn/odoo-go/oca/helpdesk"
)

func executeHelpdesk(
	ctx context.Context,
	c *helpdesk.Client,
	opts helpdeskOptions,
	input helpdeskInput,
) (any, error) {
	var (
		count int
		id    = opts.id
		err   error
	)
	switch opts.action {
	case "list":
		if opts.all {
			return collect(c.AllTickets(ctx, opts.query))
		}
		return c.SearchTickets(ctx, opts.query)
	case "get":
		return c.GetTicketWithQuery(ctx, id, opts.query)
	case "count":
		count, err = c.CountTicketsWithQuery(ctx, opts.query)
		return countResult{Count: count}, err
	case "stages":
		return c.ListTicketStages(ctx)
	case "teams":
		return c.ListTicketTeams(ctx)
	case "channels":
		return c.ListTicketChannels(ctx)
	case "categories":
		return c.ListTicketCategories(ctx)
	case "tags":
		return c.ListTicketTags(ctx)
	case "members":
		return c.ListTicketTeamMembers(ctx, opts.teamID)
	case "team-stages":
		return c.ListTicketTeamStages(ctx, opts.teamID)
	case "messages":
		limit := opts.query.Limit
		if opts.all {
			limit = 0
		}
		return c.SearchTicketMessages(ctx, id, helpdesk.MessageQuery{
			Limit: limit, Offset: opts.query.Offset, Order: opts.query.Order, Domain: opts.query.Domain,
		})
	case "attachments":
		return c.ListTicketAttachments(ctx, id)
	case "followers":
		return c.ListTicketFollowers(ctx, id)
	case "activities":
		return c.ListTicketActivities(ctx, id)
	case "activity-types":
		return c.ListTicketActivityTypes(ctx)
	case "create":
		id, err = c.CreateTicket(ctx, input.create)
	case "update":
		err = c.UpdateTicket(ctx, id, input.patch)
	case "copy":
		id, err = c.CopyTicket(ctx, id, input.patch)
	case "archive", "unarchive":
		err = c.SetTicketActive(ctx, id, opts.action == "unarchive")
	case "delete":
		err = c.DeleteTicket(ctx, id)
	case "create-category":
		id, err = c.CreateTicketCategory(ctx, input.category)
	case "update-category":
		err = c.UpdateTicketCategory(ctx, id, input.categoryPatch)
	case "delete-category":
		err = c.DeleteTicketCategory(ctx, id)
	case "create-tag":
		id, err = c.CreateTicketTag(ctx, input.tag)
	case "update-tag":
		err = c.UpdateTicketTag(ctx, id, input.tagPatch)
	case "delete-tag":
		err = c.DeleteTicketTag(ctx, id)
	case "post":
		id, err = c.PostTicketMessageWithOptions(ctx, id, input.message)
	case "subscribe":
		err = c.SubscribeTicket(ctx, id, opts.partnerIDs, opts.subtypeIDs)
	case "unsubscribe":
		err = c.UnsubscribeTicket(ctx, id, opts.partnerIDs)
	case "schedule":
		id, err = c.ScheduleTicketActivity(ctx, id, input.activity)
	case "update-activity":
		err = c.UpdateTicketActivity(ctx, id, opts.activityID, input.activityPatch)
		id = opts.activityID
	case "complete-activity":
		err = c.CompleteTicketActivity(ctx, id, opts.activityID, opts.feedback)
		id = opts.activityID
	case "delete-activity":
		err = c.DeleteTicketActivity(ctx, id, opts.activityID)
		id = opts.activityID
	case "upload":
		if opts.name == "" {
			opts.name = filepath.Base(opts.input)
		}
		if opts.mimetype == "" {
			opts.mimetype = detectMimeType(opts.name, input.data)
		}
		id, err = c.UploadTicketAttachment(ctx, id, opts.name, opts.mimetype, input.data)
	case "download":
		return downloadHelpdeskAttachment(ctx, c, opts)
	case "delete-attachment":
		err = c.DeleteTicketAttachment(ctx, id, opts.attachmentID)
		id = opts.attachmentID
	default:
		return nil, fmt.Errorf("unknown helpdesk action %q", opts.action)
	}
	return helpdeskIDResult{ID: id}, err
}

func downloadHelpdeskAttachment(ctx context.Context, c *helpdesk.Client, opts helpdeskOptions) (any, error) {
	var (
		data []byte
		file *os.File
		err  error
	)
	if data, err = c.DownloadTicketAttachment(ctx, opts.id, opts.attachmentID); err != nil {
		return nil, err
	}
	if file, err = os.OpenFile(opts.output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600); err != nil {
		return nil, fmt.Errorf("create output file: %w", err)
	}
	if _, err = file.Write(data); err != nil {
		file.Close()
		os.Remove(opts.output)
		return nil, fmt.Errorf("write attachment: %w", err)
	}
	if err = file.Close(); err != nil {
		os.Remove(opts.output)
		return nil, fmt.Errorf("close attachment: %w", err)
	}
	return struct {
		ID     int    `json:"id"`
		Output string `json:"output"`
		Size   int    `json:"size"`
	}{ID: opts.attachmentID, Output: opts.output, Size: len(data)}, nil
}

// detectMimeType returns a bare media type: the extension wins, else the sniffed type without
// parameters such as charset.
func detectMimeType(name string, data []byte) string {
	var (
		err  error
		kind string
	)

	if kind, _, err = mime.ParseMediaType(mime.TypeByExtension(filepath.Ext(name))); err == nil {
		return kind
	}
	if kind, _, err = mime.ParseMediaType(http.DetectContentType(data)); err == nil {
		return kind
	}
	return "application/octet-stream"
}
