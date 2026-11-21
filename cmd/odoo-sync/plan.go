package main

import (
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/d2jvkpn/odoo-go"
)

const (
	emptyCodeReject = "reject"
	emptyCodeSkip   = "skip"
	saleOKAll       = "all"
	saleOKOnly      = "only"
)

// Diff field names; the applier builds its patches from them.
const (
	fieldName        = "name"
	fieldListPrice   = "list_price"
	fieldCategory    = "category"
	fieldSaleOK      = "sale_ok"
	fieldIsStorable  = "is_storable"
	fieldType        = "type"
	fieldUoM         = "uom"
	fieldDescription = "description_sale"
	fieldBarcode     = "barcode"
	fieldWeight      = "weight"
	fieldVolume      = "volume"
)

// importOptions are the import flags the planner honors.
type importOptions struct {
	EmptyCode    string // emptyCodeReject or emptyCodeSkip
	SaleOK       string // saleOKAll or saleOKOnly
	NoCreateDeps bool   // a missing category, attribute or value is an error
	NoProducts   bool   // plan no templates (a BOM-only import)
	BOMs         bool   // plan the file's BOMs
}

// Diff is one field that differs between the file and the target.
type Diff struct{ Field, Old, New string }

// PriceExtraPlan is a template attribute value whose extra price must change.
type PriceExtraPlan struct {
	ValueID          int
	Attribute, Value string
	Old, New         float64
}

// VariantPlan is the plan for one variant of an existing template.
type VariantPlan struct {
	File      FileVariant
	TargetID  int // zero when the template is created
	Diffs     []Diff
	SetActive *bool
}

// TemplatePlan is the plan for one template. TargetID zero means create it.
type TemplatePlan struct {
	Template    FileTemplate
	TargetID    int
	Diffs       []Diff
	PriceExtras []PriceExtraPlan
	Variants    []VariantPlan
	SetActive   *bool
}

// Creates reports whether the template does not exist in the target.
func (t TemplatePlan) Creates() bool { return t.TargetID == 0 }

// Changes reports whether an existing template needs any write.
func (t TemplatePlan) Changes() bool {
	if len(t.Diffs) > 0 || len(t.PriceExtras) > 0 || t.SetActive != nil {
		return true
	}
	for _, v := range t.Variants {
		if len(v.Diffs) > 0 || v.SetActive != nil {
			return true
		}
	}
	return false
}

// Plan is everything an import would do, computed without writing.
type Plan struct {
	NewCategories []string            // full paths, parents before children
	NewAttributes []string            // attribute names
	NewValues     map[string][]string // attribute name -> value names to create
	Templates     []TemplatePlan
	Skipped       []string
	BOMs          *BOMPlan          // nil unless BOMs are imported
	SkippedCodes  map[string]string // default_code -> why its template was skipped, for BOMs that name it
}

func sameNumber(a, b float64) bool { return math.Abs(a-b) < 0.005 }

