package main

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/d2jvkpn/odoo-go"
)

// catalogSource is what the exporter and the snapshot loader read from an Odoo database.
type catalogSource interface {
	ListCatalogVariants(ctx context.Context, includeArchived bool) ([]odoo.CatalogVariant, error)
	ProductAttributeValues(ctx context.Context, ids []int) (map[int]odoo.ProductAttributeValue, error)
}

// buildExport reads every variant, active and archived, and groups them by template. An archived
// variant without default_code cannot be matched in another database and only repeats the value
// combination of the variant that replaced it, so it is left out; omitted counts them. The result
// is sorted so that exporting the same data twice gives the same file.
func buildExport(
	ctx context.Context,
	src catalogSource,
	name, baseURL string,
	now time.Time,
) (*CatalogFile, int, error) {
	var (
		omitted  int
		variants []odoo.CatalogVariant
		valueIDs []int
		values   map[int]odoo.ProductAttributeValue
		byTmpl   = make(map[int][]odoo.CatalogVariant)
		tmplIDs  []int
		cats     = make(map[string]bool)
		attrs    = make(map[string]map[string]bool)
		out      *CatalogFile
		t        FileTemplate
		err      error
	)

	out = &CatalogFile{
		Format: formatVersion, ExportedAt: now.UTC().Format(time.RFC3339), Include: []string{"products"},
		Source: FileSource{Name: name, BaseURL: baseURL},
	}
	if variants, err = src.ListCatalogVariants(ctx, true); err != nil {
		return nil, 0, fmt.Errorf("read source variants: %w", err)
	}
	for _, v := range variants {
		if !v.Active && v.DefaultCode == "" {
			omitted++
			continue
		}
		valueIDs = append(valueIDs, v.ValueIDs...)
		byTmpl[v.Template.ID] = append(byTmpl[v.Template.ID], v)
	}
	if values, err = src.ProductAttributeValues(ctx, valueIDs); err != nil {
		return nil, 0, fmt.Errorf("read source attribute values: %w", err)
	}
	for id := range byTmpl {
		tmplIDs = append(tmplIDs, id)
	}
	sort.Ints(tmplIDs)
	for _, id := range tmplIDs {
		if t, err = exportTemplate(byTmpl[id], values, attrs); err != nil {
			return nil, 0, err
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
	return out, omitted, nil
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
