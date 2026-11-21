package main

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"time"

	"github.com/d2jvkpn/odoo-go"
)

// catalogSource is what the exporter and the snapshot loader read from an Odoo database.
type catalogSource interface {
	ListCatalogVariants(ctx context.Context, includeArchived bool) ([]odoo.CatalogVariant, error)
	ProductAttributeValues(ctx context.Context, ids []int) (map[int]odoo.ProductAttributeValue, error)
	ListBOMs(ctx context.Context, includeArchived bool) ([]odoo.BOM, error)
}

// buildExport reads every variant, active and archived, and groups them by template. An archived
// variant without default_code cannot be matched in another database and only repeats the value
// combination of the variant that replaced it, so it is left out; omitted counts them. The result
// is sorted so that exporting the same data twice gives the same file. include is "products", "boms"
// or "all"; BOMs need the variants too, for the codes they reference.
func buildExport(
	ctx context.Context,
	src catalogSource,
	name, baseURL string,
	now time.Time,
	include string,
) (*CatalogFile, int, error) {
	var (
		omitted  int
		variants []odoo.CatalogVariant
		out      *CatalogFile
		err      error
	)

	out = &CatalogFile{
		Format: formatVersion, ExportedAt: now.UTC().Format(time.RFC3339),
		Source: FileSource{Name: name, BaseURL: baseURL},
	}
	if include != "boms" {
		out.Include = append(out.Include, "products")
	}
	if include != "products" {
		out.Include = append(out.Include, "boms")
	}
	if variants, err = src.ListCatalogVariants(ctx, true); err != nil {
		return nil, 0, fmt.Errorf("read source variants: %w", err)
	}
	if include != "boms" {
		if omitted, err = buildProducts(ctx, src, out, variants); err != nil {
			return nil, 0, err
		}
	}
	if include != "products" {
		var boms []odoo.BOM

		if boms, err = src.ListBOMs(ctx, true); err != nil {
			return nil, 0, fmt.Errorf("read source BOMs: %w", err)
		}
		if out.BOMs, err = exportBOMs(boms, variants); err != nil {
			return nil, 0, err
		}
	}
	return out, omitted, nil
}

// buildProducts fills the categories, attributes and templates of out from the variants and returns
// how many archived variants without a code it left out.
func buildProducts(ctx context.Context, src catalogSource, out *CatalogFile, variants []odoo.CatalogVariant) (int, error) {
	var (
		omitted  int
		valueIDs []int
		values   map[int]odoo.ProductAttributeValue
		byTmpl   = make(map[int][]odoo.CatalogVariant)
		tmplIDs  []int
		cats     = make(map[string]bool)
		attrs    = make(map[string]map[string]bool)
		t        FileTemplate
		err      error
	)

	for _, v := range variants {
		if !v.Active && v.DefaultCode == "" {
			omitted++
			continue
		}
		valueIDs = append(valueIDs, v.ValueIDs...)
		byTmpl[v.Template.ID] = append(byTmpl[v.Template.ID], v)
	}
	if values, err = src.ProductAttributeValues(ctx, valueIDs); err != nil {
		return 0, fmt.Errorf("read source attribute values: %w", err)
	}
	for id := range byTmpl {
		tmplIDs = append(tmplIDs, id)
	}
	sort.Ints(tmplIDs)
	for _, id := range tmplIDs {
		if t, err = exportTemplate(byTmpl[id], values, attrs); err != nil {
			return 0, err
		}
		if t.Category != "" {
			cats[t.Category] = true
		}
		out.Templates = append(out.Templates, t)
	}
	for _, path := range sortedKeys(cats) {
		out.Categories = append(out.Categories, FileCategory{Path: path})
	}
	for _, attr := range sortedKeys(attrs) {
		out.Attributes = append(out.Attributes, FileAttribute{Name: attr, Values: sortedKeys(attrs[attr])})
	}
	return omitted, nil
}

