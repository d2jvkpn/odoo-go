# Odoo Module Requirements

The SDK targets Odoo 19 (the JSON-2 API). Names below are technical module names, as in
`ir.module.module.name` and the addons directory; the Apps name is what the Odoo UI shows. Field
ownership was read from `ir.model.fields.modules` on an Odoo 19.0 test database, so a different
Odoo version or customized database can differ.

## Minimum set

`base`, `product`, `sale`, `stock` and `sale_stock`. Helpdesk additionally needs the OCA
`helpdesk_mgmt` and `mail`. Everything else is optional.

## Modules

| Module | Apps name | Needed for |
|--------|-----------|------------|
| `base` | Base | Partners, countries, partner tags (`res.partner`, `res.country`, `res.partner.category`) |
| `product` | Products | Products, internal categories, attributes and variants |
| `sale` | Sales | Sale orders and lines, `pricelist_id`, order confirm and cancel |
| `sale_stock` | Sales and Warehouse Management | `delivery_status`, requested by every order read |
| `stock` | Inventory | `is_storable`, stock quantities and locations, `SetProductStock` |
| `account` | Invoicing | `taxes_id`, only with `products list -details` |
| `mail` | Discuss | Helpdesk messages, followers and activities |
| `website_sale` | eCommerce | Website categories (`product.public.category`) only |
| `helpdesk_mgmt` | Helpdesk Management (OCA) | Tickets, teams, stages, tags, channels, categories |
| `helpdesk_mgmt_sale` | OCA | Linking tickets to sale orders (opt-in) |
| `helpdesk_product` | OCA | Linking tickets to products (opt-in) |

The Contacts app (`contacts`) is not required: it only adds menus and views over `res.partner`.

Product categories come in two independent kinds. A product's `categ_id` is always an internal
`product.category` (module `product`), so products can be categorized without `website_sale`.
Website categories (`product.public.category`, `-type website`) need `website_sale`, and the SDK
only lists, creates and updates them: it has no call that assigns a product to one
(`public_categ_ids` on `product.template`), so do that in Odoo.

## By feature

| Feature | Models | Modules |
|---------|--------|---------|
| Product read | `product.product` | `product`, `stock` (the base field list always requests `is_storable`) |
| Product create and update | `product.template` | `product`; `stock` only when `is_storable` is set |
| Variants (`variants.go`) | `product.attribute`, `product.attribute.value`, `product.template.attribute.line`, `product.template.attribute.value` | `product` |
| Internal categories | `product.category` | `product` |
| Website categories | `product.public.category` | `website_sale` |
| Orders | `sale.order`, `sale.order.line` | `sale`, `sale_stock` |
| Order deliveries, validation | `stock.picking`, `stock.move`, `stock.backorder.confirmation` | `stock`, `sale_stock` |
| Order invoices and payments | `account.move`, `account.payment` | `account` |
| Partners | `res.partner`, `res.partner.category`, `res.country` | `base` |
| Stock | `stock.quant`, `stock.location`, `stock.inventory.conflict` | `stock` |
| Helpdesk | `helpdesk.ticket` and related, `mail.*`, `ir.attachment` | `helpdesk_mgmt`, `mail` |

## Failure modes

A missing module shows up as an Odoo HTTP 500 naming the missing field or model, for example
`Invalid field 'is_storable' on 'product.product'` when `stock` is not installed. The SDK does not
probe for modules. Variant creation sets `create_variant=always` on new attributes; with `dynamic`
Odoo creates variants only on first order, so none appear when the template line is added.
