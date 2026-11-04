# Helpdesk SDK and CLI

## Scope and requirements

The core integration targets Odoo 19 JSON-2 and OCA `helpdesk_mgmt`. Configure an API user with
the required helpdesk, mail and attachment permissions through the usual `odoo.LoadClient` YAML
configuration. Odoo remains responsible for record rules, company access, stage automation,
email delivery and activity follow-ups.

The SDK covers core ticket reads and writes, lifecycle actions, category/tag management,
team/stage discovery, messages, attachments, followers and activities. It does not implement
optional SLA, rating, project, CRM or field-service modules, or administer email gateways and
automation rules. Those are separate integrations, not prerequisites for the core client.

Sales-order links require `helpdesk_mgmt_sale`; product links require `helpdesk_product`.
Core reads exclude these optional fields by default. Opt in with `TicketQuery.IncludeSaleOrders`
or `TicketQuery.IncludeProduct`; ID reads use `GetTicketWithQuery(ctx, id, query)`.
An unrequested association has its Go zero value, which does not mean the server has no links.
This differs from the initial client, which always requested both fields and failed if either
addon was missing. Writes specifying these relations still require their corresponding addon.

## Tickets and lifecycle

```sh
odoo helpdesk list --limit 100 --offset 0 --order 'id asc'
odoo helpdesk list --all --include-archived --domain '[["partner_id","=",15]]'
odoo helpdesk get --id 101 --include-sale-orders --include-product
odoo helpdesk count --include-archived
odoo helpdesk create --file examples/helpdesk/ticket.json
odoo helpdesk update --id 101 --file examples/helpdesk/ticket-patch.json
odoo helpdesk copy --id 101
odoo helpdesk archive --id 101
odoo helpdesk unarchive --id 101
odoo helpdesk delete --id 101
```

All actions accept `--config`; a flag after the action overrides a global `--config`.
Writes execute immediately. Delete actions permanently remove records; use archive for reversible
removal. All writes are attempted once. A transport error can leave a write's outcome unknown;
check the server before retrying it yourself.

`SearchTickets` supports Domain, Limit, Offset, Order and IncludeArchived. `AllTickets` iterates
pages until exhausted or the caller stops. `CountTicketsWithQuery` supports archived counts;
the existing `CountTickets(ctx, domain)` counts active tickets. `GetTicket` and
`GetTicketWithQuery` include archived records, as do write existence checks.
Pagination uses offsets and is not a snapshot if records change while pages are fetched.

`CreateTicketRequest` and `UpdateTicketRequest` expose common ticket fields, including contact,
company and workflow metadata. Patch pointers distinguish omission from an explicit zero or
empty value. A zero optional many-to-one ID clears the relation; an empty many-to-many list
replaces it with no links. IDs in nonempty lists must be positive. Required fields and Odoo
business constraints are still enforced by the server.

To close or reopen a ticket, select an appropriate stage from `ListTicketTeamStages` and update
`StageID`; the SDK does not guess which open or closed stage matches the team's workflow.

## Categories, tags and teams

```sh
odoo helpdesk categories
odoo helpdesk tags
odoo helpdesk stages
odoo helpdesk teams
odoo helpdesk channels
odoo helpdesk members --team-id 1
odoo helpdesk team-stages --team-id 1
odoo helpdesk create-category --file category.json
odoo helpdesk update-category --id 4 --file category-patch.json
odoo helpdesk delete-category --id 4
odoo helpdesk create-tag --file tag.json
odoo helpdesk update-tag --id 7 --file tag-patch.json
odoo helpdesk delete-tag --id 7
```

Category requests support names, hierarchy, company and ordering; tag requests support names and
colors. Update request fields are pointers. Team metadata exposes member IDs; member lookup returns
user names. Applicable-stage lookup includes shared stages and respects the team's company.
Stage and team creation/configuration remain Odoo administration operations.

## Messages and attachments

```sh
odoo helpdesk messages --id 101 --limit 50 --offset 0 --order 'id asc'
odoo helpdesk messages --id 101 --all
odoo helpdesk post --id 101 --file examples/helpdesk/message.json
odoo helpdesk attachments --id 101
odoo helpdesk upload --id 101 --input photo.png --mimetype image/png
odoo helpdesk download --id 101 --attachment-id 31 --output downloaded-photo.png
odoo helpdesk delete-attachment --id 101 --attachment-id 31
```

`SearchTicketMessages(ctx, id, MessageQuery)` supports pagination and an additional domain.
The ticket's model and ID always constrain the query. Existing `ListTicketMessages` returns the
whole visible thread. Internal notes are included when the API user has access; callers showing
messages to customers must apply visibility rules rather than expose an internal user's thread.

`PostTicketMessageWithOptions` returns the new message ID and accepts Body, Subject, Internal,
ParentID, PartnerIDs and AttachmentIDs. JSON names are `body`, `subject`, `internal`, `parent_id`,
`partner_ids` and `attachment_ids`. Upload attachments first, then use their IDs in the message.
An attachment-only message may omit the body. Parent messages and attachments must belong to the
same ticket. Odoo may normalize reply threading according to the model's mail-thread behavior.

Both public comments and internal notes use `message_type=comment`; the subtype is respectively
`mail.mt_comment` or `mail.mt_note`. HTML requires an internal Odoo API user; Odoo escapes HTML from
portal users. Choosing recipients does not bypass Odoo permissions or notification settings.

SDK upload/download methods accept and return raw bytes; JSON-2 base64 conversion is internal.
Attachment listings contain metadata without binary payloads or access tokens. Download and
delete check ownership by ticket. The CLI uses explicit input/output paths and does not overwrite
an existing download target. Binary data is written to the file; stdout contains JSON metadata.

## Followers and activities

```sh
odoo helpdesk followers --id 101
odoo helpdesk subscribe --id 101 --partner-ids 15,16
odoo helpdesk unsubscribe --id 101 --partner-ids 16
odoo helpdesk activity-types
odoo helpdesk activities --id 101
odoo helpdesk schedule --id 101 --file examples/helpdesk/activity.json
odoo helpdesk update-activity --id 101 --activity-id 25 --file activity-patch.json
odoo helpdesk complete-activity --id 101 --activity-id 25 --feedback 'Customer contacted'
odoo helpdesk delete-activity --id 101 --activity-id 25
```

Follower partner IDs are `res.partner` IDs, not user IDs. Subscribe optionally accepts
`--subtype-ids`; omission lets Odoo choose defaults. Activity `user_id` is a `res.users` ID.
Activity requests use `activity_type_id`, `user_id`, `summary`, `note` and `date_deadline`.
Omitted creation values use Odoo defaults. Explicit deadlines must be real `YYYY-MM-DD` dates.
Activity updates use pointers and require at least one field. Child mutations validate the
activity's model and ticket ID before invoking write, completion or deletion.

## Verification and references

Unit and CLI tests use local HTTP fixtures. They cover request payloads, optional false values,
ownership filters, invalid input, archived reads, binary encoding and the no-write-retry rule.
They do not replace acceptance testing against your installed addon versions and permissions.

- [OCA helpdesk 19.0](https://github.com/OCA/helpdesk/tree/19.0)
- [Odoo JSON-2 API](https://www.odoo.com/documentation/19.0/developer/reference/external_api.html)
- [Odoo mail thread](https://github.com/odoo/odoo/blob/19.0/addons/mail/models/mail_thread.py)
- [Odoo activity mixin](https://github.com/odoo/odoo/blob/19.0/addons/mail/models/mail_activity_mixin.py)
