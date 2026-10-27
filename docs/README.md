# Documentation Index

## Architecture

- The root package `odoo` is the SDK. `Client.Call` posts named parameters to
  `/json/2/<model>/<method>` with a bearer API key and optional `X-Odoo-Database` header; typed
  helpers (`SearchProducts`, `SearchProductCategories`, `SearchSaleOrders`, `CreateSaleOrder`,
  `CreateProduct`, and their `Get*` counterparts) are built on it.
- `cmd/odoo-cli` only parses arguments and prints JSON; reusable API behavior belongs in the root
  package.
- Configuration is loaded from YAML (`odoo.base_url`, `odoo.api_key`, optional `odoo.db`) by
  `odoo.LoadClient`; default path `configs/local.yaml`, which is not committed.
- Write calls such as order and product creation and the generic `Call` are never retried
  automatically. `Client.Retry` (or `odoo.max_retries`) opts read-only calls (`CallRead`,
  `Search*`, `Get*`) into retries of transient failures only.

## Index

- [README.md](../README.md): CLI and Go API usage, with examples for every endpoint.
- [manual-testing.md](manual-testing.md): read-only CLI checks against a live Odoo server.
- `records/` holds workflow output (plans, specs, reviews) and is deliberately not indexed.
