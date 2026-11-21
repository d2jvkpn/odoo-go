package odoo

import (
	"context"
	"fmt"
)

// catalogPageSize is the page size of the full-table reads in this file.
const catalogPageSize = 500

// CatalogVariant is a product.product variant with every field odoo-sync copies between databases.
// Name, ListPrice, Category, SaleOK, IsStorable, Type, UoM and Description belong to the shared
// product.template, so sibling variants report the same values. Active is the variant's own flag.
// is_storable is added by the stock module, so reads fail with HTTP 500 on a database without it.
type CatalogVariant struct {
	ID          int            `json:"id"`                                 // product.product ID
	Template    Many2One       `json:"product_tmpl_id"`                    // the product.template that groups sibling variants
	Name        string         `json:"name"`                               // template name, without attribute values
	DefaultCode OptionalString `json:"default_code"`                       // internal reference (SKU); not unique in Odoo
	ListPrice   float64        `json:"list_price"`                         // template base price
	Category    Many2One       `json:"categ_id"`                           // internal category; Name is the full path
	SaleOK      bool           `json:"sale_ok"`                            // whether the product can be sold
	IsStorable  bool           `json:"is_storable"`                        // whether inventory is tracked
	Type        string         `json:"type"`                               // "consu", "service" or "combo"
	UoM         Many2One       `json:"uom_id"`                             // unit of measure
	Description OptionalString `json:"description_sale"`                   // sales description
	Barcode     OptionalString `json:"barcode"`                            // EAN/UPC barcode
	Weight      float64        `json:"weight"`                             // in the database's weight unit
	Volume      float64        `json:"volume"`                             // in the database's volume unit
	ValueIDs    []int          `json:"product_template_variant_value_ids"` // IDs for ProductAttributeValues
	Active      bool           `json:"active"`                             // false when archived
}

var catalogVariantFields = []string{
	"id", "product_tmpl_id", "name", "default_code", "list_price", "categ_id", "sale_ok", "is_storable", "type",
	"uom_id", "description_sale", "barcode", "weight", "volume", "product_template_variant_value_ids", "active",
}

// readAll reads every record of model matching domain, page by page and ordered by id. With
// withArchived it also returns archived records.
func readAll[T any](
	ctx context.Context,
	c *Client,
	model string,
	fields []string,
	domain []any,
	withArchived bool,
) ([]T, error) {
	var (
		out  = make([]T, 0)
		item T
		err  error
	)

	if domain == nil {
		domain = []any{}
	}
	for item, err = range paginate(catalogPageSize, 0, func(limit, offset int) ([]T, error) {
		var page []T

		params := map[string]any{"domain": domain, "fields": fields, "limit": limit, "offset": offset, "order": "id"}
		if withArchived {
			params["context"] = map[string]any{"active_test": false}
		}
		if err := c.CallRead(ctx, model, "search_read", params, &page); err != nil {
			return nil, fmt.Errorf("read %s: %w", model, err)
		}
		return page, nil
	}) {
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}

// ListCatalogVariants returns every product.product variant ordered by ID. With includeArchived it
// also returns archived variants.
func (c *Client) ListCatalogVariants(ctx context.Context, includeArchived bool) ([]CatalogVariant, error) {
	return readAll[CatalogVariant](ctx, c, "product.product", catalogVariantFields, nil, includeArchived)
}

// ProductAttributeOption is one value of a product.attribute, such as "500g" of "Weight".
type ProductAttributeOption struct {
	ID     int    `json:"id"`     // product.attribute.value ID
	Name   string `json:"name"`   // the value
	Active bool   `json:"active"` // false when archived
}

// ProductAttribute is a product.attribute with its values.
type ProductAttribute struct {
	ID            int                      `json:"id"`             // product.attribute ID
	Name          string                   `json:"name"`           // for example "Weight"
	CreateVariant string                   `json:"create_variant"` // "always", "dynamic" or "no_variant"
	Active        bool                     `json:"active"`         // false when archived
	Values        []ProductAttributeOption `json:"values"`         // its values, ordered by ID
}

type attributeRow struct {
	ID            int    `json:"id"`
	Name          string `json:"name"`
	CreateVariant string `json:"create_variant"`
	Active        bool   `json:"active"`
}

type attributeValueRow struct {
	ID        int      `json:"id"`
	Name      string   `json:"name"`
	Attribute Many2One `json:"attribute_id"`
	Active    bool     `json:"active"`
}

// ListProductAttributes returns every product attribute with its values, ordered by ID, archived
// attributes and values included (check Active).
func (c *Client) ListProductAttributes(ctx context.Context) ([]ProductAttribute, error) {
	var (
		rows   []attributeRow
		values []attributeValueRow
		out    []ProductAttribute
		index  = make(map[int]int)
		row    attributeRow
		value  attributeValueRow
		err    error
	)

	if rows, err = readAll[attributeRow](
		ctx, c, "product.attribute", []string{"id", "name", "create_variant", "active"}, nil, true,
	); err != nil {
		return nil, err
	}
	if values, err = readAll[attributeValueRow](
		ctx, c, "product.attribute.value", []string{"id", "name", "attribute_id", "active"}, nil, true,
	); err != nil {
		return nil, err
	}
	out = make([]ProductAttribute, 0, len(rows))
	for _, row = range rows {
		index[row.ID] = len(out)
		out = append(out, ProductAttribute{
			ID: row.ID, Name: row.Name, CreateVariant: row.CreateVariant, Active: row.Active,
			Values: []ProductAttributeOption{},
		})
	}
	for _, value = range values {
		i, ok := index[value.Attribute.ID]
		if !ok {
			continue
		}
		out[i].Values = append(out[i].Values, ProductAttributeOption{ID: value.ID, Name: value.Name, Active: value.Active})
	}
	return out, nil
}

// UoM is a unit of measure.
type UoM struct {
	ID   int    `json:"id"`   // uom.uom ID
	Name string `json:"name"` // for example "kg"
}

// ListUoMs returns every unit of measure, ordered by ID. The uom.uom model and its name field are
// those of Odoo 19; check them before using this against another version.
func (c *Client) ListUoMs(ctx context.Context) ([]UoM, error) {
	return readAll[UoM](ctx, c, "uom.uom", []string{"id", "name"}, nil, false)
}
