# Odoo Sales Orders Implementation Plan

> **For agentic workers:** Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Query and create sales orders, and query product categories through the existing JSON-2 client.

**Architecture:** Add typed sales order methods to `pkg/odoo`, then expose them through a CLI using the existing local configuration. Share configuration loading between the product and order commands in `internal/odooconfig`.

**Tech Stack:** Go 1.22, standard library HTTP/JSON, existing go.yaml.in/yaml/v3 dependency for CLI configuration.

**Spec:** User request: “继续完成 pkg/odoo: 订单查询，订单创建”. Scope follows the ecommerce context: `sale.order`, list/detail queries, creation of draft quotations with product lines. Default configuration remains `configs/local.yaml`.

The user subsequently added “商品分类接口” and clarified “只需要做分类查询”. Support internal and website category lists/details and direct parent/child filtering, without category mutations.

## Global Constraints

- Preserve `NewClient` and `Call` and the product command.
- Query supports Odoo domain filters, limit, offset, and ordering.
- Creation sends `vals_list` with nested `[0, 0, values]` line commands in one request; never automatically retries or confirms.
- Keep YAML dependencies outside `pkg/odoo`; never print configuration secrets.
- Validate creation using local HTTP fixtures, without creating records in the configured live database.
- Workspace has no usable Git metadata; deliver changes in place without commits.

## Review Focus

- Many-to-one fields may be `[id, name]`, `false`, or `null`; reject malformed values.
- Missing unit price must be omitted; an explicit zero price must be preserved.
- Invalid IDs, nonpositive/nonfinite quantities and negative/nonfinite prices must fail before HTTP calls.
- Empty queries return empty lists; missing order details return a recognizable not-found error.
- Creation failures must not trigger retries or return a successful ID; malformed CLI input must not send requests.

## Task 1: Typed order API

Files: `pkg/odoo/orders.go`, `pkg/odoo/orders_test.go`.

Interfaces: `SearchSaleOrders(ctx, SaleOrderQuery) ([]SaleOrder, error)`, `GetSaleOrder(ctx, id) (*SaleOrder, error)`, `CreateSaleOrder(ctx, CreateSaleOrderRequest) (int, error)`.

- [x] Write HTTP fixture tests for query filters/pagination, detail lines, relational fields, creation payload/default prices, validation, missing records and remote errors.
- [x] Run tests and confirm missing API failures.
- [x] Implement types, methods, validation and wrapped errors.
- [x] Run package tests and confirm success.

## Task 2: Configured order CLI

Files: `internal/odooconfig/config.go`, `cmd/odoo-orders/main.go`, `cmd/odoo-orders/main_test.go`, `cmd/odoo-products/main.go`, `pkg/odoo/README.md`, `examples/sale-order.json`.

Interfaces: `odooconfig.LoadClient(path string) (*odoo.Client, error)`; CLI `list`, `get -id`, and `create -file` subcommands.

- [x] Add failing CLI tests for configuration, list/detail JSON, explicit create input and invalid input with no requests.
- [x] Extract existing configuration loader; reuse product command tests to verify compatibility.
- [x] Implement CLI and document package methods and runnable commands with sample input.
- [x] Run `go test -race ./...`, `go vet ./...`, formatting checks and CLI help; review final changes.

## Task 3: Product category queries (added by user)

Files: `pkg/odoo/categories.go`, `pkg/odoo/categories_test.go`, `cmd/odoo-categories/main.go`, `cmd/odoo-categories/main_test.go`, `pkg/odoo/README.md`.

Interfaces: `SearchProductCategories(ctx, ProductCategoryQuery) ([]ProductCategory, error)`, `GetProductCategory(ctx, CategoryType, id) (*ProductCategory, error)`.

- [x] Add failing tests for internal/website routing, pagination, root/child filtering, no mutation of caller domains, missing records, invalid arguments and HTTP errors.
- [x] Implement typed category queries using shared relation decoding.
- [x] Add failing CLI tests, then implement list/get using the shared local configuration.
- [x] Document runnable category examples and include all additions in the final verification and independent review.

## Execution record

- Task 1 API tests failed on absent methods, then passed with implementation.
- Task 2 CLI tests failed on absent command, then the entire suite passed after implementation; existing product tests verify the extracted configuration loader.
- Task 3 API and CLI tests failed on absent methods/command, then the entire suite passed.
- No live records were created. Configuration credentials remain unmodified.

- Independent read-only review found no critical/important issues. Reproduced and fixed explicit CLI `-parent-id -1` bypassing parent validation; omission remains the only way to disable that filter. Regression failed before the fix and passed afterward.
- Final verification: `go test -race -cover ./...` and `go vet ./...` passed; `pkg/odoo` statement coverage is 97.4%. Both new command help paths were run successfully.