// exportBOMs converts source BOMs to file BOMs, sorted by source id. A BOM with operations,
// byproducts or attribute-restricted lines is refused: the file cannot carry them.
func exportBOMs(boms []odoo.BOM, variants []odoo.CatalogVariant) ([]FileBOM, error) {
	var (
		codes    = make(map[int]string, len(variants))
		tmplCode = make(map[int][]string)
		out      = make([]FileBOM, 0, len(boms))
	)

	for _, v := range variants {
		codes[v.ID] = string(v.DefaultCode)
		if v.Active && v.DefaultCode != "" {
			tmplCode[v.Template.ID] = append(tmplCode[v.Template.ID], string(v.DefaultCode))
		}
	}
	for _, b := range boms {
		label := fmt.Sprintf("bom %d", b.ID)
		if len(b.OperationIDs) > 0 {
			return nil, fmt.Errorf("%s has operations, which odoo-sync does not support", label)
		}
		if len(b.ByproductIDs) > 0 {
			return nil, fmt.Errorf("%s has byproducts, which odoo-sync does not support", label)
		}
		fb := FileBOM{
			SourceID: b.ID, Template: FileBOMTemplate{SourceID: b.Template.ID}, Type: b.Type,
			ProductQty: b.Quantity, UoM: b.UoM.Name, Code: string(b.Code), Active: b.Active,
			Lines: make([]FileBOMLine, 0, len(b.Lines)),
		}
		if b.Variant.ID != 0 {
			code, ok := codes[b.Variant.ID]
			if !ok {
				return nil, fmt.Errorf("%s names variant %d that Odoo did not return", label, b.Variant.ID)
			}
			fb.Product = &FileBOMProduct{DefaultCode: code}
		} else {
			fb.Template.DefaultCodes = slices.Sorted(slices.Values(tmplCode[b.Template.ID]))
		}
		for _, l := range b.Lines {
			if len(l.ValueIDs) > 0 {
				return nil, fmt.Errorf("%s has lines restricted by attribute value, which odoo-sync does not support", label)
			}
			if l.OperationID.ID != 0 {
				return nil, fmt.Errorf("%s has operations, which odoo-sync does not support", label)
			}
			code, ok := codes[l.Product.ID]
			if !ok {
				return nil, fmt.Errorf("%s uses component variant %d that Odoo did not return", label, l.Product.ID)
			}
			fb.Lines = append(fb.Lines, FileBOMLine{Component: code, Qty: l.Quantity, UoM: l.UoM.Name})
		}
		out = append(out, fb)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SourceID < out[j].SourceID })
	return out, nil
}

// exportTemplate builds one template from its variants and records the attribute values it uses.
func exportTemplate(
	vs []odoo.CatalogVariant,
	values map[int]odoo.ProductAttributeValue,
	attrs map[string]map[string]bool,
) (FileTemplate, error) {
	var (
		t      FileTemplate
		prices = make(map[string]map[string]float64)
	)

	sort.Slice(vs, func(i, j int) bool { return vs[i].ID < vs[j].ID })
	t = FileTemplate{
		SourceID: vs[0].Template.ID, Name: vs[0].Name, Category: vs[0].Category.Name, ListPrice: vs[0].ListPrice,
		SaleOK: vs[0].SaleOK, Type: vs[0].Type, IsStorable: vs[0].IsStorable, UoM: vs[0].UoM.Name,
		DescriptionSale: string(vs[0].Description),
	}
	for _, v := range vs {
		fv := FileVariant{
			SourceID: v.ID, DefaultCode: string(v.DefaultCode), Values: make(map[string]string),
			Barcode: string(v.Barcode), Weight: v.Weight, Volume: v.Volume, Active: v.Active,
		}
		for _, valueID := range v.ValueIDs {
			val, ok := values[valueID]
			if !ok {
				return t, fmt.Errorf("variant %d references attribute value %d that Odoo did not return", v.ID, valueID)
			}
			fv.Values[val.Attribute.Name] = val.Value
			if prices[val.Attribute.Name] == nil {
				prices[val.Attribute.Name] = make(map[string]float64)
			}
			prices[val.Attribute.Name][val.Value] = val.PriceExtra
			if attrs[val.Attribute.Name] == nil {
				attrs[val.Attribute.Name] = make(map[string]bool)
			}
			attrs[val.Attribute.Name][val.Value] = true
		}
		t.Variants = append(t.Variants, fv)
	}
	for _, attr := range sortedKeys(prices) {
		line := FileAttrLine{Attribute: attr}
		for _, name := range sortedKeys(prices[attr]) {
			line.Values = append(line.Values, FileAttrValue{Name: name, PriceExtra: prices[attr][name]})
		}
		t.AttributeLines = append(t.AttributeLines, line)
	}
	return t, nil
}

// sortedKeys returns the keys of m in ascending order.
func sortedKeys[V any](m map[string]V) []string {
	var keys = make([]string, 0, len(m))

	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