func formatNumber(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

// uncodedVariants returns the source ids of the template's variants without default_code.
func uncodedVariants(t FileTemplate) []int {
	var ids []int

	for _, v := range t.Variants {
		if v.DefaultCode == "" {
			ids = append(ids, v.SourceID)
		}
	}
	return ids
}

// skipCodes remembers the codes of a skipped template, so a BOM that names one is skipped with the
// reason instead of being reported as a missing product.
func skipCodes(plan *Plan, t FileTemplate, reason string) {
	for _, v := range t.Variants {
		if v.DefaultCode != "" {
			plan.SkippedCodes[v.DefaultCode] = reason
		}
	}
}

// buildPlan compares the file with the target snapshot. It returns the plan and every problem that
// stops the import; with problems the plan must not be applied.
func buildPlan(f *CatalogFile, snap *Snapshot, opt importOptions) (*Plan, []string) {
	var (
		plan     = &Plan{NewValues: make(map[string][]string), SkippedCodes: make(map[string]string)}
		problems []string
		byCode   = make(map[string][]odoo.CatalogVariant)
		wantCats = make(map[string]bool)
		newAttrs = make(map[string]bool)
		newVals  = make(map[string]map[string]bool)
	)

	for _, v := range snap.Variants {
		if v.DefaultCode != "" {
			byCode[string(v.DefaultCode)] = append(byCode[string(v.DefaultCode)], v)
		}
	}
	templates := f.Templates
	if opt.NoProducts {
		templates = nil
	}
	for _, t := range templates {
		label := fmt.Sprintf("template %d %q", t.SourceID, t.Name)
		if opt.SaleOK == saleOKOnly && !t.SaleOK {
			plan.Skipped = append(plan.Skipped, label+": sale_ok is false")
			skipCodes(plan, t, label+" is skipped (sale_ok is false)")
			continue
		}
		if missing := uncodedVariants(t); len(missing) > 0 {
			if opt.EmptyCode == emptyCodeSkip {
				plan.Skipped = append(plan.Skipped, fmt.Sprintf("%s: %d variant(s) without default_code", label, len(missing)))
				skipCodes(plan, t, label+" is skipped (variants without default_code)")
				continue
			}
			for _, id := range missing {
				problems = append(problems, fmt.Sprintf("%s: variant %d has no default_code", label, id))
			}
			continue
		}
		tp, tproblems := planTemplate(t, label, byCode, snap)
		if len(tproblems) > 0 {
			problems = append(problems, tproblems...)
			continue
		}
		if tp.Creates() {
			if aproblems := attributeProblems(t, label, snap); len(aproblems) > 0 {
				problems = append(problems, aproblems...)
				continue
			}
		}
		plan.Templates = append(plan.Templates, tp)
		if t.Category != "" {
			wantCats[t.Category] = true
		}
		for _, line := range t.AttributeLines {
			attr, known := snap.Attributes[line.Attribute]
			if !known {
				newAttrs[line.Attribute] = true
			}
			for _, val := range line.Values {
				if known && attr.Values[val.Name] != 0 {
					continue
				}
				if newVals[line.Attribute] == nil {
					newVals[line.Attribute] = make(map[string]bool)
				}
				newVals[line.Attribute][val.Name] = true
			}
		}
	}
	plan.NewCategories = missingCategories(wantCats, snap.Categories)
	plan.NewAttributes = sortedKeys(newAttrs)
	for attr, names := range newVals {
		plan.NewValues[attr] = sortedKeys(names)
	}
	if opt.NoCreateDeps {
		for _, c := range plan.NewCategories {
			problems = append(problems, fmt.Sprintf("category %q does not exist in the target", c))
		}
		for _, a := range plan.NewAttributes {
			problems = append(problems, fmt.Sprintf("attribute %q does not exist in the target", a))
		}
		for _, a := range sortedKeys(plan.NewValues) {
			if newAttrs[a] {
				continue
			}
			for _, v := range plan.NewValues[a] {
				problems = append(problems, fmt.Sprintf("value %q of attribute %q does not exist in the target", v, a))
			}
		}
	}
	if opt.BOMs {
		var bproblems []string

		plan.BOMs, bproblems = buildBOMPlan(f, snap, plan, opt)
		problems = append(problems, bproblems...)
	}
	return plan, problems
}

// attributeProblems lists why a template that is about to be created cannot use the target's
// existing attributes: Odoo only generates every combination for attributes whose create_variant is
// "always", and an archived attribute or value cannot be attached to a new template.
func attributeProblems(t FileTemplate, label string, snap *Snapshot) []string {
	var problems []string

	for _, line := range t.AttributeLines {
		attr, known := snap.Attributes[line.Attribute]
		if !known {
			continue
		}
		if attr.Archived {
			problems = append(problems, fmt.Sprintf("%s: attribute %q is archived in the target; restore it first", label, line.Attribute))
		}
		if attr.CreateVariant != "always" {
			problems = append(problems, fmt.Sprintf(
				"%s: attribute %q has create_variant %q in the target; Odoo would not generate every combination, set it to \"always\" first",
				label, line.Attribute, attr.CreateVariant,
			))
		}
		for _, val := range line.Values {
			if attr.ArchivedValues[val.Name] {
				problems = append(problems, fmt.Sprintf(
					"%s: value %q of attribute %q is archived in the target; restore it first", label, val.Name, line.Attribute,
				))
			}
		}
	}
	return problems
}

// missingCategories returns the category paths, and their missing ancestors, that the target lacks,
// shallowest first so that a parent is always created before its children.
func missingCategories(want map[string]bool, have map[string]int) []string {
	var need = make(map[string]bool)

	for path := range want {
		parts := strings.Split(path, " / ")
		for i := 1; i <= len(parts); i++ {
			prefix := strings.Join(parts[:i], " / ")
			if have[prefix] == 0 {
				need[prefix] = true
			}
		}
	}
	out := sortedKeys(need)
	sort.SliceStable(out, func(i, j int) bool {
		return strings.Count(out[i], " / ") < strings.Count(out[j], " / ")
	})
	return out
}

// planTemplate matches one template's variants by default_code and decides create or update.
func planTemplate(
	t FileTemplate,
	label string,
	byCode map[string][]odoo.CatalogVariant,
	snap *Snapshot,
) (TemplatePlan, []string) {
	var (
		tp                       = TemplatePlan{Template: t}
		problems                 []string
		matches                  = make([]odoo.CatalogVariant, len(t.Variants))
		existing                 int
		fileActive, targetActive bool
	)

	if t.UoM != "" && snap.UoMs[t.UoM] == 0 {
		problems = append(problems, fmt.Sprintf("%s: unit of measure %q does not exist in the target", label, t.UoM))
	}
	for i, v := range t.Variants {
		found := byCode[v.DefaultCode]
		if len(found) > 1 {
			problems = append(problems, fmt.Sprintf(
				"%s: default_code %q matches %d variants in the target", label, v.DefaultCode, len(found),
			))
			continue
		}
		if len(found) == 1 {
			matches[i] = found[0]
			existing++
		}
	}
	if len(problems) > 0 {
		return tp, problems
	}
	if existing == 0 {
		for _, v := range t.Variants {
			tp.Variants = append(tp.Variants, VariantPlan{File: v})
		}
		return tp, nil
	}
	if existing != len(t.Variants) {
		return tp, []string{fmt.Sprintf(
			"%s: %d of %d variants exist in the target; refusing to merge", label, existing, len(t.Variants),
		)}
	}
	tp.TargetID = matches[0].Template.ID
	for _, m := range matches {
		if m.Template.ID != tp.TargetID {
			return tp, []string{label + ": its variants belong to different templates in the target"}
		}
	}
	tp.Diffs = templateDiffs(t, matches[0])
	for i, v := range t.Variants {
		tv := matches[i]
		vp := VariantPlan{File: v, TargetID: tv.ID}
		if v.Barcode != "" && v.Barcode != string(tv.Barcode) {
			vp.Diffs = append(vp.Diffs, Diff{fieldBarcode, string(tv.Barcode), v.Barcode})
		}
		if !sameNumber(v.Weight, tv.Weight) {
			vp.Diffs = append(vp.Diffs, Diff{fieldWeight, formatNumber(tv.Weight), formatNumber(v.Weight)})
		}
		if !sameNumber(v.Volume, tv.Volume) {
			vp.Diffs = append(vp.Diffs, Diff{fieldVolume, formatNumber(tv.Volume), formatNumber(v.Volume)})
		}
		fileActive = fileActive || v.Active
		targetActive = targetActive || tv.Active
		tp.Variants = append(tp.Variants, vp)
	}
	if fileActive != targetActive {
		tp.SetActive = &fileActive
		if fileActive {
			// Odoo restores every variant together with the template, so the ones the file keeps
			// archived must be archived again afterwards.
			for i := range tp.Variants {
				if !tp.Variants[i].File.Active {
					off := false
					tp.Variants[i].SetActive = &off
				}
			}
		}
	} else if fileActive {
		for i := range tp.Variants {
			if tp.Variants[i].File.Active != matches[i].Active {
				want := tp.Variants[i].File.Active
				tp.Variants[i].SetActive = &want
			}
		}
	}
	for _, line := range t.AttributeLines {
		for _, val := range line.Values {
			id, cur, ok := findValue(snap, matches, line.Attribute, val.Name)
			if ok && !sameNumber(cur, val.PriceExtra) {
				tp.PriceExtras = append(tp.PriceExtras, PriceExtraPlan{
					ValueID: id, Attribute: line.Attribute, Value: val.Name, Old: cur, New: val.PriceExtra,
				})
			}
		}
	}
	return tp, nil
}

// findValue looks for the template attribute value of attr and value among the target variants.
func findValue(snap *Snapshot, matches []odoo.CatalogVariant, attr, value string) (int, float64, bool) {
	for _, m := range matches {
		for _, id := range m.ValueIDs {
			if v, ok := snap.Values[id]; ok && v.Attribute.Name == attr && v.Value == value {
				return id, v.PriceExtra, true
			}
		}
	}
	return 0, 0, false
}

// templateDiffs lists the template-level fields that differ from the target variant tv. Fields the
// file leaves empty (type, unit, category) are not compared.
func templateDiffs(t FileTemplate, tv odoo.CatalogVariant) []Diff {
	var d []Diff

	if t.Name != tv.Name {
		d = append(d, Diff{fieldName, tv.Name, t.Name})
	}
	if !sameNumber(t.ListPrice, tv.ListPrice) {
		d = append(d, Diff{fieldListPrice, formatNumber(tv.ListPrice), formatNumber(t.ListPrice)})
	}
	if t.Category != "" && t.Category != tv.Category.Name {
		d = append(d, Diff{fieldCategory, tv.Category.Name, t.Category})
	}
	if t.SaleOK != tv.SaleOK {
		d = append(d, Diff{fieldSaleOK, strconv.FormatBool(tv.SaleOK), strconv.FormatBool(t.SaleOK)})
	}
	if t.IsStorable != tv.IsStorable {
		d = append(d, Diff{fieldIsStorable, strconv.FormatBool(tv.IsStorable), strconv.FormatBool(t.IsStorable)})
	}
	if t.Type != "" && t.Type != tv.Type {
		d = append(d, Diff{fieldType, tv.Type, t.Type})
	}
	if t.UoM != "" && t.UoM != tv.UoM.Name {
		d = append(d, Diff{fieldUoM, tv.UoM.Name, t.UoM})
	}
	if t.DescriptionSale != string(tv.Description) {
		d = append(d, Diff{fieldDescription, string(tv.Description), t.DescriptionSale})
	}
	return d
}

// printPlan writes a human-readable account of the plan, used by -dry-run.
func printPlan(w io.Writer, p *Plan) {
	var creates, updates, same int

	for _, c := range p.NewCategories {
		fmt.Fprintf(w, "create category %q\n", c)
	}
	for _, a := range p.NewAttributes {
		fmt.Fprintf(w, "create attribute %q\n", a)
	}
	for _, a := range sortedKeys(p.NewValues) {
		for _, v := range p.NewValues[a] {
			fmt.Fprintf(w, "create value %q of attribute %q\n", v, a)
		}
	}
	for _, t := range p.Templates {
		label := fmt.Sprintf("template %d %q", t.Template.SourceID, t.Template.Name)
		switch {
		case t.Creates():
			creates++
			fmt.Fprintf(w, "create %s with %d variant(s)\n", label, len(t.Variants))
		case t.Changes():
			updates++
			fmt.Fprintf(w, "update %s (target %d)\n", label, t.TargetID)
			for _, d := range t.Diffs {
				fmt.Fprintf(w, "  %s: %q -> %q\n", d.Field, d.Old, d.New)
			}
			for _, e := range t.PriceExtras {
				fmt.Fprintf(w, "  price_extra %s=%s: %s -> %s\n", e.Attribute, e.Value, formatNumber(e.Old), formatNumber(e.New))
			}
			if t.SetActive != nil {
				fmt.Fprintf(w, "  template active: %t\n", *t.SetActive)
			}
			for _, v := range t.Variants {
				for _, d := range v.Diffs {
					fmt.Fprintf(w, "  variant %d (%s) %s: %q -> %q\n", v.TargetID, v.File.DefaultCode, d.Field, d.Old, d.New)
				}
				if v.SetActive != nil {
					fmt.Fprintf(w, "  variant %d (%s) active: %t\n", v.TargetID, v.File.DefaultCode, *v.SetActive)
				}
			}
		default:
			same++
		}
	}
	for _, s := range p.Skipped {
		fmt.Fprintf(w, "skip %s\n", s)
	}
	fmt.Fprintf(w, "summary: %d to create, %d to update, %d unchanged, %d skipped\n", creates, updates, same, len(p.Skipped))
	if p.BOMs != nil {
		printBOMPlan(w, p.BOMs)
	}
}

func printBOMPlan(w io.Writer, b *BOMPlan) {
	var creates, archives, codes int

	for _, a := range b.Actions {
		switch a.Kind {
		case bomCreate:
			creates++
			fmt.Fprintf(w, "create bom for %s (%s, %s %s, %d lines)\n", a.Product, a.File.Type, formatNumber(a.File.ProductQty), a.File.UoM, len(a.File.Lines))
		case bomArchive:
			archives++
			fmt.Fprintf(w, "archive bom %d of %s\n", a.TargetID, a.Product)
		case bomSetCode:
			codes++
			fmt.Fprintf(w, "bom %d of %s code: %q -> %q\n", a.TargetID, a.Product, a.Old, a.New)
		}
	}
	for _, m := range b.Warnings {
		fmt.Fprintln(w, "warning:", m)
	}
	for _, s := range b.Skipped {
		fmt.Fprintf(w, "skip %s\n", s)
	}
	fmt.Fprintf(w, "bom summary: %d to create, %d to archive, %d code updates, %d unchanged, %d skipped\n",
		creates, archives, codes, b.Unchanged, len(b.Skipped))
}
