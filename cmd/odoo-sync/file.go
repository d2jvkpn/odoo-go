package main

import (
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
)

// formatVersion is the only export file format this tool reads and writes.
const formatVersion = 2

// CatalogFile is the export file. Source ids are records only and are never used to match.
type CatalogFile struct {
	Format     int             `json:"format"`
	ExportedAt string          `json:"exported_at"`
	Section    string          `json:"section"`
	Source     FileSource      `json:"source"`
	Units      []FileUoM       `json:"units,omitempty"`
	Categories []FileCategory  `json:"categories,omitempty"`
	Attributes []FileAttribute `json:"attributes,omitempty"`
	Templates  []FileTemplate  `json:"templates,omitempty"`
	BOMs       []FileBOM       `json:"boms,omitempty"`
}

// FileSource says where the file came from; it is informational.
type FileSource struct {
	Filename string `json:"filename"`
	BaseURL  string `json:"base_url"`
}

// FileUoM describes an Odoo 19 unit. RelativeTo names its reference unit;
// an empty value means this is a root unit with factor 1.
type FileUoM struct {
	Name           string  `json:"name"`
	RelativeTo     string  `json:"relative_to,omitempty"`
	RelativeFactor float64 `json:"relative_factor"`
}

// FileCategory is a product category by full path, for example "蔬菜 / 瓜果茄果 / 番茄类".
type FileCategory struct {
	Path string `json:"path"`
}

// FileAttribute is an attribute and the values the file uses.
type FileAttribute struct {
	Name   string   `json:"name"`
	Values []string `json:"values"`
}

// FileAttrValue is one value of an attribute line with its extra price (a template attribute value
// property in Odoo, not a variant property).
type FileAttrValue struct {
	Name       string  `json:"name"`
	PriceExtra float64 `json:"price_extra,omitzero"`
}

// FileAttrLine is one attribute of a template and its values.
type FileAttrLine struct {
	Attribute string          `json:"attribute"`
	Values    []FileAttrValue `json:"values"`
}

// FileTemplate is a product.template with its variants.
type FileTemplate struct {
	SourceID        int            `json:"source_id"`
	Name            string         `json:"name"`
	Category        string         `json:"category"`
	ListPrice       float64        `json:"list_price"`
	SaleOK          bool           `json:"sale_ok"`
	Type            string         `json:"type"`
	IsStorable      bool           `json:"is_storable"`
	UoM             string         `json:"uom"`
	DescriptionSale string         `json:"description_sale,omitempty"`
	AttributeLines  []FileAttrLine `json:"attribute_lines,omitempty"`
	Variants        []FileVariant  `json:"variants"`
}

// FileVariant is a product.product variant. Values maps attribute name to value name and is empty
// for a template without attribute lines.
type FileVariant struct {
	SourceID    int               `json:"source_id"`
	DefaultCode string            `json:"default_code"`
	Values      map[string]string `json:"values"`
	Barcode     string            `json:"barcode,omitempty"`
	Weight      float64           `json:"weight,omitzero"`
	Volume      float64           `json:"volume,omitzero"`
	Active      bool              `json:"active"`
}

// FileBOMProduct names the variant a BOM is for, by default_code.
type FileBOMProduct struct {
	DefaultCode string `json:"default_code,omitempty"`
}

// FileBOMTemplate records the template of a BOM. For a template-level BOM, DefaultCodes holds the
// sorted codes of the template's active, coded variants, which identify the product across databases.
type FileBOMTemplate struct {
	SourceID     int      `json:"source_id"`
	DefaultCodes []string `json:"default_codes,omitempty"`
}

// FileBOMLine is one component, referenced by default_code ("" when the component has none).
type FileBOMLine struct {
	Component string  `json:"component"`
	Qty       float64 `json:"qty"`
	UoM       string  `json:"uom"`
}

// FileBOM is a bill of materials. Product is nil for a template-level BOM.
type FileBOM struct {
	SourceID   int             `json:"source_id"`
	Product    *FileBOMProduct `json:"product,omitempty"`
	Template   FileBOMTemplate `json:"template"`
	Type       string          `json:"type"` // "normal" or "phantom"
	ProductQty float64         `json:"product_qty"`
	UoM        string          `json:"uom"`
	Code       string          `json:"code,omitempty"`
	Active     bool            `json:"active"`
	Lines      []FileBOMLine   `json:"lines"`
}

// HasSection reports whether the file declares the section ("products" or "boms").
func (f *CatalogFile) HasSection(name string) bool {
	return f.Section == name || f.Section == "all"
}

func readCatalogFile(path string) (*CatalogFile, error) {
	var (
		data []byte
		f    CatalogFile
		err  error
	)

	if data, err = os.ReadFile(path); err != nil {
		return nil, fmt.Errorf("read catalog file: %w", err)
	}
	if err = jsonv2.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse catalog file %q: %w", path, err)
	}
	return &f, nil
}

func writeCatalogFile(path string, f *CatalogFile) error {
	var (
		data []byte
		err  error
	)

	if data, err = jsonv2.Marshal(f, jsontext.WithIndent("  ")); err != nil {
		return fmt.Errorf("encode catalog file: %w", err)
	}
	if err = os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write catalog file: %w", err)
	}
	return nil
}

