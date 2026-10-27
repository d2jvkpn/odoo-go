# Partner upsert: no locking in the SDK

- Author: evol
- Status: decided, not implemented
- Date: 2026-10-27

## Decision

`UpsertPartnerByRef` does not lock, retry, or otherwise guard against concurrent calls for the same
`ref`. Callers must serialize per ref, or Odoo must enforce uniqueness.

## Evidence

Run against an Odoo 19 test database with the sales, inventory, and mail modules installed:

- 8 parallel `partners upsert -ref zz-bulk-race` calls all reported `created: true`, and the
  database held 8 partners with that ref.
- The next upsert of that ref returned `ErrAmbiguousPartner` and wrote nothing, so the failure is
  loud and safe, but it persists until someone merges or removes the duplicates in Odoo.
- 12 parallel `orders create` calls produced 12 distinct orders, so order creation does not share
  this problem; it never looks anything up first.

## Why not fix it in the SDK

- The race is between two HTTP requests (search, then create). An in-process mutex cannot cover
  several service instances, and a distributed lock needs infrastructure the SDK does not own.
- Odoo's `res.partner.ref` has no unique constraint. Adding one is a change to the Odoo database or
  a custom module, outside this repository.
- Retrying the search after a create, or deleting duplicates afterwards, would add hidden writes.
  The repository rule is that writes are never retried or repeated automatically.

## Alternatives considered

- **Per-ref in-process mutex inside the Client:** rejected. It protects only one process and gives
  callers a false sense of safety.
- **Create-then-verify (search again after create, delete the extra):** rejected. It needs
  `unlink`, which the SDK deliberately does not expose.

## Revisit when

- The Odoo side gains a unique constraint on `res.partner.ref` (then a create that loses the race
  would return an Odoo error the SDK could map to a retry of the search), or
- the SDK gains a supported way to serialize writes across processes.
