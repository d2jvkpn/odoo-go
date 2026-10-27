package odoo

import (
	"context"
	"fmt"
	"iter"
)

// count runs search_count for a model and domain.
func (c *Client) count(ctx context.Context, model string, domain []any) (int, error) {
	var (
		total int
		err   error
	)

	if domain == nil {
		domain = []any{}
	}
	if err = c.CallRead(ctx, model, "search_count", map[string]any{"domain": domain}, &total); err != nil {
		return 0, fmt.Errorf("count %s: %w", model, err)
	}
	return total, nil
}

// paginate yields every record by fetching pages of pageSize starting at offset. It stops after a
// short page, after the first error (yielded once), or when the consumer stops iterating.
// Offset pagination can skip or repeat records if the data changes between pages.
func paginate[T any](
	pageSize, offset int,
	fetch func(limit, offset int) ([]T, error),
) iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		var (
			page []T
			item T
			err  error
		)

		for {
			if page, err = fetch(pageSize, offset); err != nil {
				var zero T
				yield(zero, err)
				return
			}
			for _, item = range page {
				if !yield(item, nil) {
					return
				}
			}
			if len(page) < pageSize {
				return
			}
			offset += len(page)
		}
	}
}

// allPages validates the paging parameters and applies the default page size. An invalid query
// surfaces as the first (and only) yielded error.
func allPages[T any](
	limit, offset, def int,
	fetch func(limit, offset int) ([]T, error),
) iter.Seq2[T, error] {
	if limit < 0 || offset < 0 {
		return func(yield func(T, error) bool) {
			var zero T

			yield(zero, invalidArgument("limit and offset must not be negative"))
		}
	}
	if limit == 0 {
		limit = def
	}
	return paginate(limit, offset, fetch)
}

// CountProducts returns how many products match domain; a nil domain counts every visible product.
func (c *Client) CountProducts(ctx context.Context, domain []any) (int, error) {
	return c.count(ctx, "product.product", domain)
}

// AllProducts iterates over every product matching the query. Limit is the page size (default 100)
// and Offset the starting position. The first error ends the iteration. Calls issued by the
// iterator follow the client's retry policy.
func (c *Client) AllProducts(ctx context.Context, query ProductQuery) iter.Seq2[Product, error] {
	return allPages(query.Limit, query.Offset, 100, func(limit, offset int) ([]Product, error) {
		query.Limit, query.Offset = limit, offset
		return c.SearchProducts(ctx, query)
	})
}

// CountSaleOrders returns how many orders match domain; a nil domain counts every visible order.
func (c *Client) CountSaleOrders(ctx context.Context, domain []any) (int, error) {
	return c.count(ctx, "sale.order", domain)
}

// AllSaleOrders iterates over every order matching the query. Limit is the page size (default 100)
// and Offset the starting position. The first error ends the iteration.
func (c *Client) AllSaleOrders(ctx context.Context, query SaleOrderQuery) iter.Seq2[SaleOrder, error] {
	return allPages(query.Limit, query.Offset, 100, func(limit, offset int) ([]SaleOrder, error) {
		query.Limit, query.Offset = limit, offset
		return c.SearchSaleOrders(ctx, query)
	})
}

// CountProductCategories returns how many categories match the query's Type, Domain and ParentID;
// Limit, Offset and Order are ignored.
func (c *Client) CountProductCategories(ctx context.Context, query ProductCategoryQuery) (int, error) {
	var (
		model  string
		domain []any
		err    error
	)

	if model, err = categoryModel(query.Type); err != nil {
		return 0, err
	}
	if domain, err = categoryDomain(query); err != nil {
		return 0, err
	}
	return c.count(ctx, model, domain)
}

// AllProductCategories iterates over every category matching the query. Limit is the page size
// (default 1000 for internal and 100 for website categories) and Offset the starting position.
// The first error ends the iteration.
func (c *Client) AllProductCategories(
	ctx context.Context,
	query ProductCategoryQuery,
) iter.Seq2[ProductCategory, error] {
	var def int = 1000

	if query.Type == WebsiteCategory {
		def = 100
	}
	return allPages(query.Limit, query.Offset, def, func(limit, offset int) ([]ProductCategory, error) {
		query.Limit, query.Offset = limit, offset
		return c.SearchProductCategories(ctx, query)
	})
}