// variantKey is a canonical string of an attribute-to-value map, empty for no attributes.
func variantKey(values map[string]string) string {
	var (
		names = make([]string, 0, len(values))
		parts []string
		name  string
	)

	for name = range values {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name = range names {
		parts = append(parts, name+"="+values[name])
	}
	return strings.Join(parts, "|")
}

// Validate checks the file on its own, without Odoo, and returns every problem it finds.
func (f *CatalogFile) Validate() []string {
	var (
		problems []string
		codes    = make(map[string]int)
	)

	if f.Format != formatVersion {
		return []string{fmt.Sprintf("unsupported format %d: this tool reads format %d", f.Format, formatVersion)}
	}
	if f.Section != "products" && f.Section != "boms" && f.Section != "all" {
		problems = append(problems, fmt.Sprintf("section must be products, boms or all, got %q", f.Section))
	}
	problems = append(problems, f.validateUnits()...)
	for _, t := range f.Templates {
		label := fmt.Sprintf("template %d %q", t.SourceID, t.Name)
		if strings.TrimSpace(t.Name) == "" {
			problems = append(problems, label+": name is empty")
		}
		if len(t.Variants) == 0 {
			problems = append(problems, label+": no variants")
			continue
		}
		lines := make(map[string]map[string]bool)
		for _, l := range t.AttributeLines {
			if lines[l.Attribute] != nil {
				problems = append(problems, fmt.Sprintf("%s: attribute %q appears twice", label, l.Attribute))
				continue
			}
			set := make(map[string]bool)
			for _, v := range l.Values {
				set[v.Name] = true
			}
			if len(set) == 0 || len(set) != len(l.Values) {
				problems = append(problems, fmt.Sprintf("%s: attribute %q needs unique values", label, l.Attribute))
			}
			lines[l.Attribute] = set
		}
		seen := make(map[string]bool)
		for _, v := range t.Variants {
			if len(v.Values) != len(lines) {
				problems = append(problems, fmt.Sprintf("%s: variant %d must have one value per attribute line", label, v.SourceID))
				continue
			}
			ok := true
			for attr, val := range v.Values {
				if !lines[attr][val] {
					ok = false
					problems = append(problems, fmt.Sprintf(
						"%s: variant %d has value %q for attribute %q that the template does not list",
						label, v.SourceID, val, attr,
					))
				}
			}
			if ok {
				key := variantKey(v.Values)
				if seen[key] {
					problems = append(problems, fmt.Sprintf("%s: variant %d repeats the value combination %q", label, v.SourceID, key))
				}
				seen[key] = true
			}
			if v.DefaultCode != "" {
				if first, dup := codes[v.DefaultCode]; dup {
					problems = append(problems, fmt.Sprintf(
						"default_code %q appears in templates %d and %d", v.DefaultCode, first, t.SourceID,
					))
				} else {
					codes[v.DefaultCode] = t.SourceID
				}
			}
		}
	}
	return append(problems, f.validateBOMs()...)
}

func (f *CatalogFile) validateUnits() []string {
	var (
		problems []string
		units    = make(map[string]FileUoM, len(f.Units))
	)
	for _, u := range f.Units {
		if strings.TrimSpace(u.Name) == "" {
			problems = append(problems, "unit name is empty")
			continue
		}
		if _, found := units[u.Name]; found {
			problems = append(problems, fmt.Sprintf("unit %q appears twice", u.Name))
		}
		units[u.Name] = u
		if math.IsNaN(u.RelativeFactor) || math.IsInf(u.RelativeFactor, 0) || u.RelativeFactor <= 0 ||
			(u.RelativeTo == "" && u.RelativeFactor != 1) {
			problems = append(problems, fmt.Sprintf("unit %q has invalid relative_factor", u.Name))
		}
	}
	for _, u := range f.Units {
		if u.RelativeTo != "" {
			if _, found := units[u.RelativeTo]; !found {
				problems = append(problems, fmt.Sprintf("unit %q references missing unit %q", u.Name, u.RelativeTo))
			}
		}
		seen := make(map[string]bool)
		for name := u.Name; name != ""; name = units[name].RelativeTo {
			if seen[name] {
				problems = append(problems, fmt.Sprintf("unit %q has a reference cycle", u.Name))
				break
			}
			seen[name] = true
			if _, found := units[name]; !found {
				break
			}
		}
	}
	return problems
}

func (f *CatalogFile) validateBOMs() []string {
	var problems []string

	for _, b := range f.BOMs {
		label := fmt.Sprintf("bom %d", b.SourceID)
		if b.Type != "normal" && b.Type != "phantom" {
			problems = append(problems, fmt.Sprintf("%s: type must be normal or phantom, got %q", label, b.Type))
		}
		if math.IsNaN(b.ProductQty) || b.ProductQty <= 0 {
			problems = append(problems, label+": product_qty must be positive")
		}
		if b.UoM == "" {
			problems = append(problems, label+": uom is empty")
		}
		if b.Product != nil && len(b.Template.DefaultCodes) > 0 {
			problems = append(problems, label+": has both product and template default_codes")
		}
		if len(b.Lines) == 0 {
			problems = append(problems, label+": has no lines")
		}
		for i, l := range b.Lines {
			if math.IsNaN(l.Qty) || l.Qty <= 0 {
				problems = append(problems, fmt.Sprintf("%s: line %d: qty must be positive", label, i+1))
			}
			if l.UoM == "" {
				problems = append(problems, fmt.Sprintf("%s: line %d: uom is empty", label, i+1))
			}
		}
	}
	return problems
}
