package main

import (
	"context"
	"fmt"
	"iter"

	"github.com/d2jvkpn/odoo-go"
)

// snapshotAPI is what loadSnapshot reads from the target Odoo.
type snapshotAPI interface {
	catalogSource
	ListProductAttributes(ctx context.Context) ([]odoo.ProductAttribute, error)
	AllProductCategories(ctx context.Context, query odoo.ProductCategoryQuery) iter.Seq2[odoo.ProductCategory, error]
}

// SnapAttribute is a target attribute. Values holds its active values by name; ArchivedValues the
// names of its archived ones, which a new template cannot use.
type SnapAttribute struct {
	ID             int
	CreateVariant  string
	Archived       bool
	Values         map[string]int
	ArchivedValues map[string]bool
}

// Snapshot is everything the planner needs to know about the target, as plain data.
type Snapshot struct {
	Categories    map[string]int                     // full path -> product.category ID
	Attributes    map[string]SnapAttribute           // attribute name -> attribute
	UoMs          map[string]int                     // unit name -> uom.uom ID
	UnitDetails   map[string]odoo.SyncUoM            // unit name -> conversion definition
	DuplicateUoMs map[string]bool                    // names that identify multiple target units
	Variants      []odoo.CatalogVariant              // active and archived
	Values        map[int]odoo.ProductAttributeValue // template attribute value ID -> value, for Variants
	BOMs          []odoo.BOM                         // active and archived, with lines; nil unless requested
}

// loadSnapshot reads the target. It only reads. BOMs are read only when withBOMs is set, so a
// products-only import works on a database without the mrp module.
func loadSnapshot(ctx context.Context, api snapshotAPI, withBOMs bool) (*Snapshot, error) {
	var (
		snap = &Snapshot{
			Categories:    make(map[string]int),
			Attributes:    make(map[string]SnapAttribute),
			UoMs:          make(map[string]int),
			UnitDetails:   make(map[string]odoo.SyncUoM),
			DuplicateUoMs: make(map[string]bool),
		}
		attrs []odoo.ProductAttribute
		uoms  []odoo.SyncUoM
		ids   []int
		cat   odoo.ProductCategory
		err   error
	)

	for cat, err = range api.AllProductCategories(ctx, odoo.ProductCategoryQuery{Type: odoo.InternalCategory}) {
		if err != nil {
			return nil, fmt.Errorf("read target categories: %w", err)
		}
		snap.Categories[cat.CompleteName] = cat.ID
	}
	if attrs, err = api.ListProductAttributes(ctx); err != nil {
		return nil, fmt.Errorf("read target attributes: %w", err)
	}
	for _, a := range attrs {
		sa := SnapAttribute{
			ID: a.ID, CreateVariant: a.CreateVariant, Archived: !a.Active,
			Values: make(map[string]int), ArchivedValues: make(map[string]bool),
		}
		for _, v := range a.Values {
			if v.Active {
				sa.Values[v.Name] = v.ID
			} else {
				sa.ArchivedValues[v.Name] = true
			}
		}
		snap.Attributes[a.Name] = sa
	}
	if uoms, err = api.ListSyncUoMs(ctx); err != nil {
		return nil, fmt.Errorf("read target units of measure: %w", err)
	}
	for _, u := range uoms {
		if _, found := snap.UnitDetails[u.Name]; found {
			snap.DuplicateUoMs[u.Name] = true
		}
		snap.UnitDetails[u.Name] = u
		if u.Active {
			snap.UoMs[u.Name] = u.ID
		}
	}
	if snap.Variants, err = api.ListCatalogVariants(ctx, true); err != nil {
		return nil, fmt.Errorf("read target variants: %w", err)
	}
	for _, v := range snap.Variants {
		ids = append(ids, v.ValueIDs...)
	}
	if snap.Values, err = api.ProductAttributeValues(ctx, ids); err != nil {
		return nil, fmt.Errorf("read target attribute values: %w", err)
	}
	if withBOMs {
		if snap.BOMs, err = api.ListBOMs(ctx, true); err != nil {
			return nil, fmt.Errorf("read target BOMs: %w", err)
		}
	}
	return snap, nil
}
