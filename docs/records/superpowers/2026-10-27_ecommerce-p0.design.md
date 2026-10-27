# E-commerce P0 API Design

## Goal

Extend the SDK so an e-commerce backend can run the core loop: browse, place an order, and
confirm it after payment. Scope is P0 only. Fulfilment, invoicing, payment, pricelists, and
variants (P1/P2) are out of scope.

Assumptions: Odoo 19+ (JSON-2 API) with the sales, inventory, and accounting modules installed.
Existing conventions stay in force: reads use `CallRead` and may retry, writes never retry, the
CLI only parses arguments and prints JSON.

## Approach

Typed methods per capability, one file per model, matching `products.go` and `orders.go`. No
exported generic `Write` or `Unlink`: an exported generic write would allow arbitrary field
writes without validation. Package-private write helpers are shared internally.

## Scope and files

| File | Methods | Kind |
|------|---------|------|
| `partners.go` | `SearchPartners`, `GetPartner`, `CreatePartner`, `UpdatePartner`, `UpsertPartnerByRef` | read, write |
| `stock.go` | `GetProductStock(ids)` returning `qty_available`, `virtual_available`, `free_qty` | read |
| `orders.go` | `ConfirmSaleOrder`, `CancelSaleOrder`; `SaleOrder` gains `invoice_status`, `delivery_status` | write, read |
| `products.go`, `categories.go` | `UpdateProduct`, `SetProductActive`, `UpdateProductCategory` | write |

Decisions:

- Stock is queried in bulk by product ID with a single `search_read` on `id in ids`; `read` would
  raise on an unknown ID instead of omitting it.
- `Update*` takes a patch struct with pointer fields; nil means "leave unchanged", consistent with
  `CreateSaleOrderLine.UnitPrice`.
- Deletion is archive only (`active=false`) and applies to products. Neither category model has an
  `active` field, so categories get update only. There is no `unlink`.

## Behavior

### Partner upsert

- `UpsertPartnerByRef` stores the e-commerce user ID in `res.partner.ref`. It searches by `ref`
  (a read, retryable), then writes on a hit or creates on a miss (neither retried).
- Returns `(id int, created bool, err error)`.
- Concurrent callers with the same ref can both miss and create duplicates, because Odoo does not
  enforce a unique `ref`. The SDK does not hide this; the docs state that callers must serialize
  per ref or add a unique constraint in Odoo.
- More than one match returns `ErrAmbiguousPartner`; the SDK never picks one.

### Order confirm and cancel

- `ConfirmSaleOrder` calls `action_confirm`; `CancelSaleOrder` calls `action_cancel` with the
  context `disable_cancel_warning=true` (the underscore-prefixed `_action_cancel` is private and
  not callable over JSON-2, and without the context Odoo may return a wizard action instead of
  cancelling). Neither retries.
- A pre-check reads `state` first. Confirm requires `draft` or `sent`; cancel requires a state
  other than `cancel`. Otherwise `ErrInvalidOrderState` is returned and no write is sent.
- The gap between the pre-check and the write is accepted; Odoo's server-side validation is
  authoritative. The pre-check only yields clearer errors.
- A transport failure leaves the outcome uncertain. The error keeps the `transportError`, and the
  method docs tell callers to `GetSaleOrder` and inspect `state` before retrying.
- Odoo refusals (for example cancelling a delivered or invoiced order) pass through as
  `HTTPError`.

### Stock

- IDs that do not exist are omitted from the result, not reported as errors.
- An empty ID list returns an empty result without a request.
- More than 200 IDs returns `ErrInvalidArgument`.

### Update and archive

- A zero value for a `*_id` relation is sent as `false`, Odoo's way to clear it (an integer 0
  corrupts category parent paths). A product's `categ_id` is required, so it must be positive.
- A patch with every field nil returns `ErrInvalidArgument` and sends nothing.
- Odoo's `write` succeeds silently on a missing ID, so update first reads the record and returns
  `ErrXxxNotFound` when it is absent, at the cost of one extra request.

### New errors

`ErrPartnerNotFound`, `ErrAmbiguousPartner`, `ErrInvalidOrderState`.

## CLI

| Command | Notes |
|---------|-------|
| `partners list/get/create/update/upsert` | `upsert` takes `--ref`; output includes `created` |
| `stock get --ids 1,2,3` | bulk stock query |
| `orders confirm -id N`, `orders cancel -id N` | requires `--yes`; without it, print the action only |
| `products update/archive/unarchive`, `categories update` | input via `-file` JSON |

`--yes` guards against accidental side effects from a terminal; the SDK layer is unaffected.
New sample files: `examples/partner.json` and `examples/partner.response.json`, checked by the
existing `example_files_test.go` mechanism.

## Testing

`httptest` with `testify`; no real credentials. Each group covers:

- Request payload: model, method, and parameter fields.
- Validation: invalid IDs, empty patch, batch limit exceeded.
- Empty results: missing stock IDs, searches with no hits.
- Remote errors: HTTP 4xx and 5xx passed through.
- Retry boundary: write methods issue exactly one request on transport failure; read methods
  follow `Retry`.
- State pre-check: no write request is sent in an invalid state (asserted by request count).
- Upsert branches: hit, miss, and multiple matches.

## Docs

Update `README.md` usage and the `docs/README.md` architecture section (new methods and the list
of never-retried writes). `docs/manual-testing.md` gains read-only checks only.
