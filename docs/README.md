# Documentation Index

## Architecture

- The root package `odoo` is the SDK. `Client.Call` posts named parameters to
  `/json/2/<model>/<method>` with a bearer API key and optional `X-Odoo-Database` header; typed
  helpers (`SearchProducts`, `SearchProductCategories`, `SearchSaleOrders`, `SearchPartners`,
  `CreateSaleOrder`, `CreateProduct`, `CreateProductCategory`, `CreatePartner`, and their `Get*`
  counterparts) are built on it, plus `GetProductStock`, `ListStockLocations`, `ListCountries`,
  `ListPartnerTags`, `SetProductStock`, the `Update*`/`SetProductActive`/`SetPartnerActive` writes,
  `UpsertPartnerByRef`, `ConfirmSaleOrder`/`CancelSaleOrder`, and the variant helpers in
  `variants.go` (`AddProductVariants` and the attribute, value and line creators), and the
  read-only `OrderPickings`/`OrderInvoices`/`OrderPayments` in `fulfillment.go`, plus
  `ValidatePicking` in `picking.go`. Writes share the helpers in `write.go`; there is no exported
  generic write or delete, and only products and partners can be archived.
- `oca/helpdesk` wraps `*odoo.Client` with OCA core ticket lifecycle, metadata, attachment,
  message, follower and activity operations. Its typed delete methods are explicit and never
  retried. The `helpdesk` CLI exposes these operations. Core ticket reads work without the
  optional sale/product addons; callers opt into those fields. ID reads include archived tickets.
  Customer and agent comments use `message_type=comment`; the `mail.mt_comment`/`mail.mt_note`
  subtype distinguishes public replies from internal notes. See [helpdesk.md](helpdesk.md).
- `cmd/odoo-cli` only parses arguments and prints JSON; reusable API behavior belongs in the root
  package.
- `cmd/odoo-sync` exports products and BOMs from one Odoo database and imports them into another,
  matching variants by `default_code`. It uses `ListCatalogVariants`, `ListProductAttributes`,
  `ListUoMs`, `CreateProductTemplate`, `UpdateProductTemplate`, `SetProductTemplateActive`,
  `UpdateVariantFields`, `ListBOMs`, `CreateBOM`, `SetBOMActive` and `UpdateBOMCode` from the root
  package. See [catalog-sync.md](catalog-sync.md).
- `UpsertPartnerByRef` is not safe under concurrent calls for the same ref; the SDK deliberately
  adds no locking. See
  [the decision record](../.agents/decisions/2026-10-27_evol_partner-upsert-locking.md).
- Order reads request `delivery_status` (from `sale_stock`) and stock queries need the stock module,
  so the target database must have the sales and inventory modules installed. Product reads
  (`Get*`/`Search*`) always request `is_storable`, a field the `stock` module adds to
  `product.template`; without `stock` they fail with HTTP 500 "Invalid field 'is_storable'".
  Partner calls need only `base` (`res.partner`, `res.partner.category`); the Contacts app is not
  required, since it only adds menus and views.
- All JSON is encoded and decoded with `encoding/json/v2`, with no leniency options. Responses
  with invalid UTF-8, duplicate member names or trailing data fail with `ErrUnexpectedResponse`,
  and member names are case-sensitive. v2's `omitempty` judges the encoded value, so request and
  output struct fields that are numbers, bools, pointers or `ReplaceIDs` use `omitzero`;
  otherwise zero IDs would be sent and a pointer to `""` could not clear a field. `*ReplaceIDs`
  keeps `omitempty` so an empty list can still clear all tags. Tests may still use
  `encoding/json` as an independent reference.
- Configuration is loaded from YAML (`odoo.base_url`, `odoo.api_key`, optional `odoo.db`) by
  `odoo.LoadClient`; default path `configs/local.yaml`, which is not committed.
- Write calls such as order, product, and partner creation, updates, stock changes, order
  confirm/cancel, delivery validation, and the generic `Call` are never retried automatically.
  `Client.Retry` (or `odoo.max_retries`) opts read-only calls (`CallRead`, `Search*`, `Get*`,
  `GetProductStock`, `ListStockLocations`, `ListCountries`, `ListPartnerTags`) into retries of
  transient failures only.

## Index

- [README.md](../README.md): CLI and Go API usage, with examples for every endpoint.
- [helpdesk.md](helpdesk.md): core helpdesk SDK/CLI operations, inputs and module requirements.
- [catalog-sync.md](catalog-sync.md): `odoo-sync` export and import of products and BOMs between databases.
- [order-flow.md](order-flow.md): placing orders with several shipping addresses per customer, and
  creating child address contacts idempotently.
- [modules.md](modules.md): Odoo modules each feature depends on, by technical and Apps name.
- [manual-testing.md](manual-testing.md): CLI checks against a live Odoo server, read-only plus
  order, helpdesk and `odoo-sync` write tests for a test database.
- `records/` holds workflow output (plans, specs, reviews) and is deliberately not indexed.
