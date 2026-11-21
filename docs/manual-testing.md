# Manual Testing

Manual checks against a live Odoo server, for behavior the `httptest` fixtures cannot prove. Run
every command from the repository root. All commands are read-only except those under
[Order write tests](#order-write-tests), [Helpdesk tests](#helpdesk-tests) and the write steps of
[odoo-sync tests](#odoo-sync-tests); never run any write command (`create`, `update`, `upsert`,
`archive`, `stock set -yes`, `orders confirm|cancel -yes`, `odoo-sync import` without `-dry-run`,
and every `helpdesk` action except the reads listed there) against a database that holds real data,
because they write records and are never retried automatically.

## Prerequisites

- `configs/local.yaml` holds `odoo.base_url`, `odoo.api_key` and optional `odoo.db`. Do not print
  or commit the key.
- Confirm the server answers before testing the CLI. `200` or `303` is healthy; `Empty reply`
  means the server closed the connection and the CLI will fail the same way.

```sh
curl -sS -m 10 -o /dev/null -w "%{http_code}\n" http://192.168.1.149:8068/web/login
```

## Raw JSON-2 call

Separates server problems from SDK problems. Keep the key in an environment variable, and omit the
`X-Odoo-Database` header when `odoo.db` is not configured. The response should be a number.

```sh
export ODOO_KEY='<api_key>'
curl -sS -X POST http://192.168.1.149:8068/json/2/product.product/search_count \
  -H "Authorization: bearer $ODOO_KEY" -H "Content-Type: application/json" \
  -H "X-Odoo-Database: <db>" -d '{"domain": []}'
```

## CLI checks

```sh
go run ./cmd/odoo-cli products count
go run ./cmd/odoo-cli products list -limit 2
go run ./cmd/odoo-cli products list -limit 2 -offset 2 -order 'id asc'
go run ./cmd/odoo-cli products list -all -limit 50
go run ./cmd/odoo-cli orders count -domain '[["state","=","sale"]]'
go run ./cmd/odoo-cli orders list -all -limit 20
go run ./cmd/odoo-cli categories count
go run ./cmd/odoo-cli categories list -all -limit 20
go run ./cmd/odoo-cli partners list -limit 5
go run ./cmd/odoo-cli stock get -ids 1
go run ./cmd/odoo-cli stock locations
go run ./cmd/odoo-cli boms list          # needs the mrp module
go run ./cmd/odoo-cli boms list -archived
go run ./cmd/odoo-cli boms get -id 14
```

## Consistency checks

Expected results; any mismatch is a bug in paging or counting.

- `list -all` returns as many records as `count` for the same `-domain`.
- Two adjacent pages, `-limit 2 -offset 0` and `-limit 2 -offset 2` with a fixed `-order`, share
  no `id`.
- `list -all -limit 3` and `list -all -limit 1000` return identical arrays.
- An impossible filter returns `0` from `count` and `[]` from `list`:

```sh
go run ./cmd/odoo-cli products count -domain '[["id","=",-1]]'
go run ./cmd/odoo-cli products list -domain '[["id","=",-1]]'
```

## Order write tests

Run these only against a test database, never one that holds real data. Keep request files under
`data/tests/orders/`.

- Use a dedicated test partner, never the company's own contact (partner ID 1 in the current test
  database). Odoo gives the company's contact transit stock locations, so `orders confirm` fails
  with `No rule has been found to replenish ... in Inter-warehouse transit`. A partner created
  through `partners create` gets the default Customer Location (`Customers`) and confirms
  normally. The current test database has one: ID 552, `ref` `odoo-go-test`.
- Create the test partner if it is missing; look it up first, because `create` does not check for
  duplicates. `data/tests/orders/partner-test.json`:

```json
{"name": "odoo-go test customer", "ref": "odoo-go-test", "phone": "13800000000", "country_id": 48}
```

```sh
go run ./cmd/odoo-cli partners list -domain '[["ref","=","odoo-go-test"]]'
go run ./cmd/odoo-cli partners create -file data/tests/orders/partner-test.json
```

- Set `origin` to `odoo-go-test` on every test order, so they can be filtered and cleaned up in
  Odoo, and give each a unique `client_order_ref`.
- Each step reads back with `orders get -id ID`; compare `state`, line prices and `amount_total`:

```sh
go run ./cmd/odoo-cli orders create -file data/tests/orders/order.json
go run ./cmd/odoo-cli orders update -id ID -file data/tests/orders/patch.json
go run ./cmd/odoo-cli orders confirm -id ID -yes
go run ./cmd/odoo-cli orders cancel -id ID -yes
go run ./cmd/odoo-cli orders list -domain '[["origin","=","odoo-go-test"]]' -order id
```

