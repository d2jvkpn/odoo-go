package odoo

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ErrAttributeValueNotFound reports that a product.template.attribute.value ID does not exist.
var ErrAttributeValueNotFound = errors.New("product attribute value not found")

// createRecords creates one record per element of valsList in a single call and returns their IDs in
// order. It does not retry; after a transport or decode error the outcome may be unknown.
func (c *Client) createRecords(ctx context.Context, model string, valsList []any) ([]int, error) {
	var (
		err error
		ids []int
		id  int
	)

	if err = c.Call(ctx, model, "create", map[string]any{"vals_list": valsList}, &ids); err != nil {
		return nil, fmt.Errorf("create %s: %w", model, err)
	}
	if len(ids) != len(valsList) {
		return nil, fmt.Errorf(
			"%w: create %s returned %d IDs for %d records", ErrUnexpectedResponse, model, len(ids), len(valsList),
		)
	}
	for _, id = range ids {
		if id <= 0 {
			return nil, fmt.Errorf("%w: create %s returned a non-positive ID", ErrUnexpectedResponse, model)
		}
	}
	return ids, nil
}

// CreateProductAttribute creates a product.attribute such as "Weight" and returns its ID. Its
// create_variant is "always", so a template that uses it gets its variants immediately instead of on
// first order. It does not retry; after a transport error check Odoo before resubmitting.
func (c *Client) CreateProductAttribute(ctx context.Context, name string) (int, error) {
	var (
		err error
		ids []int
	)

	if strings.TrimSpace(name) == "" {
		return 0, invalidArgument("name must not be empty")
	}
	ids, err = c.createRecords(ctx, "product.attribute", []any{
		map[string]any{"name": name, "create_variant": "always"},
	})
	if err != nil {
		return 0, err
	}
	return ids[0], nil
}

// CreateProductAttributeValues creates values such as "500g" under one product.attribute and returns
// their IDs in the order given. Values must be non-blank and unique. It does not retry.
func (c *Client) CreateProductAttributeValues(
	ctx context.Context,
	attributeID int,
	names []string,
) ([]int, error) {
	var (
		valsList []any
		seen     = make(map[string]bool, len(names))
		name     string
	)

	if attributeID <= 0 {
		return nil, invalidArgument("attribute ID must be positive")
	}
	if len(names) == 0 {
		return nil, invalidArgument("at least one value is required")
	}
	for _, name = range names {
		if strings.TrimSpace(name) == "" {
			return nil, invalidArgument("value must not be empty")
		}
		if seen[name] {
			return nil, invalidArgument("duplicate value %q", name)
		}
		seen[name] = true
		valsList = append(valsList, map[string]any{"name": name, "attribute_id": attributeID})
	}
	return c.createRecords(ctx, "product.attribute.value", valsList)
}

// AddProductAttributeLine attaches an attribute with the given value IDs to a product.template and
// returns the product.template.attribute.line ID. Odoo then generates one variant per value, or per
// combination when the template has several lines. It does not retry.
func (c *Client) AddProductAttributeLine(
	ctx context.Context,
	templateID, attributeID int,
	valueIDs []int,
) (int, error) {
	var (
		err error
		ids []int
	)

	if templateID <= 0 {
		return 0, invalidArgument("template ID must be positive")
	}
	if attributeID <= 0 {
		return 0, invalidArgument("attribute ID must be positive")
	}
	if len(valueIDs) == 0 {
		return 0, invalidArgument("at least one value ID is required")
	}
	for _, id := range valueIDs {
		if id <= 0 {
			return 0, invalidArgument("value ID must be positive")
		}
	}
	ids, err = c.createRecords(ctx, "product.template.attribute.line", []any{map[string]any{
		"product_tmpl_id": templateID,
		"attribute_id":    attributeID,
		"value_ids":       []any{[]any{6, 0, valueIDs}},
	}})
	if err != nil {
		return 0, err
	}
	return ids[0], nil
}

// SetVariantPriceExtra sets the extra price of one product.template.attribute.value, the ID listed in
// Product.ValueIDs. Every variant that uses the value then sells at ListPrice plus the extras of its
// values. It returns ErrAttributeValueNotFound for an unknown ID and does not retry.
func (c *Client) SetVariantPriceExtra(ctx context.Context, templateValueID int, extra float64) error {
	return c.updateRecord(ctx, "product.template.attribute.value", templateValueID, struct {
		PriceExtra float64 `json:"price_extra"`
	}{extra}, ErrAttributeValueNotFound)
}

// AddProductVariants adds a new attribute with the given values to a product.template and returns the
// template's variants afterwards. It returns ErrProductNotFound, before writing anything, when the
// template does not exist. It creates the attribute, its values and the template line in three
// writes; if a later write fails, the error names the IDs already created. It does not retry.
func (c *Client) AddProductVariants(
	ctx context.Context,
	templateID int,
	attribute string,
	values []string,
) ([]Product, error) {
	var (
		err         error
		exists      bool
		attributeID int
		valueIDs    []int
		variants    []Product
	)

	if templateID <= 0 {
		return nil, invalidArgument("template ID must be positive")
	}
	if strings.TrimSpace(attribute) == "" {
		return nil, invalidArgument("attribute must not be empty")
	}
	if len(values) == 0 {
		return nil, invalidArgument("at least one value is required")
	}
	if exists, err = c.recordExists(ctx, "product.template", templateID); err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("%w: product.template %d", ErrProductNotFound, templateID)
	}
	if attributeID, err = c.CreateProductAttribute(ctx, attribute); err != nil {
		return nil, err
	}
	if valueIDs, err = c.CreateProductAttributeValues(ctx, attributeID, values); err != nil {
		return nil, fmt.Errorf("created product.attribute %d but creating its values failed: %w", attributeID, err)
	}
	if _, err = c.AddProductAttributeLine(ctx, templateID, attributeID, valueIDs); err != nil {
		return nil, fmt.Errorf(
			"created product.attribute %d and values %v but adding them to template %d failed: %w",
			attributeID, valueIDs, templateID, err,
		)
	}
	if variants, err = c.ProductVariants(ctx, templateID); err != nil {
		return nil, fmt.Errorf("added attribute line to template %d but reading its variants failed: %w", templateID, err)
	}
	return variants, nil
}

// ProductVariants returns the variants of one product.template, ordered by ID, with their display
// name, extra price and attribute value IDs (not stock tracking, which needs the stock module). Archived variants are not included.
func (c *Client) ProductVariants(ctx context.Context, templateID int) ([]Product, error) {
	var (
		err      error
		variants []Product
	)

	if templateID <= 0 {
		return nil, invalidArgument("template ID must be positive")
	}
	err = c.CallRead(ctx, "product.product", "search_read", map[string]any{
		"domain": []any{[]any{"product_tmpl_id", "=", templateID}},
		"fields": []string{
			"id", "name", "default_code", "list_price", "categ_id", "sale_ok", "product_tmpl_id", "display_name",
			"price_extra", "product_template_variant_value_ids",
		},
		"order": "id",
	}, &variants)
	if err != nil {
		return nil, fmt.Errorf("read product variants: %w", err)
	}
	return variants, nil
}
