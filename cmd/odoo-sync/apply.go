package main

import (
	"context"
	"encoding/csv"
	"fmt"
	"maps"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/d2jvkpn/odoo-go"
)

// targetAPI is what the applier writes to; *odoo.Client satisfies it. Writes are never retried.
type targetAPI interface {
	CreateProductCategory(ctx context.Context, kind odoo.CategoryType, in odoo.CreateProductCategoryRequest) (int, error)
	CreateProductAttribute(ctx context.Context, name string) (int, error)
	CreateProductAttributeValues(ctx context.Context, attributeID int, names []string) ([]int, error)
	CreateProductTemplate(ctx context.Context, in odoo.CreateTemplateRequest) (int, error)
	AddProductAttributeLine(ctx context.Context, templateID, attributeID int, valueIDs []int) (int, error)
	ProductVariants(ctx context.Context, templateID int) ([]odoo.Product, error)
	ProductAttributeValues(ctx context.Context, ids []int) (map[int]odoo.ProductAttributeValue, error)
	SetVariantPriceExtra(ctx context.Context, templateValueID int, extra float64) error
	UpdateProductTemplate(ctx context.Context, id int, patch odoo.TemplatePatch) error
	UpdateVariantFields(ctx context.Context, id int, patch odoo.VariantPatch) error
	SetProductActive(ctx context.Context, id int, active bool) error
	SetProductTemplateActive(ctx context.Context, id int, active bool) error
}

// MapRow relates a source variant to its target variant, by default_code.
type MapRow struct {
	DefaultCode      string
	SourceID         int
	TargetID         int
	TargetTemplateID int
}

// Result is what an import did.
type Result struct {
	Created, Updated, Unchanged, Archived, Skipped, Failed int
	Map                                                    []MapRow
	Failures, Warnings                                     []string
}

type applier struct {
	api     targetAPI
	catIDs  map[string]int
	attrIDs map[string]int
	valIDs  map[string]map[string]int
	uomIDs  map[string]int
	res     *Result
}

// applyPlan executes a plan that has no problems. A failure in a dependency (category, attribute)
// aborts; a failure in one template is recorded and the run goes on with the next one.
func applyPlan(ctx context.Context, api targetAPI, snap *Snapshot, p *Plan) (*Result, error) {
	var (
		a = &applier{
			api: api, catIDs: maps.Clone(snap.Categories), attrIDs: make(map[string]int),
			valIDs: make(map[string]map[string]int), uomIDs: snap.UoMs, res: &Result{Skipped: len(p.Skipped)},
		}
		err error
	)

	for name, attr := range snap.Attributes {
		a.attrIDs[name] = attr.ID
		a.valIDs[name] = maps.Clone(attr.Values)
	}
	if err = a.createCategories(ctx, p.NewCategories); err != nil {
		return a.res, err
	}
	if err = a.createAttributes(ctx, p); err != nil {
		return a.res, err
	}
	for _, tp := range p.Templates {
		if err = ctx.Err(); err != nil {
			return a.res, err
		}
		if err = a.applyTemplate(ctx, tp); err != nil {
			a.res.Failed++
			a.res.Failures = append(a.res.Failures, fmt.Sprintf("template %d %q: %v", tp.Template.SourceID, tp.Template.Name, err))
		}
	}
	return a.res, nil
}

func (a *applier) createCategories(ctx context.Context, paths []string) error {
	for _, path := range paths {
		parts := strings.Split(path, " / ")
		parentID := 0
		if len(parts) > 1 {
			if parentID = a.catIDs[strings.Join(parts[:len(parts)-1], " / ")]; parentID == 0 {
				return fmt.Errorf("create category %q: its parent was not created", path)
			}
		}
		id, err := a.api.CreateProductCategory(ctx, odoo.InternalCategory, odoo.CreateProductCategoryRequest{
			Name: parts[len(parts)-1], ParentID: parentID,
		})
		if err != nil {
			return fmt.Errorf("create category %q: %w", path, err)
		}
		a.catIDs[path] = id
	}
	return nil
}

func (a *applier) createAttributes(ctx context.Context, p *Plan) error {
	for _, name := range p.NewAttributes {
		id, err := a.api.CreateProductAttribute(ctx, name)
		if err != nil {
			return fmt.Errorf("create attribute %q: %w", name, err)
		}
		a.attrIDs[name] = id
		a.valIDs[name] = make(map[string]int)
	}
	for _, attr := range sortedKeys(p.NewValues) {
		names := p.NewValues[attr]
		ids, err := a.api.CreateProductAttributeValues(ctx, a.attrIDs[attr], names)
		if err != nil {
			return fmt.Errorf("create values of attribute %q: %w", attr, err)
		}
		for i, name := range names {
			a.valIDs[attr][name] = ids[i]
		}
	}
	return nil
}

func (a *applier) applyTemplate(ctx context.Context, tp TemplatePlan) error {
	if tp.Creates() {
		return a.createTemplate(ctx, tp)
	}
	return a.updateTemplate(ctx, tp)
}

