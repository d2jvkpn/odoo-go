# Order Flow and Shipping Addresses

How a backend places an order through this SDK when each customer keeps several shipping
addresses. The SDK performs the individual calls; ordering, serialization and ID bookkeeping
belong to the calling application.

## Data model

- A customer is a `res.partner`. Each shipping address is a child `res.partner` with
  `parent_id` set to the customer and `type=delivery`; an invoice address uses `type=invoice`.
- An order references addresses through `partner_shipping_id` and `partner_invoice_id`. These are
  references, not snapshots: editing the child contact changes every order that points to it.
  When a user changes an address, create a new child contact and leave the old one in place.
- Archive superseded addresses with `SetPartnerActive` (`partners archive`); existing orders keep
  their reference. `type` cannot tell a child from a top-level customer (both default to
  `contact`); test `parent_id` instead.
- The application owns `parent_id`, `type` and ID validation. The SDK passes them through and
  Odoo is the final judge.

## Flow

1. Upsert the customer with `UpsertPartnerByRef` (`ref` such as `user-42`) and store the returned
   partner ID in the backend user record.
2. Ensure the chosen address has a child contact (see the next section) and store its partner ID
   in the backend address record.
3. `CreateSaleOrder` with `PartnerID`, `PartnerShippingID`, optional `PartnerInvoiceID`, and
   `ClientOrderRef` set to the backend order number. The result is a draft quotation.
4. `ConfirmSaleOrder` once payment succeeds; `CancelSaleOrder` to cancel. Odoo may refuse to
   cancel delivered or invoiced orders.
5. `GetSaleOrder` reads `state`, `delivery_status`, `invoice_status` and both addresses.

No write is retried automatically. After a transport error the outcome is unknown: look the record
up before repeating the call (orders by `client_order_ref`, which is not unique in Odoo, so keep
backend order numbers unique).

## Creating an address contact idempotently

The SDK has no find-or-create for child contacts. `UpsertPartnerByRef` matches on `ref` alone,
ignores `parent_id` and skips archived records, so it must not be used for addresses. Do this in
the application instead:

1. Give every address a globally unique `ref`, for example `user-42-addr-1001`.
2. If the backend address row already holds an Odoo partner ID, use it and stop.
3. Otherwise serialize per address (a row lock, or a unique key plus a state column) so only one
   request creates the contact.
4. Under that lock, search first, including archived records:

   ```sh
   odoo partners list -domain '[["ref","=","user-42-addr-1001"],["parent_id","=",15],["active","in",[true,false]]]'
   ```

5. Reuse a match; otherwise call `CreatePartner` and store the returned ID immediately.
6. If `CreatePartner` fails in transport, go back to step 4 rather than retrying the create: the
   first attempt may have succeeded.

Without step 3 two concurrent requests can both miss in step 4 and both create a contact. See
[the locking decision](../.agents/decisions/2026-10-27_evol_partner-upsert-locking.md) for the
same limitation on `UpsertPartnerByRef`.
