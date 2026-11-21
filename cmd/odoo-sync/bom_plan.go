package main

import (
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"

	"github.com/d2jvkpn/odoo-go"
)

// BOM action kinds.
const (
	bomCreate  = "create"
	bomArchive = "archive"
	bomSetCode = "code"
)

// BOMAction is one write the importer makes to a bill of materials.
type BOMAction struct {
	Kind     string
	Product  string   // human label: the default_code, or the template's codes
	Group    string   // the product group key; archives are withheld per group
	File     *FileBOM // create: the BOM to create; code: the BOM whose code is copied
	TargetID int      // archive and code: the target mrp.bom ID
	Old, New string   // code: the old and the new reference
}

// BOMPlan is everything a BOM import would do.
type BOMPlan struct {
	Actions   []BOMAction
	Unchanged int
	Skipped   []string // BOMs skipped under -empty-code skip
	Warnings  []string
}

// bomFingerprint identifies a BOM by content: type, quantity, unit and the sorted components. The
// reference, source id, line order and active state are not part of it.
func bomFingerprint(typ string, qty float64, uom string, lines []FileBOMLine) string {
	parts := make([]string, 0, len(lines))
	for _, l := range lines {
		parts = append(parts, l.Component+":"+roundedNumber(l.Qty)+":"+l.UoM)
	}
	sort.Strings(parts)
	return typ + "|" + roundedNumber(qty) + "|" + uom + "|" + strings.Join(parts, ";")
}

func roundedNumber(f float64) string { return formatNumber(math.Round(f*1e6) / 1e6) }

// bomScope is the target product a BOM belongs to; zero means the product does not exist yet.
type bomScope struct{ Template, Variant int }

type bomGroup struct {
	key       string
	label     string
	scope     bomScope
	boms      []*FileBOM
	noArchive bool // a BOM of this product was skipped, so the target BOMs cannot be judged
}

// bomProductCodes returns the codes that identify the BOM's product and the group key.
func bomProductCodes(b *FileBOM) (codes []string, key string) {
	if b.Product != nil {
		return []string{b.Product.DefaultCode}, "v:" + b.Product.DefaultCode
	}
	codes = slices.Sorted(slices.Values(b.Template.DefaultCodes))
	return codes, "t:" + strings.Join(codes, ",")
}

