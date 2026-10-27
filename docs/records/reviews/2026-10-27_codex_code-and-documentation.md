# Code and documentation review

- Author: Codex
- Status: review complete; findings open
- Date: 2026-10-27
- Reviewed revision: `cb9b00a`

## Scope

Reviewed the SDK, CLI, tests, README, and maintained architecture and manual testing documentation.
The working tree was clean when the review began. This was a repository-wide review, not a review
of an uncommitted diff. Relevant Odoo behavior was checked against the upstream Odoo 19.0 source.
No live Odoo database was queried or modified, and no production fixes were made.

## Findings

### P1: Inventory conflicts are reported as successful stock adjustments

Location: `stock.go:132`, in `SetProductStock`.

The call to `stock.quant.action_apply_inventory` discards its response by passing a nil result.
If stock moves between creating the count and applying it, Odoo can return an action for the
`stock.inventory.conflict` wizard without applying the adjustment. The SDK still returns nil,
and the CLI reports `executed: true`.

A local HTTP fixture returned the conflict action after a successful quant creation. The test
expected an error but observed a nil return from `SetProductStock`.

Recommendation: decode the apply response, distinguish a completed adjustment from a conflict
action, and return an actionable error containing the pending quant ID. Add regression coverage
for the conflict response. Do not automatically retry the write.

Source: [Odoo inventory application][inventory-source].

[inventory-source]:
  https://github.com/odoo/odoo/blob/19.0/addons/stock/models/stock_quant.py#L409-L426

### P2: Reusing a pagination iterator silently omits records

Location: `pagination.go:53`, in `paginate`.

The iterator increments the outer function's captured `offset`. That offset survives after one
traversal ends. Reusing the same iterator over stable records 1 through 5 with page size 2 returns
`[1 2 3 4 5]` on the first traversal and `[5]` on the second. An exact multiple of the page size
leaves the next traversal empty.

This affects `AllProducts`, `AllSaleOrders`, and `AllProductCategories`. Their documentation does
not declare single-use behavior, and the implementation also fails to behave consistently as a
single-use stream when iteration stops in the middle of a page.

Recommendation: initialize a traversal-local offset inside the returned function. Add tests that
reuse an iterator after a complete traversal and after an early break. The local reproduction
confirmed the two different results above. The standard library's `go doc iter` describes repeated
traversal as the convention unless single-use behavior is documented explicitly.

### P2: Unnamed partner addresses break partner reads

Location: `partners.go:12`, in `Partner.Name`.

The SDK decodes `name` into a Go string, but Odoo allows invoice, delivery, and other addresses
without a name. Such records can return `name: false`. One record with this value causes the
entire `SearchPartners` response to fail decoding. `GetPartner` and an upsert whose lookup matches
such a record are affected as well.

A local fixture containing an unnamed address produced:

```text
search partners: unexpected response: decode response: json: cannot unmarshal bool into Go struct field .0.name of type string
```

Recommendation: normalize an unset name with `OptionalString` or equivalent custom decoding that
preserves the public field type if compatibility requires it. Add an unnamed address fixture to
partner read tests.

Source: [Odoo partner name constraint][partner-source].

[partner-source]:
  https://github.com/odoo/odoo/blob/19.0/odoo/addons/base/models/res_partner.py#L302-L305

### P2: Product documentation understates shared template effects

Locations: `products.go:162` and `products.go:16`.

The update documentation describes changing a product variant, but fields including `name`,
`list_price`, `categ_id`, `sale_ok`, and `is_storable` belong to its shared product template.
Updating them through one variant also affects sibling variants of that template.

The `ListPrice` comment also describes a sales price without explaining that `list_price`
excludes variant price extras. Odoo computes the variant catalog price as `lst_price`, adding
`price_extra` to the template base price. A variant with a base price of 100 and an extra of 20
therefore exposes 100 through this SDK field, not 120.

Recommendation: document which update fields are shared across variants, and describe `ListPrice`
as the template base price excluding variant extras. Keep the current Odoo JSON field mapping
unless a separate API change is intended. This finding was verified against upstream source;
shared-template behavior was not exercised against a live database.

Source: [Odoo product variant implementation][product-source].

[product-source]:
  https://github.com/odoo/odoo/blob/19.0/addons/product/models/product_product.py

### P3: Category documentation incorrectly says endpoints are read-only

Location: `README.md:288`.

The sentence "Category endpoints are read-only" contradicts the implemented create and update
methods and their CLI examples in the same README.

Recommendation: remove the stale sentence or explicitly distinguish reads from writes.

## Validation

The existing checks passed:

| Command | Result |
|---------|--------|
| `go test -v -count=1 ./...` | 235 tests passed across two packages |
| `go test -race -cover ./...` | Passed; SDK coverage 92.9%, CLI coverage 80.3% |
| `go vet ./...` | Passed |
| `go build -o target/odoo ./cmd/odoo-cli` | Passed |
| `target/odoo -h` | Displayed all five command groups |

The sandbox initially blocked local listening sockets. Test commands requiring `httptest`
servers were rerun with local socket access enabled and completed with the results above.

Three temporary reproduction tests were created under the ignored directory
`data/explore/.audit_review/`. All three failed on the reviewed implementation for the expected
reasons: a conflict returned nil, a second traversal returned only the final page, and an unnamed
address failed decoding. They are intentionally failing audit probes, not part of the passing
repository suite. Run them locally with:

```sh
go test -v -count=1 ./data/explore/.audit_review
```

These temporary files are not versioned and may be absent in another checkout. The scenarios
above describe the fixtures needed to recreate them.

## Limits and existing decisions

The passing suite does not cover the three reproduced defects. Partner fixtures supply names,
pagination tests construct fresh iterators, and stock application fixtures return a successful
response instead of a conflict action. Product request fixtures do not establish shared-template
behavior on a real Odoo server.

The documented partner upsert race and archived-reference behavior were not treated as new
findings. The existing decision deliberately leaves synchronization to callers or the server.
Documented changes during offset pagination and non-atomic order reads were also not reported
again as defects. Installed modules, customizations, and permissions on a deployment can change
server behavior and were outside this offline review.

Priority should go to detecting unapplied inventory adjustments, followed by partner decoding
and iterator reuse. The remaining findings require documentation corrections.
