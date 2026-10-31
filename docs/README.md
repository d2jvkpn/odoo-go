# Documentation Index

## Architecture

- The root package `odoo` is the SDK. `Client.Call` posts named parameters to
  `/json/2/<model>/<method>` with a bearer API key and optional `X-Odoo-Database` header; typed
  helpers (`SearchProducts`, `SearchProductCategories`, `SearchSaleOrders`, `SearchPartners`,
  `CreateSaleOrder`, `CreateProduct`, `CreateProductCategory`, `CreatePartner`, and their `Get*`
  counterparts) are built on it, plus `GetProductStock`, `ListStockLocations`, `ListCountries`,
  `ListPartnerTags`, `SetProductStock`, the `Update*`/`SetProductActive` writes,
  `UpsertPartnerByRef`, and `ConfirmSaleOrder`/`CancelSaleOrder`. Writes share the helpers in
  `write.go`; there is no exported generic write or delete, and only products can be archived.
- `cmd/odoo-cli` only parses arguments and prints JSON; reusable API behavior belongs in the root
  package.
- `UpsertPartnerByRef` is not safe under concurrent calls for the same ref; the SDK deliberately
  adds no locking. See
  [the decision record](records/decisions/2026-10-27_evol_partner-upsert-locking.md).
- Order reads request `delivery_status` (from `sale_stock`) and stock queries need the stock module,
  so the target database must have the sales and inventory modules installed.
- Configuration is loaded from YAML (`odoo.base_url`, `odoo.api_key`, optional `odoo.db`) by
  `odoo.LoadClient`; default path `configs/local.yaml`, which is not committed.
- Write calls such as order, product, and partner creation, updates, stock changes, order
  confirm/cancel, and the generic `Call` are never retried automatically. `Client.Retry` (or
  `odoo.max_retries`) opts read-only calls (`CallRead`, `Search*`, `Get*`,
  `GetProductStock`, `ListStockLocations`, `ListCountries`, `ListPartnerTags`) into retries of
  transient failures only.

## Index

- [README.md](../README.md): CLI and Go API usage, with examples for every endpoint.
- [manual-testing.md](manual-testing.md): CLI checks against a live Odoo server, read-only plus
  order write tests for a test database.
- `records/` holds workflow output (plans, specs, reviews) and is deliberately not indexed.