// buildBOMPlan compares the file's BOMs with the target for every product the file names a BOM for.
// plan supplies the products the same run is about to create.
func buildBOMPlan(f *CatalogFile, snap *Snapshot, plan *Plan, opt importOptions) (*BOMPlan, []string) {
	var (
		bp          = &BOMPlan{}
		problems    []string
		known       = make(map[string]bool)
		byCode      = make(map[string]odoo.CatalogVariant)
		idCode      = make(map[int]string)
		groups      = make(map[string]*bomGroup)
		order       []string
		skippedKeys = make(map[string]bool)
		codeCount   = make(map[string]int)
	)

	for _, v := range snap.Variants {
		idCode[v.ID] = string(v.DefaultCode)
		if v.DefaultCode != "" {
			codeCount[string(v.DefaultCode)]++
			known[string(v.DefaultCode)] = true
			if _, dup := byCode[string(v.DefaultCode)]; !dup {
				byCode[string(v.DefaultCode)] = v
			}
		}
	}
	for _, tp := range plan.Templates {
		for _, v := range tp.Template.Variants {
			if v.DefaultCode != "" {
				known[v.DefaultCode] = true
			}
		}
	}
	dupCodes := make(map[string]bool)
	for code, n := range codeCount {
		if n > 1 {
			dupCodes[code] = true
		}
	}
	reportedDup := make(map[string]bool)
	for i := range f.BOMs {
		b := &f.BOMs[i]
		label := fmt.Sprintf("bom %d", b.SourceID)
		codes, key := bomProductCodes(b)

		if !b.Active {
			// An archived BOM is never created: it only says its product has no active BOM to keep.
			if slices.Contains(codes, "") || len(codes) == 0 || !allKnown(codes, known) {
				continue
			}
			if scope, bad := resolveBOMScope(b, codes, byCode); bad == "" {
				addToGroup(groups, &order, key, codes, scope, b)
			}
			continue
		}
		var own []string
		if len(codes) == 0 || slices.Contains(codes, "") {
			own = append(own, label+": product has no default_code")
		}
		for _, l := range b.Lines {
			if l.Component == "" {
				own = append(own, label+": component has no default_code")
				break
			}
		}
		if len(own) > 0 {
			if opt.EmptyCode == emptyCodeSkip {
				bp.Skipped = append(bp.Skipped, label+": "+strings.TrimPrefix(own[0], label+": "))
				skippedKeys[key] = true
			} else {
				problems = append(problems, own...)
			}
			continue
		}
		if reason := skippedByProducts(b, codes, known, plan); reason != "" {
			bp.Skipped = append(bp.Skipped, label+": "+reason)
			skippedKeys[key] = true
			continue
		}
		own = nil
		for _, c := range append(slices.Clone(codes), componentCodes(b)...) {
			switch {
			case dupCodes[c]:
				if !reportedDup[c] {
					reportedDup[c] = true
					own = append(own, fmt.Sprintf("default_code %q matches several variants in the target", c))
				}
			case !known[c]:
				what := "component"
				if slices.Contains(codes, c) {
					what = "product"
				}
				own = append(own, fmt.Sprintf("%s: %s %q does not exist in the target", label, what, c))
			}
		}
		for _, u := range append([]string{b.UoM}, lineUnits(b)...) {
			if _, ok := snap.UoMs[u]; !ok {
				own = append(own, fmt.Sprintf("%s: unit of measure %q does not exist in the target", label, u))
			}
		}
		scope, scopeProblem := resolveBOMScope(b, codes, byCode)
		if scopeProblem != "" {
			own = append(own, label+": "+scopeProblem)
		}
		if len(own) > 0 {
			problems = append(problems, dedupe(own)...)
			continue
		}
		addToGroup(groups, &order, key, codes, scope, b)
	}
	for _, key := range order {
		if g := groups[key]; skippedKeys[key] {
			g.noArchive = true
		}
	}
	if len(problems) > 0 {
		return bp, problems
	}
	for _, key := range order {
		planBOMGroup(bp, groups[key], snap, idCode)
	}
	return bp, nil
}

func addToGroup(groups map[string]*bomGroup, order *[]string, key string, codes []string, scope bomScope, b *FileBOM) {
	g := groups[key]
	if g == nil {
		g = &bomGroup{key: key, label: strings.Join(codes, " "), scope: scope}
		groups[key] = g
		*order = append(*order, key)
	}
	g.boms = append(g.boms, b)
}

func allKnown(codes []string, known map[string]bool) bool {
	for _, c := range codes {
		if !known[c] {
			return false
		}
	}
	return true
}

func componentCodes(b *FileBOM) []string {
	codes := make([]string, 0, len(b.Lines))
	for _, l := range b.Lines {
		codes = append(codes, l.Component)
	}
	return codes
}

// skippedByProducts explains a BOM that names a product whose template the products side skipped
// (-sale-ok only, -empty-code skip); "" when none does.
func skippedByProducts(b *FileBOM, codes []string, known map[string]bool, plan *Plan) string {
	for _, c := range append(slices.Clone(codes), componentCodes(b)...) {
		if reason, ok := plan.SkippedCodes[c]; ok && !known[c] {
			return fmt.Sprintf("names %q, and %s", c, reason)
		}
	}
	return ""
}

func lineUnits(b *FileBOM) []string {
	units := make([]string, 0, len(b.Lines))
	for _, l := range b.Lines {
		units = append(units, l.UoM)
	}
	return units
}

