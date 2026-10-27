package odoo

import (
	"context"
	"fmt"
	"math"
	"strings"
)

// Product is a product.product variant. DefaultCode is the internal reference
// (SKU) and is empty when unset.
type Product struct {
	ID          int            `json:"id"`
	Name        string         `json:"name"`
	DefaultCode OptionalString `json:"default_code"`
	ListPrice   float64        `json:"list_price"`
	Category    Many2One       `json:"categ_id"`
}

// ProductQuery filters and paginates products. Limit defaults to 100 and
// Order to "id". Domain is an Odoo domain; nil matches every visible product.
type ProductQuery struct {
	Domain []any
	Limit  int
	Offset int
	Order  string
}

// SearchProducts returns product.product records matching the query.
func (c *Client) SearchProducts(ctx context.Context, query ProductQuery) ([]Product, error) {
	var (
		err      error
		domain   []any
		products []Product
	)

	if query.Limit < 0 || query.Offset < 0 {
		return nil, invalidArgument("limit and offset must not be negative")
	}
	if query.Limit == 0 {
		query.Limit = 100
	}
	if query.Order == "" {
		query.Order = "id"
	}
	domain = query.Domain
	if domain == nil {
		domain = []any{}
	}
	products = make([]Product, 0)
	err = c.CallRead(ctx, "product.product", "search_read", map[string]any{
		"domain": domain,
		"fields": []string{"id", "name", "default_code", "list_price", "categ_id"},
		"limit":  query.Limit,
		"offset": query.Offset,
		"order":  query.Order,
	}, &products)
	if err != nil {
		return nil, fmt.Errorf("search products: %w", err)
	}
	return products, nil
}

// GetProduct retrieves one product by ID.
func (c *Client) GetProduct(ctx context.Context, id int) (*Product, error) {
	var (
		err      error
		products []Product
	)

	if id <= 0 {
		return nil, invalidArgument("product ID must be positive")
	}
	products, err = c.SearchProducts(ctx, ProductQuery{
		Domain: []any{[]any{"id", "=", id}},
		Limit:  1,
	})
	if err != nil {
		return nil, err
	}
	if len(products) == 0 {
		return nil, fmt.Errorf("%w: %d", ErrProductNotFound, id)
	}
	return &products[0], nil
}

// CreateProductRequest describes a new product. The JSON names are Odoo's field names. Nil ListPrice and SaleOK
// leave Odoo's defaults in place; a pointer to zero or false is sent explicitly. CategoryID zero leaves the
// default category.
type CreateProductRequest struct {
	Name        string   `json:"name"`
	ListPrice   *float64 `json:"list_price,omitempty"`
	DefaultCode string   `json:"default_code,omitempty"`
	CategoryID  int      `json:"categ_id,omitempty"`
	SaleOK      *bool    `json:"sale_ok,omitempty"`
}

// validateCreateProduct checks the request shape and numeric bounds. Odoo-side rules are left to the server.
func validateCreateProduct(input CreateProductRequest) error {
	var price *float64

	if strings.TrimSpace(input.Name) == "" {
		return invalidArgument("name must not be empty")
	}
	price = input.ListPrice
	if price != nil && (math.IsNaN(*price) || math.IsInf(*price, 0) || *price < 0) {
		return invalidArgument("list_price must be finite and nonnegative")
	}
	if input.CategoryID < 0 {
		return invalidArgument("categ_id must not be negative")
	}
	return nil
}

// CreateProduct creates one product.template and returns the ID of its product.product variant, the ID used by
// GetProduct and order lines. It does not retry. After a transport/decode error the write outcome may be
// unknown; check Odoo before resubmitting, to avoid duplicates (Odoo does not require default_code to be
// unique). If the template was created but its variant cannot be resolved, the error names the template ID.
func (c *Client) CreateProduct(ctx context.Context, input CreateProductRequest) (int, error) {
	var (
		err       error
		ids       []int
		templates []struct {
			Variant Many2One `json:"product_variant_id"`
		}
	)

	if err = validateCreateProduct(input); err != nil {
		return 0, err
	}
	err = c.Call(ctx, "product.template", "create", map[string]any{"vals_list": []any{input}}, &ids)
	if err != nil {
		return 0, fmt.Errorf("create product: %w", err)
	}
	if len(ids) != 1 || ids[0] <= 0 {
		return 0, fmt.Errorf(
			"%w: expected one positive ID from create (product may already exist)", ErrUnexpectedResponse,
		)
	}
	err = c.CallRead(ctx, "product.template", "read", map[string]any{
		"ids": ids, "fields": []string{"product_variant_id"},
	}, &templates)
	if err != nil {
		return 0, fmt.Errorf("created product.template %d but reading its variant failed: %w", ids[0], err)
	}
	if len(templates) != 1 || templates[0].Variant.ID <= 0 {
		return 0, fmt.Errorf(
			"%w: created product.template %d has no product variant", ErrUnexpectedResponse, ids[0],
		)
	}
	return templates[0].Variant.ID, nil
}