// placement pairs a variant Odoo generated with the file variant it matches by value combination.
type placement struct {
	variant odoo.Product
	file    FileVariant
}

// unlistedVariant is a variant Odoo generated for a combination the file does not list.
type unlistedVariant struct {
	variant odoo.Product
	key     string
}

// createTemplate creates the template, attaches its attribute lines so that Odoo generates the
// variants, then writes the per-variant fields onto the generated variants by value combination.
func (a *applier) createTemplate(ctx context.Context, tp TemplatePlan) error {
	var (
		t        = tp.Template
		tmplID   int
		targets  []odoo.Product
		valueIDs []int
		values   map[int]odoo.ProductAttributeValue
		byKey    = make(map[string]FileVariant)
		prices   = make(map[string]float64)
		seen     = make(map[string]bool)
		placed   []placement
		unlisted []unlistedVariant
		active   int
		err      error
	)

	partial := func(step string, err error) error {
		return fmt.Errorf(
			"template %d was created but %s failed; clear the default_code of its variants or finish it by hand "+
				"in the target before rerunning (archiving it does not help): %w", tmplID, step, err,
		)
	}
	tmplID, err = a.api.CreateProductTemplate(ctx, odoo.CreateTemplateRequest{
		Name: t.Name, ListPrice: t.ListPrice, CategoryID: a.catIDs[t.Category], SaleOK: t.SaleOK,
		IsStorable: t.IsStorable, Type: t.Type, UoMID: a.uomIDs[t.UoM], Description: t.DescriptionSale,
	})
	if err != nil {
		return fmt.Errorf("create template: %w", err)
	}
	for _, line := range t.AttributeLines {
		ids := make([]int, 0, len(line.Values))
		for _, v := range line.Values {
			ids = append(ids, a.valIDs[line.Attribute][v.Name])
			prices[line.Attribute+"\x00"+v.Name] = v.PriceExtra
		}
		if _, err = a.api.AddProductAttributeLine(ctx, tmplID, a.attrIDs[line.Attribute], ids); err != nil {
			return partial("adding attribute "+strconv.Quote(line.Attribute), err)
		}
	}
	if targets, err = a.api.ProductVariants(ctx, tmplID); err != nil {
		return partial("reading its variants", err)
	}
	for _, tv := range targets {
		valueIDs = append(valueIDs, tv.ValueIDs...)
	}
	if values, err = a.api.ProductAttributeValues(ctx, valueIDs); err != nil {
		return partial("reading its attribute values", err)
	}
	for _, fv := range t.Variants {
		byKey[variantKey(fv.Values)] = fv
		if fv.Active {
			active++
		}
	}
	// Place every generated variant before writing anything, so that a template Odoo did not fully
	// generate is reported without a half-written set of codes.
	for _, tv := range targets {
		combo := make(map[string]string)
		for _, id := range tv.ValueIDs {
			combo[values[id].Attribute.Name] = values[id].Value
		}
		key := variantKey(combo)
		fv, ok := byKey[key]
		if !ok {
			unlisted = append(unlisted, unlistedVariant{variant: tv, key: key})
			continue
		}
		placed = append(placed, placement{variant: tv, file: fv})
		seen[key] = true
	}
	if len(seen) != len(byKey) {
		return partial("placing its variants", fmt.Errorf(
			"Odoo did not generate %d of the file's %d variant combinations; every attribute of the template must have create_variant \"always\"",
			len(byKey)-len(seen), len(byKey),
		))
	}
	ids := make([]int, 0, len(values))
	for id := range values {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	for _, id := range ids {
		want := prices[values[id].Attribute.Name+"\x00"+values[id].Value]
		if !sameNumber(want, values[id].PriceExtra) {
			if err = a.api.SetVariantPriceExtra(ctx, id, want); err != nil {
				return partial("setting a price extra", err)
			}
		}
	}
	for _, pl := range placed {
		if err = a.api.UpdateVariantFields(ctx, pl.variant.ID, variantPatch(pl.file, nil)); err != nil {
			return partial("writing variant "+strconv.Itoa(pl.variant.ID), err)
		}
		a.res.Map = append(a.res.Map, MapRow{
			DefaultCode: pl.file.DefaultCode, SourceID: pl.file.SourceID, TargetID: pl.variant.ID, TargetTemplateID: tmplID,
		})
		if !pl.file.Active && active > 0 {
			if err = a.api.SetProductActive(ctx, pl.variant.ID, false); err != nil {
				return partial("archiving variant "+strconv.Itoa(pl.variant.ID), err)
			}
			a.res.Archived++
		}
	}
	// A combination Odoo generated but the file does not list (an excluded or archived one in the
	// source) must not stay on sale without a code.
	for _, u := range unlisted {
		if active > 0 {
			if err = a.api.SetProductActive(ctx, u.variant.ID, false); err != nil {
				return partial("archiving variant "+strconv.Itoa(u.variant.ID), err)
			}
			a.res.Archived++
		}
		a.res.Warnings = append(a.res.Warnings, fmt.Sprintf(
			"template %d: Odoo generated variant %d (%s) that the file does not list; it has no default_code and was archived",
			tmplID, u.variant.ID, u.key,
		))
	}
	if active == 0 {
		if err = a.api.SetProductTemplateActive(ctx, tmplID, false); err != nil {
			return partial("archiving it", err)
		}
		a.res.Archived++
	}
	a.res.Created++
	return nil
}

// updateTemplate writes only what the planner found different.
func (a *applier) updateTemplate(ctx context.Context, tp TemplatePlan) error {
	var err error

	if len(tp.Diffs) > 0 {
		patch := templatePatch(tp.Template, tp.Diffs, a.catIDs[tp.Template.Category], a.uomIDs[tp.Template.UoM])
		if err = a.api.UpdateProductTemplate(ctx, tp.TargetID, patch); err != nil {
			return fmt.Errorf("update template %d: %w", tp.TargetID, err)
		}
	}
	for _, pe := range tp.PriceExtras {
		if err = a.api.SetVariantPriceExtra(ctx, pe.ValueID, pe.New); err != nil {
			return fmt.Errorf("set price extra of %s=%s: %w", pe.Attribute, pe.Value, err)
		}
	}
	if tp.SetActive != nil {
		if err = a.api.SetProductTemplateActive(ctx, tp.TargetID, *tp.SetActive); err != nil {
			return fmt.Errorf("set template %d active=%t: %w", tp.TargetID, *tp.SetActive, err)
		}
		if !*tp.SetActive {
			a.res.Archived++
		}
	}
	for _, vp := range tp.Variants {
		if len(vp.Diffs) > 0 {
			if err = a.api.UpdateVariantFields(ctx, vp.TargetID, variantPatch(vp.File, vp.Diffs)); err != nil {
				return fmt.Errorf("update variant %d: %w", vp.TargetID, err)
			}
		}
		if vp.SetActive != nil {
			if err = a.api.SetProductActive(ctx, vp.TargetID, *vp.SetActive); err != nil {
				return fmt.Errorf("set variant %d active=%t: %w", vp.TargetID, *vp.SetActive, err)
			}
			if !*vp.SetActive {
				a.res.Archived++
			}
		}
		a.res.Map = append(a.res.Map, MapRow{
			DefaultCode: vp.File.DefaultCode, SourceID: vp.File.SourceID, TargetID: vp.TargetID, TargetTemplateID: tp.TargetID,
		})
	}
	if tp.Changes() {
		a.res.Updated++
	} else {
		a.res.Unchanged++
	}
	return nil
}

// templatePatch builds the template write for the fields named in diffs.
func templatePatch(t FileTemplate, diffs []Diff, categoryID, uomID int) odoo.TemplatePatch {
	var p odoo.TemplatePatch

	for _, d := range diffs {
		switch d.Field {
		case fieldName:
			p.Name = &t.Name
		case fieldListPrice:
			p.ListPrice = &t.ListPrice
		case fieldCategory:
			p.CategoryID = &categoryID
		case fieldSaleOK:
			p.SaleOK = &t.SaleOK
		case fieldIsStorable:
			p.IsStorable = &t.IsStorable
		case fieldType:
			p.Type = &t.Type
		case fieldUoM:
			p.UoMID = &uomID
		case fieldDescription:
			p.Description = &t.DescriptionSale
		}
	}
	return p
}

// variantPatch builds the variant write. With nil diffs (a new variant) it writes the code and every
// non-empty value; otherwise only the fields named in diffs.
func variantPatch(v FileVariant, diffs []Diff) odoo.VariantPatch {
	var p odoo.VariantPatch

	if diffs == nil {
		p.DefaultCode = &v.DefaultCode
		if v.Barcode != "" {
			p.Barcode = &v.Barcode
		}
		if v.Weight != 0 {
			p.Weight = &v.Weight
		}
		if v.Volume != 0 {
			p.Volume = &v.Volume
		}
		return p
	}
	for _, d := range diffs {
		switch d.Field {
		case fieldBarcode:
			p.Barcode = &v.Barcode
		case fieldWeight:
			p.Weight = &v.Weight
		case fieldVolume:
			p.Volume = &v.Volume
		}
	}
	return p
}

// writeMapCSV writes the source-to-target mapping, sorted by default_code.
func writeMapCSV(path string, rows []MapRow) error {
	var (
		f   *os.File
		w   *csv.Writer
		err error
	)

	sort.Slice(rows, func(i, j int) bool { return rows[i].DefaultCode < rows[j].DefaultCode })
	if f, err = os.Create(path); err != nil {
		return fmt.Errorf("write mapping: %w", err)
	}
	defer f.Close()
	w = csv.NewWriter(f)
	_ = w.Write([]string{"default_code", "source_variant_id", "target_variant_id", "target_template_id"})
	for _, r := range rows {
		_ = w.Write([]string{r.DefaultCode, strconv.Itoa(r.SourceID), strconv.Itoa(r.TargetID), strconv.Itoa(r.TargetTemplateID)})
	}
	w.Flush()
	if err = w.Error(); err != nil {
		return fmt.Errorf("write mapping: %w", err)
	}
	return nil
}
