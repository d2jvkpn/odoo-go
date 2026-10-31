# Manual Testing

Manual checks against a live Odoo server, for behavior the `httptest` fixtures cannot prove. Run
every command from the repository root. All commands are read-only except those under
[Order write tests](#order-write-tests); never run any write command (`create`, `update`, `upsert`,
`archive`, `stock set -yes`, `orders confirm|cancel -yes`) against a database that holds real data,
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