func dedupe(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := in[:0:0]
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// resolveBOMScope finds the target product of a BOM. A product that only the same run creates has
// the zero scope, which no target BOM belongs to.
func resolveBOMScope(b *FileBOM, codes []string, byCode map[string]odoo.CatalogVariant) (bomScope, string) {
	var templates = make(map[int]bool)

	if b.Product != nil {
		if v, ok := byCode[b.Product.DefaultCode]; ok {
			return bomScope{Template: v.Template.ID, Variant: v.ID}, ""
		}
		return bomScope{}, ""
	}
	for _, c := range codes {
		if v, ok := byCode[c]; ok {
			templates[v.Template.ID] = true
		}
	}
	switch len(templates) {
	case 0:
		return bomScope{}, ""
	case 1:
		for id := range templates {
			return bomScope{Template: id}, ""
		}
	}
	return bomScope{}, fmt.Sprintf("the template's codes %v belong to more than one template in the target", codes)
}

// planBOMGroup reconciles the active BOMs of the file with the active BOMs of the target for one
// product: keep the same content, create what is missing, archive what the file no longer has.
func planBOMGroup(bp *BOMPlan, g *bomGroup, snap *Snapshot, idCode map[int]string) {
	type entry struct {
		fp string
		b  *FileBOM
	}
	var (
		wanted     []entry
		wantedSeen = make(map[string]bool)
		target     = make(map[string]odoo.BOM)
		extras     []odoo.BOM
	)

	for _, b := range g.boms {
		if !b.Active {
			continue
		}
		fp := bomFingerprint(b.Type, b.ProductQty, b.UoM, b.Lines)
		if wantedSeen[fp] {
			bp.Warnings = append(bp.Warnings, fmt.Sprintf("product %s: two active BOMs have the same components; keeping one", g.label))
			continue
		}
		wantedSeen[fp] = true
		wanted = append(wanted, entry{fp, b})
	}
	if g.scope != (bomScope{}) {
		for _, tb := range snap.BOMs {
			if !tb.Active || tb.Template.ID != g.scope.Template || tb.Variant.ID != g.scope.Variant {
				continue
			}
			fp := targetFingerprint(tb, idCode)
			if _, dup := target[fp]; dup {
				extras = append(extras, tb)
				continue
			}
			target[fp] = tb
		}
	}
	for _, w := range wanted {
		tb, ok := target[w.fp]
		switch {
		case !ok:
			bp.Actions = append(bp.Actions, BOMAction{Kind: bomCreate, Product: g.label, Group: g.key, File: w.b})
		case w.b.Code != string(tb.Code):
			bp.Actions = append(bp.Actions, BOMAction{
				Kind: bomSetCode, Product: g.label, Group: g.key, File: w.b, TargetID: tb.ID, Old: string(tb.Code), New: w.b.Code,
			})
		default:
			bp.Unchanged++
		}
	}
	if g.noArchive {
		bp.Warnings = append(bp.Warnings, fmt.Sprintf("product %s: a BOM was skipped, so no BOM of this product is archived", g.label))
		return
	}
	for _, tb := range snap.BOMs {
		fp := targetFingerprint(tb, idCode)
		if !tb.Active || tb.Template.ID != g.scope.Template || tb.Variant.ID != g.scope.Variant || g.scope == (bomScope{}) {
			continue
		}
		if !wantedSeen[fp] || slices.ContainsFunc(extras, func(e odoo.BOM) bool { return e.ID == tb.ID }) {
			bp.Actions = append(bp.Actions, BOMAction{Kind: bomArchive, Product: g.label, Group: g.key, TargetID: tb.ID})
		}
	}
}

// targetFingerprint is the fingerprint of a target BOM, with components named by default_code.
func targetFingerprint(b odoo.BOM, idCode map[int]string) string {
	lines := make([]FileBOMLine, 0, len(b.Lines))
	for _, l := range b.Lines {
		code, ok := idCode[l.Product.ID]
		if !ok || code == "" {
			code = fmt.Sprintf("?%d", l.Product.ID) // never equals a file component
		}
		lines = append(lines, FileBOMLine{Component: code, Qty: l.Quantity, UoM: l.UoM.Name})
	}
	return bomFingerprint(b.Type, b.Quantity, b.UoM.Name, lines)
}