- A confirmed order has `state` `sale` and `delivery_status` `pending`. Delivery, invoicing and
  payment are not covered by the SDK; check them in Odoo.
- The SDK cannot delete orders or set a partner's stock locations (shown only in Odoo developer
  mode). Clean up in the Odoo UI.

## odoo-sync tests

Needs two connection files (`odoo.base_url`, `odoo.api_key`, optional `odoo.db`): a source with real
products and a **test** target, empty or disposable. Both need the sales and inventory modules, and
BOMs need `mrp`. Keep files under `data/tests/sync/`. The unit tests use fakes, so only these steps
show how a real Odoo behaves.

Read-only steps (safe on any database as the source; `import -dry-run` writes nothing):

```sh
go build -o target/odoo-sync ./cmd/odoo-sync
target/odoo-sync export -source configs/source.yaml -out data/tests/sync/catalog.json
target/odoo-sync check  -in data/tests/sync/catalog.json
target/odoo-sync import -target configs/target.yaml -in data/tests/sync/catalog.json -dry-run
```

Expected: `check` prints `ok:` with the template, variant and BOM counts; exporting twice gives the
same file apart from `exported_at`; a dry run against the source itself proposes no product
changes. A source BOM with operations, byproducts or attribute-restricted lines makes the export
fail, naming the BOM. Without `mrp` the export fails; use `-include products` and note the HTTP
status Odoo returns (404 or 500), which [modules.md](modules.md) states as 500 and is unverified.

Strict mode stops on products or components without a `default_code`; confirm that the dry run
lists them all and that `-empty-code skip` skips them and says so.

Write steps, **test target only** (confirm the target before each run; the first import creates
records that the SDK cannot delete, so clean up in the Odoo UI):

1. `import ... -include products -map-out data/tests/sync/map.csv`. Check in Odoo: every template
   has all the variants the file lists, each `default_code` is set, archived variants are archived,
   and extra prices match the source. This is the only proof of how Odoo generates variants after
   attribute lines are added through JSON-2, and that `SetProductActive` archives a variant.
2. Run the same import again. Expected: no creates, updates or archives.
3. `import ... -include boms`. Check in Odoo: each BOM has the right product (variant or template),
   components, quantities and units. This proves the `CreateBOM` line commands.
4. Run it again; expected `0 to create, 0 to archive, 0 code updates` and all BOMs unchanged.
5. In the source, change a component quantity and a BOM `code`, export again and import. Expected:
   the changed BOM is created and the old one archived (never edited), and a code-only change
   updates the reference in place (`UpdateBOMCode`).
6. Try an import of `-include boms` into a target that lacks a product or component: it must list
   every missing code and write nothing.

Note any Odoo behavior that differs from [catalog-sync.md](catalog-sync.md) and fix the document.

## Helpdesk tests

Needs Odoo 19 with OCA `helpdesk_mgmt` installed; run it only against a test database. The
unit tests use fixtures, so the checks below are the only proof that your addon versions and the
API user's permissions work. Keep request files under `data/tests/helpdesk/`.

