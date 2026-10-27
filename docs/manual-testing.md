# Manual Testing

Manual checks against a live Odoo server, for behavior the `httptest` fixtures cannot prove. Run
every command from the repository root. All commands below are read-only; never run
`orders create` against a database that holds real data, because it writes a draft quotation and
is never retried automatically.

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
