package odoo

import (
	"context"
	"fmt"
	"strings"
)

// CategoryType selects either backend inventory categories or website categories.
type CategoryType string

const (
	// InternalCategory selects product.category.
	InternalCategory CategoryType = "internal"
	// WebsiteCategory selects product.public.category.
	WebsiteCategory CategoryType = "website"
)

// ProductCategory exposes category hierarchy fields. Internal categories use
// CompleteName; website categories use DisplayName and ChildIDs.
type ProductCategory struct {
	ID           int      `json:"id"`
	Name         string   `json:"name"`
	CompleteName string   `json:"complete_name,omitempty"`
	DisplayName  string   `json:"display_name,omitempty"`
	Parent       Many2One `json:"parent_id"`
	ChildIDs     []int    `json:"child_id,omitempty"`
}

// ProductCategoryQuery filters and paginates categories. Type defaults to
// InternalCategory and Order to "id". Limit defaults to 1000 for internal
// categories and 100 for website categories.
// ParentID nil searches all levels; zero selects roots; a positive ID selects
// direct children. Domain is combined with ParentID using implicit AND.
type ProductCategoryQuery struct {
	Type     CategoryType
	Domain   []any
	ParentID *int
	Limit    int
	Offset   int
	Order    string
}

// categoryModel maps a category type to its Odoo model name.
func categoryModel(kind CategoryType) (string, error) {
	switch kind {
	case "", InternalCategory:
		return "product.category", nil
	case WebsiteCategory:
		return "product.public.category", nil
	default:
		return "", invalidArgument("unknown category type %q; use internal or website", kind)
	}
}

// SearchProductCategories returns category names and parent relations, plus
// child IDs for website categories. Use ParentID to query direct children.
// Website categories require Odoo's website_sale module and appropriate access.
func (c *Client) SearchProductCategories(
	ctx context.Context,
	query ProductCategoryQuery,
) ([]ProductCategory, error) {
	var (
		err          error
		model        string
		fields       []string
		defaultLimit int
		domain       []any
		categories   []ProductCategory
	)

	if model, err = categoryModel(query.Type); err != nil {
		return nil, err
	}
	if query.Limit < 0 || query.Offset < 0 {
		return nil, invalidArgument("limit and offset must not be negative")
	}
	fields = []string{"id", "name", "complete_name", "parent_id"}
	defaultLimit = 1000
	if query.Type == WebsiteCategory {
		fields = []string{"id", "name", "display_name", "parent_id", "child_id"}
		defaultLimit = 100
	}
	if query.Limit == 0 {
		query.Limit = defaultLimit
	}
	if query.Order == "" {
		query.Order = "id"
	}
	if domain, err = categoryDomain(query); err != nil {
		return nil, err
	}
	categories = make([]ProductCategory, 0)
	err = c.CallRead(ctx, model, "search_read", map[string]any{
		"domain": domain,
		"fields": fields,
		"limit":  query.Limit,
		"offset": query.Offset,
		"order":  query.Order,
	}, &categories)
	if err != nil {
		return nil, fmt.Errorf("search product categories: %w", err)
	}
	return categories, nil
}

// GetProductCategory retrieves one category in the selected model.
func (c *Client) GetProductCategory(
	ctx context.Context,
	kind CategoryType,
	id int,
) (*ProductCategory, error) {
	var (
		err        error
		categories []ProductCategory
	)

	if id <= 0 {
		return nil, invalidArgument("category ID must be positive")
	}
	categories, err = c.SearchProductCategories(ctx, ProductCategoryQuery{
		Type:   kind,
		Domain: []any{[]any{"id", "=", id}},
		Limit:  1,
	})
	if err != nil {
		return nil, err
	}
	if len(categories) == 0 {
		return nil, fmt.Errorf("%w: %d", ErrProductCategoryNotFound, id)
	}
	return &categories[0], nil
}

// categoryDomain combines the query's Domain with its ParentID condition using implicit AND.
// It copies Domain, so the caller's slice is never modified.
func categoryDomain(query ProductCategoryQuery) ([]any, error) {
	var (
		domain []any
		parent any
	)

	if query.ParentID != nil && *query.ParentID < 0 {
		return nil, invalidArgument("parent ID must not be negative")
	}
	domain = make([]any, len(query.Domain), len(query.Domain)+1)
	copy(domain, query.Domain)
	if query.ParentID != nil {
		parent = *query.ParentID
		if *query.ParentID == 0 {
			parent = false
		}
		domain = append(domain, []any{"parent_id", "=", parent})
	}
	return domain, nil
}

// CreateProductCategoryRequest describes a new category. The JSON names are Odoo's field names. ParentID zero
// creates a root category.
type CreateProductCategoryRequest struct {
	Name     string `json:"name"`
	ParentID int    `json:"parent_id,omitempty"`
}

// CreateProductCategory creates one category in the selected model and returns its ID. It does not retry.
// After a transport/decode error the write outcome may be unknown; check Odoo before resubmitting, to avoid
// duplicates.
func (c *Client) CreateProductCategory(
	ctx context.Context,
	kind CategoryType,
	input CreateProductCategoryRequest,
) (int, error) {
	var (
		err   error
		model string
		ids   []int
	)

	if model, err = categoryModel(kind); err != nil {
		return 0, err
	}
	if strings.TrimSpace(input.Name) == "" {
		return 0, invalidArgument("name must not be empty")
	}
	if input.ParentID < 0 {
		return 0, invalidArgument("parent_id must not be negative")
	}
	err = c.Call(ctx, model, "create", map[string]any{"vals_list": []any{input}}, &ids)
	if err != nil {
		return 0, fmt.Errorf("create product category: %w", err)
	}
	if len(ids) != 1 || ids[0] <= 0 {
		return 0, fmt.Errorf(
			"%w: expected one positive ID from create (category may already exist)", ErrUnexpectedResponse,
		)
	}
	return ids[0], nil
}

// UpdateProductCategoryRequest changes selected fields of a category. Nil fields are left unchanged;
// a pointer to zero ParentID detaches the category from its parent.
type UpdateProductCategoryRequest struct {
	Name     *string `json:"name,omitempty"`
	ParentID *int    `json:"parent_id,omitempty"`
}

// UpdateProductCategory changes one category of the selected type. It returns
// ErrProductCategoryNotFound for an unknown ID and does not retry.
func (c *Client) UpdateProductCategory(
	ctx context.Context,
	kind CategoryType,
	id int,
	patch UpdateProductCategoryRequest,
) error {
	var (
		err   error
		model string
	)

	if model, err = categoryModel(kind); err != nil {
		return err
	}
	if patch.Name != nil && strings.TrimSpace(*patch.Name) == "" {
		return invalidArgument("name must not be empty")
	}
	if patch.ParentID != nil && *patch.ParentID < 0 {
		return invalidArgument("parent_id must not be negative")
	}
	return c.updateRecord(ctx, model, id, patch, ErrProductCategoryNotFound)
}
