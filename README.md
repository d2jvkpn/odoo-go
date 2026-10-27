# Odoo Client

A Go client for the
[Odoo JSON-2 API](https://www.odoo.com/documentation/19.0/developer/reference/external_api.html)
(available since Odoo 19). YAML configuration is loaded with `github.com/spf13/viper`.

The CLI lives in `cmd/odoo-cli` and has the `products`, `orders`, `categories`, `partners` and `stock` command groups:

```sh
go run ./cmd/odoo-cli -h
go run ./cmd/odoo-cli orders -h
go run ./cmd/odoo-cli categories list -h
```

`-config` may appear before the command group or among the action's arguments; the one among the
action's arguments takes precedence. For example:

```sh
go run ./cmd/odoo-cli -config configs/local.yaml products list -limit 10
```

## Build

Run from the repository root. Build output goes to `target/`, which Git ignores:

```sh
go build -o target/odoo ./cmd/odoo-cli
./target/odoo -h
```

If the workspace has no valid Git metadata, add `-buildvcs=false` to the build command.

## Querying products

Run the following from the repository root. It reads `odoo.base_url`, `odoo.api_key` and the
optional `odoo.db` from `configs/local.yaml` and prints live product data:

```sh
go run ./cmd/odoo-cli products list
```

By default it queries up to 100 saleable products ordered by ID and prints JSON with the ID, name,
internal reference, list price and product category. When nothing matches it prints `[]`. The
configuration path and the limit can be set:

```sh
go run ./cmd/odoo-cli products list -config configs/local.yaml -limit 10
```

`products list` also accepts `-offset`, `-order` and `-domain` (default `[["sale_ok","=",true]]`).
Count matching products, or fetch one by `product.product` ID (a missing product is an error):

```sh
go run ./cmd/odoo-cli products count
go run ./cmd/odoo-cli products get -id 7
```

Create a product from a JSON file (see `examples/product.json`; only `name` is required). The command
creates a `product.template` and prints the ID of its `product.product` variant, which is the ID used
by `products get` and order lines. It writes to the configured database and is never retried; after a
transport error, check Odoo before running it again. Odoo does not require `default_code` to be unique,
so a repeated run creates a duplicate.

```sh
go run ./cmd/odoo-cli products create -file examples/product.json
```

The command prints [examples/product.response.json](examples/product.response.json); `products get -id 21`
prints [examples/product-get.response.json](examples/product-get.response.json). Tests run the CLI against
Odoo-shaped fixtures and compare the output with these files.

In Go, `client.CreateProduct(ctx, odoo.CreateProductRequest{Name: "Tea"})` does the same. A nil
`ListPrice` or `SaleOK` keeps Odoo's default, while a pointer to `0` or `false` is sent explicitly. If
the template is created but its variant cannot be read, the error names the template ID.

Configuration loading is provided by `odoo.LoadClient`. The automated tests below use a local mock
server and never read the real configuration.

## Sales orders

Orders use the `sale.order` model. Every command reads `configs/local.yaml` by default; pass
`-config <path>` after the subcommand to override it.

```sh
# List orders, newest ID first by default
go run ./cmd/odoo-cli orders list -limit 10

# Confirmed sales orders of one customer
go run ./cmd/odoo-cli orders list -domain '[["partner_id","=",7],["state","=","sale"]]' -limit 20 -offset 0

# Order detail with order lines
go run ./cmd/odoo-cli orders get -id 42
```

See [examples/sale-order.json](examples/sale-order.json) for the creation input. Replace the
customer `partner_id` and the product variant `product_id` values with real IDs, then run:

```sh
go run ./cmd/odoo-cli orders create -file examples/sale-order.json
```

This writes one **draft quotation** and its lines to the configured database and prints a result
such as [examples/sale-order.response.json](examples/sale-order.response.json); `orders get -id 42`
prints [examples/sale-order-get.response.json](examples/sale-order-get.response.json). It never confirms the order, ships goods or invoices. The customer field maps
to `res.partner` and the product field to `product.product` (the variant, not the product
template ID).

The input file uses a simplified `lines` array. The client wraps it in `vals_list` and converts
each line to a `[0, 0, {...}]` entry in `order_line`. The sample sends this body to
`/json/2/sale.order/create`:

```json
{
  "vals_list": [
    {
      "partner_id": 15,
      "client_order_ref": "WX202610270001",
      "origin": "web",
      "note": "<p>Leave at door</p>",
      "order_line": [
        [0, 0, {"product_id": 101, "product_uom_qty": 2}],
        [0, 0, {"product_id": 102, "product_uom_qty": 5}]
      ]
    }
  ]
}
```

Odoo returns an ID array (for example `[42]`); the Go method turns it into a single order ID.

- `lines` needs at least one entry. `product_uom_qty` must be a finite positive number and uses the
  product's default unit of measure.
- When `price_unit` is omitted Odoo prices the line; an explicit `0` means a zero price. An
  explicit unit price must be a finite non-negative number.
- The optional `pricelist_id` selects a pricelist; when omitted Odoo's default applies.
- `client_order_ref` is the customer's order reference. It is **not guaranteed unique and does not
  make creation idempotent**. After a timeout, a dropped connection or a response decoding error
  the server may already have created the order; query to verify before retrying.
- The optional `origin` is the free-text source document, handy for a channel marker such as `web`.
  The optional `note` is Odoo's terms and conditions: it is **HTML, not escaped, and printed on the
  quotation**, so do not put internal data in it. Both are returned when reading orders.
- Detail reads the order and then its lines in two separate requests, so concurrent edits are not
  guaranteed to be seen as one snapshot.
- Relation fields in results become `{"id":7,"name":"Customer"}`; an unset relation has ID 0, and
  optional text `false`/`null` becomes an empty string.

Go API:

```go
orders, err := client.SearchSaleOrders(ctx, odoo.SaleOrderQuery{
    Domain: []any{[]any{"partner_id", "=", 7}},
    Limit: 20,
    Offset: 0,
    Order: "id desc",
})
// Handle err, then use orders.

order, err := client.GetSaleOrder(ctx, 42)
// errors.Is(err, odoo.ErrSaleOrderNotFound) detects a missing or invisible order.

id, err := client.CreateSaleOrder(ctx, odoo.CreateSaleOrderRequest{
    PartnerID: 15,
    ClientOrderRef: "WX202610270001",
    Origin: "web",
    Note: "<p>Leave at door</p>",
    Lines: []odoo.CreateSaleOrderLine{
        {ProductID: 101, Quantity: 2}, // default pricing
        {ProductID: 102, Quantity: 5},
    },
})
// Handle err, then use id. Creation is never retried automatically.
```

## Customers, stock, and order transitions

```sh
go run ./cmd/odoo-cli partners list -limit 5
go run ./cmd/odoo-cli partners get -id 15
go run ./cmd/odoo-cli partners upsert -ref web-1001 -file examples/partner.json
go run ./cmd/odoo-cli stock get -ids 21,22
go run ./cmd/odoo-cli stock locations
go run ./cmd/odoo-cli stock set -product-id 21 -location-id 5 -qty 30 -yes
go run ./cmd/odoo-cli orders update -id 42 -file order-patch.json   # client_order_ref, origin, note
go run ./cmd/odoo-cli orders confirm -id 42        # prints the action, sends nothing
go run ./cmd/odoo-cli orders confirm -id 42 -yes   # confirms the quotation
go run ./cmd/odoo-cli orders cancel -id 42 -yes
go run ./cmd/odoo-cli products update -id 21 -file patch.json
go run ./cmd/odoo-cli products archive -id 21      # unarchive restores it
go run ./cmd/odoo-cli categories update -id 7 -file patch.json
```

- `partners upsert` stores your own user ID in the partner `ref`: it updates the active partner
  with that ref, or creates one, and prints `{"id": N, "created": true|false}` (see
  `examples/partner.response.json`). Archived partners are not searched, so an archived partner with that ref leads to a duplicate. Two
  matches fail with `ErrAmbiguousPartner`. Odoo does not make
  `ref` unique, so concurrent upserts for one **new** ref can create duplicates: in a test, 8 parallel
  upserts created 8 partners, and every later upsert of that ref then fails with
  `ErrAmbiguousPartner` until the duplicates are cleaned up in Odoo. Serialize upserts per ref (a
  lock keyed by ref) or add a unique constraint on `res.partner.ref`. Different refs are independent.
- `stock get` takes up to 200 `product.product` IDs and omits unknown ones (see
  `examples/stock.response.json`). It needs Odoo's stock module.
- `stock set` replaces the on-hand quantity of a product at a location (an inventory adjustment,
  not an increment; `-qty 0` means sold out). It needs `-yes`, takes a location from
  `stock locations`, and is never retried. The product must have inventory tracking on
  (`"is_storable": true` in `products create` or `products update`), otherwise Odoo keeps its
  quantity at 0 and the SDK returns `ErrProductNotStorable`. It makes two writes (create a count,
  then apply it); if the second fails the error names the pending quant, and a later `stock set`
  overwrites it. Confirmed orders reserve stock: `free_qty` drops when an order is confirmed and
  returns when it is cancelled.
- `orders confirm|cancel` need `-yes`. They check the order state first (`ErrInvalidOrderState`)
  and are never retried. After a transport error the outcome is unknown: call `GetSaleOrder` and
  inspect `State` before trying again. Odoo may still refuse a cancel, for example for delivered
  or invoiced orders. Orders also report `invoice_status` and `delivery_status`; the latter comes from
  Odoo's `sale_stock` module, so order reads fail on a database without it.
- `orders update` changes only `client_order_ref`, `origin` and `note`; a pointer to `""` clears one.
  Odoo may refuse the change on a locked order, which surfaces as an `*HTTPError`.
- Updates take a patch with only the fields to change (`products update`, `categories update`,
  `partners update`, `orders update`); an empty patch or an unknown ID is an error. Only products can be archived.

```go
id, created, err := client.UpsertPartnerByRef(ctx, "web-1001", odoo.CreatePartnerRequest{Name: "Ann"})
stock, err := client.GetProductStock(ctx, []int{21, 22})
err = client.SetProductStock(ctx, 21, 5, 30) // product, location, quantity on hand
locations, err := client.ListStockLocations(ctx)
err = client.ConfirmSaleOrder(ctx, 42)
price := 12.5
err = client.UpdateProduct(ctx, 21, odoo.UpdateProductRequest{ListPrice: &price})
err = client.SetProductActive(ctx, 21, false)
```

## Querying product categories

Two category systems are supported: backend categories in `product.category` (`internal`, the
default) and storefront categories in `product.public.category` (`website`). Storefront categories
need the `website_sale` module installed on the server and read access granted. The two systems are
independent, so their IDs must not be mixed.

```sh
# Backend categories, up to 1000 by default
go run ./cmd/odoo-cli categories list

# Storefront categories
go run ./cmd/odoo-cli categories list -type website

# Root categories
go run ./cmd/odoo-cli categories list -parent-id 0

# Direct children of category 7
go run ./cmd/odoo-cli categories list -parent-id 7

# One category
go run ./cmd/odoo-cli categories get -id 7

# Name filter with paging
go run ./cmd/odoo-cli categories list -domain '[["name","ilike","Furniture"]]' -limit 20 -offset 20

# Create a category from a request file (see examples/category.json)
go run ./cmd/odoo-cli categories create -file examples/category.json
```

`categories create` accepts `name` and an optional `parent_id` (omit it for a root category), honors
`-type`, and prints the new ID (see `examples/category.response.json`). Like other writes it is
never retried; after a transport error, check Odoo before resubmitting.

Backend categories request `id`, `name`, `complete_name` and `parent_id`; `complete_name` is the
full path (for example "Furniture / Desks"). Storefront categories request `id`, `name`,
`display_name`, `parent_id` and `child_id`; an empty child list is omitted from the output. The
parent is still converted to a relation object such as `{"id":1,"name":"Furniture"}`.

Without a parent condition all levels are searched; use `-parent-id` for direct children. A query
returns only the current page and never downloads the whole tree recursively.

```go
parentID := 0 // root categories; use a positive ID for the direct children of that category
categories, err := client.SearchProductCategories(ctx, odoo.ProductCategoryQuery{
    Type: odoo.InternalCategory, // or odoo.WebsiteCategory
    ParentID: &parentID,         // nil means no parent filter
    Limit: 100,
})

category, err := client.GetProductCategory(ctx, odoo.WebsiteCategory, 7)
// errors.Is(err, odoo.ErrProductCategoryNotFound) detects a missing or invisible category.
```

In the Go API, `Limit == 0` defaults to 1000 for backend categories and 100 for storefront
categories, products and orders; negative paging values are errors. On the command line, omitting
`-limit` uses those defaults and an explicit value must be positive. An empty list prints `[]`.
Category endpoints are read-only.

## Pagination and counting

The list commands page with `-limit` / `-offset` (`products`, `orders`, `categories`). Each group
also has a `count` command that prints `{"count":N}` for a `-domain` (categories also take `-type`
and `-parent-id`), and `list -all` fetches every page into one JSON array, with `-limit` as the
page size:

```sh
go run ./cmd/odoo-cli orders count -domain '[["state","=","sale"]]'
go run ./cmd/odoo-cli orders list -all -limit 200
```

In Go, `CountProducts`, `CountSaleOrders` and `CountProductCategories` call `search_count`, and
`AllProducts`, `AllSaleOrders` and `AllProductCategories` return an `iter.Seq2[T, error]` that
requests one page at a time (`Limit` is the page size, `Offset` the start):

```go
for product, err := range client.AllProducts(ctx, odoo.ProductQuery{Limit: 200}) {
    if err != nil {
        return err // the first error ends the iteration
    }
    fmt.Println(product.ID, product.Name)
}

total, err := client.CountSaleOrders(ctx, []any{[]any{"state", "=", "sale"}})
```

Pagination is offset based. If records are created or deleted while paging, a record can be
skipped or returned twice; use a stable `Order` (such as `id`) and treat a count as a snapshot.
Pages are fetched with the client's retry policy but the pages do not form one consistent snapshot.

## Using the Go API

Create a client from the YAML configuration:

```go
client, err := odoo.LoadClient("configs/local.yaml")
if err != nil {
    return err
}
```

Or pass the connection parameters directly:

```go
client := odoo.NewClient(
    "https://odoo.example.com", // server URL without /json/2
    os.Getenv("ODOO_API_KEY"),
    "mydb", // when empty, X-Odoo-Database is not sent
)

products, err := client.SearchProducts(ctx, odoo.ProductQuery{
    Domain: []any{[]any{"sale_ok", "=", true}},
    Limit:  100, // 0 defaults to 100; Order defaults to "id"
})
if err != nil {
    return err
}
product, err := client.GetProduct(ctx, 7) // odoo.ErrProductNotFound when missing
```

### Errors

All errors can be inspected with `errors.Is` / `errors.As`:

| Error | Meaning |
|-------|---------|
| `odoo.ErrInvalidArgument` | The caller passed an invalid parameter; no request was sent. |
| `odoo.ErrInvalidConfig` | The YAML configuration is missing fields or malformed. |
| `odoo.ErrUnexpectedResponse` | Odoo answered successfully but the body had an unexpected shape. |
| `odoo.ErrProductNotFound`, `odoo.ErrProductCategoryNotFound`, `odoo.ErrSaleOrderNotFound` | A `Get*` call matched no visible record. |
| `*odoo.HTTPError` | A non-2xx response; carries `StatusCode` and `Body` (at most 4096 bytes). |

```go
var httpErr *odoo.HTTPError
if errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusUnauthorized {
    // The API key was rejected.
}
```

Other models can be called directly through the low-level `client.Call`:

```go
var result []map[string]any
err := client.Call(ctx, "product.product", "search_read", map[string]any{
    "domain": []any{},
    "fields": []string{"id", "name"},
    "limit":  10,
}, &result)
```

- `params` is the named-parameter object; `nil` sends `{}`.
- `result` is a pointer receiving the JSON; `nil` reads and discards a successful response.
- The default timeout is 30 seconds. Set a custom `*http.Client` through `client.HTTP`; context
  cancellation and deadlines are supported as well.
- A non-2xx response returns an error containing the status code and at most 4096 bytes of the
  body. Transport, encoding and read errors keep the original error, so `errors.Is` / `errors.As`
  work.
- `Call` and `CreateSaleOrder` are never retried, to avoid repeating model methods that have side
  effects. Retries are opt-in and apply only to read-only calls; see "Retrying read-only calls".
- Finish configuring the client before making concurrent calls and do not modify its fields while
  calls are in flight.

### Retrying read-only calls

Retries are off by default. Enable them for the `Search*` / `Get*` methods and `CallRead`:

```go
client.Retry = odoo.RetryPolicy{
    MaxRetries: 3,                     // extra attempts after the first; 0 disables retries
    Backoff:    200 * time.Millisecond, // wait before retry n is Backoff * 2^n (default 200ms)
    MaxBackoff: 5 * time.Second,        // upper bound of a single wait (default 5s)
    NoJitter:   false,                  // by default each wait is shortened by a random 0-20%
}
```

Or set `odoo.max_retries` (0 to 10) in the YAML configuration, which also applies to the CLI.

- Only transient failures are retried: transport errors (connection refused or reset, timeout)
  and HTTP 429, 502, 503 and 504. Other 4xx/5xx responses and decoding errors are returned at once.
- A `Retry-After` header (seconds or HTTP date) raises the wait to the requested delay, capped at
  `MaxBackoff`; the parsed value is also available as `HTTPError.RetryAfter`.
- Retrying stops when the context is cancelled or its deadline passes; the last error is returned.
- Use `CallRead` for your own read-only model methods (`search_read`, `read`). Never use it for
  methods with side effects.

## Field mapping and Odoo reference

The SDK returns a fixed subset of Odoo fields; JSON names are the Odoo field names. Fields not
listed here are not read, so use `CallRead` for anything else.

| SDK type | Odoo model | Fields read |
|----------|------------|-------------|
| `Product` | `product.product` | `id`, `name`, `default_code`, `list_price`, `categ_id`, `sale_ok`, `is_storable` |
| `ProductCategory` | `product.category` / `product.public.category` | `id`, `name`, `complete_name`, `display_name`, `parent_id`, `child_id` |
| `SaleOrder` | `sale.order` | `id`, `name`, `partner_id`, `date_order`, `state`, `amount_untaxed`, `amount_tax`, `amount_total`, `currency_id`, `client_order_ref`, `invoice_status`, `delivery_status`, `origin`, `note`, `order_line` |
| `SaleOrderLine` | `sale.order.line` | `id`, `product_id`, `name`, `product_uom_qty`, `price_unit`, `price_subtotal`, `display_type` |
| `Partner` | `res.partner` | `id`, `name`, `email`, `phone`, `ref`, `street`, `city`, `zip`, `country_id` |
| `ProductStock` | `product.product` | `id`, `qty_available`, `virtual_available`, `free_qty` |
| `StockLocation` | `stock.location` | `id`, `complete_name` |

The SDK does not duplicate Odoo's own documentation. For the protocol (authentication, request
format, error codes) see the
[official External API reference](https://www.odoo.com/documentation/19.0/developer/reference/external_api.html).
Fields and methods of each model depend on the Odoo version and installed modules, so check them
against your own database, for example with the `fields_get` model method through `CallRead`, or
the database's built-in API documentation page if your version provides one.

## Testing

Run the tests and local examples (no real Odoo server or API key needed):

```sh
go test -v -count=1 ./...
go test -race -cover ./...
go test . -run Example -v
```
