package odoo

import (
	"context"
	"math"
	"strings"
)

// CreateTemplateRequest describes a new product.template. The JSON names are Odoo's field names.
// Without attribute lines Odoo creates one variant; attach attributes with AddProductAttributeLine.
// CategoryID and UoMID zero leave Odoo's defaults in place.
type CreateTemplateRequest struct {
	Name        string  `json:"name"`                       // required, not blank
	ListPrice   float64 `json:"list_price"`                 // finite and >= 0
	CategoryID  int     `json:"categ_id,omitzero"`          // product.category ID; >= 0
	SaleOK      bool    `json:"sale_ok"`                    // whether the product can be sold
	IsStorable  bool    `json:"is_storable"`                // whether inventory is tracked
	Type        string  `json:"type,omitempty"`             // "consu", "service" or "combo"; empty keeps Odoo's default
	UoMID       int     `json:"uom_id,omitzero"`            // uom.uom ID; >= 0
	Description string  `json:"description_sale,omitempty"` // sales description
}

// CreateProductTemplate creates one product.template and returns its ID (not a variant ID). It
// does not retry; after a transport error the outcome may be unknown, so look the product up by its
// default_code before resubmitting.
func (c *Client) CreateProductTemplate(ctx context.Context, in CreateTemplateRequest) (int, error) {
	var (
		ids []int
		err error
	)

	if strings.TrimSpace(in.Name) == "" {
		return 0, invalidArgument("name must not be empty")
	}
	if math.IsNaN(in.ListPrice) || math.IsInf(in.ListPrice, 0) || in.ListPrice < 0 {
		return 0, invalidArgument("list_price must be finite and nonnegative")
	}
	if in.CategoryID < 0 || in.UoMID < 0 {
		return 0, invalidArgument("categ_id and uom_id must not be negative")
	}
	if ids, err = c.createRecords(ctx, "product.template", []any{in}); err != nil {
		return 0, err
	}
	return ids[0], nil
}

// TemplatePatch changes selected fields of a product.template. Nil fields are left unchanged; a
// pointer to zero or false is sent explicitly.
type TemplatePatch struct {
	Name        *string  `json:"name,omitzero"`             // new name; must not be blank
	ListPrice   *float64 `json:"list_price,omitzero"`       // new base price; finite and >= 0
	CategoryID  *int     `json:"categ_id,omitzero"`         // new product.category ID; > 0
	SaleOK      *bool    `json:"sale_ok,omitzero"`          // whether the product can be sold
	IsStorable  *bool    `json:"is_storable,omitzero"`      // whether inventory is tracked
	Type        *string  `json:"type,omitzero"`             // "consu", "service" or "combo"
	UoMID       *int     `json:"uom_id,omitzero"`           // new uom.uom ID; > 0
	Description *string  `json:"description_sale,omitzero"` // new sales description; "" clears it
}

// UpdateProductTemplate changes one product.template. It returns ErrProductNotFound for an unknown
// ID and does not retry.
func (c *Client) UpdateProductTemplate(ctx context.Context, id int, patch TemplatePatch) error {
	if patch.Name != nil && strings.TrimSpace(*patch.Name) == "" {
		return invalidArgument("name must not be empty")
	}
	if p := patch.ListPrice; p != nil && (math.IsNaN(*p) || math.IsInf(*p, 0) || *p < 0) {
		return invalidArgument("list_price must be finite and nonnegative")
	}
	if patch.CategoryID != nil && *patch.CategoryID <= 0 {
		return invalidArgument("categ_id must be positive")
	}
	if patch.UoMID != nil && *patch.UoMID <= 0 {
		return invalidArgument("uom_id must be positive")
	}
	return c.updateRecord(ctx, "product.template", id, patch, ErrProductNotFound)
}

// SetProductTemplateActive archives (false) or restores (true) a product.template and, in Odoo, all
// of its variants. It does not retry.
func (c *Client) SetProductTemplateActive(ctx context.Context, id int, active bool) error {
	return c.updateRecord(ctx, "product.template", id, struct {
		Active bool `json:"active"`
	}{active}, ErrProductNotFound)
}

// VariantPatch changes the per-variant fields of a product.product. Nil fields are left unchanged.
type VariantPatch struct {
	DefaultCode *string  `json:"default_code,omitzero"` // new internal reference; "" clears it
	Barcode     *string  `json:"barcode,omitzero"`      // new barcode
	Weight      *float64 `json:"weight,omitzero"`       // finite and >= 0
	Volume      *float64 `json:"volume,omitzero"`       // finite and >= 0
}

// UpdateVariantFields changes one product.product. It returns ErrProductNotFound for an unknown ID
// and does not retry.
func (c *Client) UpdateVariantFields(ctx context.Context, id int, patch VariantPatch) error {
	for _, n := range []*float64{patch.Weight, patch.Volume} {
		if n != nil && (math.IsNaN(*n) || math.IsInf(*n, 0) || *n < 0) {
			return invalidArgument("weight and volume must be finite and nonnegative")
		}
	}
	return c.updateRecord(ctx, "product.product", id, patch, ErrProductNotFound)
}
