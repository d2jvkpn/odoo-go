# Repository Guidelines

## What this repo is

This Go 1.27 module, `github.com/d2jvkpn/odoo-go`, is a Go SDK for the Odoo JSON-2 API. It provides a
typed client (products, product categories, sales orders) and the `odoo-cli` command line tool.

## Project Layout

- `*.go` (repo root, package `odoo`): HTTP client, YAML configuration loading, sales orders,
  category queries, and relation decoding.
- `cmd/odoo-cli/`: unified `products`, `orders`, and `categories` subcommands. Keep argument parsing
  and output here; reusable API behavior belongs in the root `odoo` package.
- Tests live beside implementation files as `*_test.go`.
- `examples/`: sample request files (`product.json`, `sale-order.json`) and the matching CLI output
  (`*.response.json`); `README.md`: usage documentation.
- `configs/local.yaml`: local connection configuration. `docs/`: harness docs;
  `docs/records/superpowers/`: design specs and implementation plans.
- `target/`: generated build artifacts; excluded from version control.

## Commands

Run commands from the repository root:

```sh
go build -o target/odoo ./cmd/odoo-cli
go test -v -count=1 ./...
go test -race -cover ./...
go vet ./...
go run ./cmd/odoo-cli -h
go run ./cmd/odoo-cli categories list
```

These build the CLI into `target/`, run uncached verbose tests, check races and coverage, perform
static analysis, display help, and query configured categories. Keep build outputs in `target/`. If
Git metadata is unavailable, add `-buildvcs=false` to the build command.

## Coding Style & Naming Conventions

Use `gofmt` on changed Go files; it controls indentation and spacing. Use PascalCase for exported
identifiers, camelCase for private identifiers, and descriptive filenames such as `categories.go`.
Document exported APIs. Accept `context.Context` for network operations and wrap errors with `%w`.
Preserve Odoo JSON field names in struct tags and request bodies.

## Testing Guidelines

Use Go’s `testing` package with `testify` assertions and `net/http/httptest`; tests must not require
real credentials or create live orders. Name tests `Test<Behavior>` and examples
`Example<Type>_<Method>`. Cover request payloads, configuration, validation, empty results, and
remote errors. Run the complete suite after changes. No fixed coverage threshold is configured; add
meaningful regression coverage for changed behavior.

## Commit & Pull Request Guidelines

Git history is unavailable in this checkout, so existing commit conventions cannot be verified.
Prefer focused imperative subjects, optionally scoped, such as `fix(odoo): validate category parent
IDs`. PRs should describe the behavior change, link relevant issues, list validation commands, and
update affected CLI examples.

## Security & Configuration

Load `odoo.base_url`, `odoo.api_key`, and optional `odoo.db` through `odoo.LoadClient`. Never commit
or print credentials. Order creation writes draft quotations; transport failures can leave the
outcome uncertain, so do not automatically retry writes. Only read-only calls may use `CallRead`.

@./.agents/README.md

@./docs/README.md