- Mark every test record so it can be found and cleaned up: give tickets a name starting with
  `odoo-go-test`, and use a dedicated test partner (see [Order write tests](#order-write-tests)).
- Unlike products, helpdesk deletes are real: `delete`, `delete-category`, `delete-tag`,
  `delete-attachment` and `delete-activity` remove records permanently. Use them only on records
  created by this test.

Read-only checks first; these write nothing. Each should print JSON without an error, and
`categories`, `stages` and `teams` should return at least the data configured in Odoo:

```sh
go run ./cmd/odoo-cli helpdesk count
go run ./cmd/odoo-cli helpdesk list --limit 2
go run ./cmd/odoo-cli helpdesk categories
go run ./cmd/odoo-cli helpdesk tags
go run ./cmd/odoo-cli helpdesk stages
go run ./cmd/odoo-cli helpdesk teams
go run ./cmd/odoo-cli helpdesk members --team-id 1
go run ./cmd/odoo-cli helpdesk team-stages --team-id 1
go run ./cmd/odoo-cli helpdesk activity-types
```

Optional addons: `helpdesk list --include-sale-orders` needs `helpdesk_mgmt_sale` and
`--include-product` needs `helpdesk_product`. A missing addon should fail only when its flag is
used; a plain `list` and `get` must still work.

Ticket lifecycle. Write the ticket to `data/tests/helpdesk/ticket.json` (based on
`examples/helpdesk/ticket.json`, with the test partner ID and a name starting `odoo-go-test`), then
run each step and read back with `helpdesk get --id ID`:

```sh
go run ./cmd/odoo-cli helpdesk create --file data/tests/helpdesk/ticket.json
go run ./cmd/odoo-cli helpdesk get --id ID
go run ./cmd/odoo-cli helpdesk update --id ID --file data/tests/helpdesk/patch.json
go run ./cmd/odoo-cli helpdesk copy --id ID
go run ./cmd/odoo-cli helpdesk archive --id ID
go run ./cmd/odoo-cli helpdesk list --domain '[["id","=",ID]]'
go run ./cmd/odoo-cli helpdesk list --domain '[["id","=",ID]]' --include-archived
go run ./cmd/odoo-cli helpdesk get --id ID
go run ./cmd/odoo-cli helpdesk unarchive --id ID
```

- After `update`, `get` shows only the patched fields changed; a pointer to an empty value clears
  the field.
- After `archive`, the plain `list` returns `[]`, while `--include-archived` and `get --id` still
  return the ticket.
- `copy` creates a second ticket; note its ID and clean it up as well.
- To close a ticket, pick a stage from `team-stages` and update `stage_id`; the SDK does not choose
  one.

Messages and attachments, on the ticket created above:

```sh
go run ./cmd/odoo-cli helpdesk post --id ID --file data/tests/helpdesk/message.json
go run ./cmd/odoo-cli helpdesk messages --id ID --all
go run ./cmd/odoo-cli helpdesk upload --id ID --input data/tests/helpdesk/photo.png --mimetype image/png
go run ./cmd/odoo-cli helpdesk attachments --id ID
go run ./cmd/odoo-cli helpdesk download --id ID --attachment-id AID --output data/tests/helpdesk/out.png
go run ./cmd/odoo-cli helpdesk delete-attachment --id ID --attachment-id AID
```

- Post once with `"internal": false` and once with `"internal": true`. Both appear in `messages`;
  in Odoo the first is a public reply (`mail.mt_comment`) and the second an internal note
  (`mail.mt_note`).
- The downloaded file must be byte-identical to the uploaded one (`cmp`). A second `download` to
  the same `--output` path must fail without overwriting.
- `attachments` lists metadata only, with no binary payload or access token.
- Use an attachment ID that belongs to another ticket; `download` and `delete-attachment` must
  refuse it.

Followers and activities. Use partner IDs (not user IDs) for followers and a user ID for the
activity:

```sh
go run ./cmd/odoo-cli helpdesk followers --id ID
go run ./cmd/odoo-cli helpdesk subscribe --id ID --partner-ids PID
go run ./cmd/odoo-cli helpdesk unsubscribe --id ID --partner-ids PID
go run ./cmd/odoo-cli helpdesk schedule --id ID --file data/tests/helpdesk/activity.json
go run ./cmd/odoo-cli helpdesk activities --id ID
go run ./cmd/odoo-cli helpdesk update-activity --id ID --activity-id AID --file data/tests/helpdesk/activity-patch.json
go run ./cmd/odoo-cli helpdesk complete-activity --id ID --activity-id AID --feedback 'done'
go run ./cmd/odoo-cli helpdesk delete-activity --id ID --activity-id AID
```

- `subscribe` then `followers` lists the partner; `unsubscribe` removes it.
- A completed activity leaves `activities` and appears as a message on the ticket.
- Passing the ID of an activity that belongs to another ticket must be rejected before any write.

Categories and tags are Odoo-wide configuration, so create throwaway ones with a distinctive name
and delete them when done:

```sh
go run ./cmd/odoo-cli helpdesk create-category --file data/tests/helpdesk/category.json
go run ./cmd/odoo-cli helpdesk update-category --id CID --file data/tests/helpdesk/category-patch.json
go run ./cmd/odoo-cli helpdesk delete-category --id CID
go run ./cmd/odoo-cli helpdesk create-tag --file data/tests/helpdesk/tag.json
go run ./cmd/odoo-cli helpdesk update-tag --id TID --file data/tests/helpdesk/tag-patch.json
go run ./cmd/odoo-cli helpdesk delete-tag --id TID
```

Finally clean up. `helpdesk delete --id ID` removes each ticket created or copied here, but it is
permanent; confirm the ID first with `helpdesk get --id ID`. The SDK cannot delete the test
partner; remove it in the Odoo UI if it was created only for this run.
