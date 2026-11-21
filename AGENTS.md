# Repository Guidelines

## What this repo is

This Go 1.27 module, `github.com/d2jvkpn/odoo-go`, is a Go SDK for the Odoo JSON-2 API. It provides
a typed client for products and variants, product categories, sales orders and their fulfillment,
partners, countries, stock and bills of materials, plus helpdesk tickets through the OCA addon. It
ships two command line tools: `odoo-cli` for querying and writing records, and `odoo-sync` for
copying products and BOMs between databases.

## Project Layout

```text
.
├── AGENTS.md             — coding-agent instructions: repo layout, commands, and doc `@`-includes
├── README.md             — usage: CLI examples, the Go API, errors, retries
├── go.mod                — Go module definition (`github.com/d2jvkpn/odoo-go`)
├── *.go                  — package `odoo`: client, config and every API call; tests beside them as `*_test.go`
├── cmd/                  — `main` entry points; arguments and output only, reusable behavior in package `odoo`
│   ├── odoo-cli/         — queries and writes: products, boms, orders, partners, stock, helpdesk
│   └── odoo-sync/        — exports and imports products and BOMs between Odoo databases
├── oca/                  — packages for OCA addons
│   └── helpdesk/         — helpdesk tickets, messages, attachments, followers and activities
├── docs/                 — project documentation
│   ├── README.md         — documentation index
│   ├── catalog-sync.md   — `odoo-sync` behavior
│   ├── helpdesk.md       — helpdesk SDK and CLI operations
│   ├── manual-testing.md — checks against a live Odoo
│   ├── modules.md        — Odoo modules each feature needs
│   └── order-flow.md     — placing orders with several shipping addresses
├── examples/             — sample request files and the matching CLI output, checked by tests
├── configs/              — local connection configs, `local.yaml` (git-ignored)
├── data/                 — temporary files: manual test inputs and exports (git-ignored)
├── target/               — build output (git-ignored)
├── .agents/              — agent rules and workflow records (git-ignored)
│   └── superpowers/      — design specs and implementation plans (git-ignored)
└── .worktrees/           — git worktrees (git-ignored)
```

## Commands

Prerequisites: Go 1.27. The tests use `httptest` fakes and need no Odoo or credentials; only the
checks in `docs/manual-testing.md` need a live server. Run commands from the repository root:

```sh
go build -o target/odoo ./cmd/odoo-cli          # build the CLI
go build -o target/odoo-sync ./cmd/odoo-sync    # build the sync tool
go test -count=1 ./...                          # run every test, uncached
go test -run TestName ./cmd/odoo-sync           # run one test in one package
go test -race -cover ./...                      # check races and coverage
go fmt ./... && go vet ./...                    # format and static analysis
go run ./cmd/odoo-cli -h                        # CLI help
```

A task is done when `go fmt`, `go vet ./...` and `go test -race ./...` pass. Keep build outputs in
`target/`. If Git metadata is unavailable, add `-buildvcs=false` to the build command.

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

Commit subjects follow the existing history: `<type>: <imperative summary>` with a type such as
`feat`, `fix`, `docs`, `test` or `refactor`, for example `fix: validate category parent IDs`.
Create a commit only when the user explicitly authorizes it. PRs should describe the behavior
change, link relevant issues, list validation commands, and update affected CLI examples.

## Security & Configuration

Load `odoo.base_url`, `odoo.api_key`, and optional `odoo.db` through `odoo.LoadClient`. Never commit
or print credentials. Order creation writes draft quotations; transport failures can leave the
outcome uncertain, so do not automatically retry writes. Only read-only calls may use `CallRead`.

@./.agents/README.md

@./docs/README.md
