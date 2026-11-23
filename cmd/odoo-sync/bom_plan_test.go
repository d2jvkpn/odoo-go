package main

import (
	"testing"

	"github.com/d2jvkpn/odoo-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var bomOpt = importOptions{EmptyCode: emptyCodeReject, SaleOK: saleOKAll, NoProducts: true, BOMs: true}

var kg = odoo.Many2One{ID: 1, Name: "kg"}

func tLine(compID int, qty float64) odoo.BOMLine {
	return odoo.BOMLine{Product: odoo.Many2One{ID: compID}, Quantity: qty, UoM: kg}
}

func tBOM(id, tmpl, variant int, code string, lines ...odoo.BOMLine) odoo.BOM {
	return odoo.BOM{
		ID: id, Code: odoo.OptionalString(code), Type: "normal", Quantity: 1, UoM: kg, Active: true,
		Template: odoo.Many2One{ID: tmpl}, Variant: odoo.Many2One{ID: variant}, Lines: lines,
	}
}

func fLine(code string, qty float64) FileBOMLine {
	return FileBOMLine{Component: code, Qty: qty, UoM: "kg"}
}

func fBOM(id int, productCode, code string, lines ...FileBOMLine) FileBOM {
	return FileBOM{
		SourceID: id, Product: &FileBOMProduct{DefaultCode: productCode}, Type: "normal", ProductQty: 1, UoM: "kg",
		Code: code, Active: true, Lines: lines,
	}
}

func bomFile(boms ...FileBOM) *CatalogFile {
	return &CatalogFile{Format: formatVersion, Section: "boms", BOMs: boms}
}

// bomSnapshot has products P-1 (variant 1, template 5001) and P-2, components C-1 (10) and C-2 (11).
func bomSnapshot() *Snapshot {
	s := emptySnapshot()
	s.Variants = []odoo.CatalogVariant{
		targetVariant(1, 5001, "P-1"), targetVariant(2, 5002, "P-2"),
		targetVariant(10, 6001, "C-1"), targetVariant(11, 6002, "C-2"),
	}
	return s
}

func actionKinds(p *BOMPlan) []string {
	var kinds []string
	for _, a := range p.Actions {
		kinds = append(kinds, a.Kind)
	}
	return kinds
}

func TestBOMFingerprintIgnoresLineOrderCodeAndFloatNoise(t *testing.T) {
	a := bomFingerprint("normal", 1, "kg", []FileBOMLine{fLine("C-1", 0.1+0.2), fLine("C-2", 1)})
	b := bomFingerprint("normal", 1, "kg", []FileBOMLine{fLine("C-2", 1), fLine("C-1", 0.3)})
	assert.Equal(t, a, b)
	assert.NotEqual(t, a, bomFingerprint("phantom", 1, "kg", []FileBOMLine{fLine("C-1", 0.3), fLine("C-2", 1)}))
	assert.NotEqual(t, a, bomFingerprint("normal", 2, "kg", []FileBOMLine{fLine("C-1", 0.3), fLine("C-2", 1)}))
	assert.NotEqual(t, a, bomFingerprint("normal", 1, "g", []FileBOMLine{fLine("C-1", 0.3), fLine("C-2", 1)}))
	assert.NotEqual(t, a, bomFingerprint("normal", 1, "kg", []FileBOMLine{fLine("C-1", 0.3), fLine("C-3", 1)}))
}

func TestBOMPlanKeepsMatchingBOMAndSyncsOnlyCode(t *testing.T) {
	snap := bomSnapshot()
	snap.BOMs = []odoo.BOM{tBOM(70, 5001, 1, "old", tLine(10, 2))}

	plan, problems := buildPlan(bomFile(fBOM(1, "P-1", "old", fLine("C-1", 2))), snap, bomOpt)
	require.Empty(t, problems)
	assert.Empty(t, plan.BOMs.Actions)
	assert.Equal(t, 1, plan.BOMs.Unchanged)

	plan, problems = buildPlan(bomFile(fBOM(1, "P-1", "new", fLine("C-1", 2))), snap, bomOpt)
	require.Empty(t, problems)
	require.Len(t, plan.BOMs.Actions, 1)
	a := plan.BOMs.Actions[0]
	assert.Equal(t, bomSetCode, a.Kind)
	assert.Equal(t, 70, a.TargetID)
	assert.Equal(t, "old", a.Old)
	assert.Equal(t, "new", a.New)
	assert.Zero(t, plan.BOMs.Unchanged)
}

func TestBOMPlanCreatesMissingAndArchivesOrphansAndReplacesEdited(t *testing.T) {
	snap := bomSnapshot()
	snap.BOMs = []odoo.BOM{tBOM(70, 5001, 1, "", tLine(10, 2)), tBOM(71, 5002, 2, "", tLine(10, 1))}

	// P-1: components edited (C-1 -> C-2): old archived, new created. P-2: absent from the file: untouched.
	plan, problems := buildPlan(bomFile(fBOM(1, "P-1", "", fLine("C-2", 2))), snap, bomOpt)
	require.Empty(t, problems)
	assert.ElementsMatch(t, []string{bomCreate, bomArchive}, actionKinds(plan.BOMs))
	for _, a := range plan.BOMs.Actions {
		if a.Kind == bomArchive {
			assert.Equal(t, 70, a.TargetID, "the BOM of the other product is never touched")
		}
	}

	snap.BOMs = nil
	plan, problems = buildPlan(bomFile(fBOM(1, "P-1", "", fLine("C-1", 2))), snap, bomOpt)
	require.Empty(t, problems)
	assert.Equal(t, []string{bomCreate}, actionKinds(plan.BOMs))
}

func TestBOMPlanIgnoresArchivedTargetBOMsAndHandlesArchivedFileBOMs(t *testing.T) {
	snap := bomSnapshot()
	archived := tBOM(70, 5001, 1, "", tLine(10, 2))
	archived.Active = false
	snap.BOMs = []odoo.BOM{archived}

	plan, problems := buildPlan(bomFile(fBOM(1, "P-1", "", fLine("C-1", 2))), snap, bomOpt)
	require.Empty(t, problems)
	assert.Equal(t, []string{bomCreate}, actionKinds(plan.BOMs), "an archived target BOM is not matched")

	snap.BOMs = []odoo.BOM{tBOM(71, 5001, 1, "", tLine(10, 2))}
	gone := fBOM(1, "P-1", "", fLine("C-1", 2))
	gone.Active = false
	plan, problems = buildPlan(bomFile(gone), snap, bomOpt)
	require.Empty(t, problems)
	require.Len(t, plan.BOMs.Actions, 1)
	assert.Equal(t, bomArchive, plan.BOMs.Actions[0].Kind)
	assert.Equal(t, 71, plan.BOMs.Actions[0].TargetID)

	snap.BOMs = nil
	plan, problems = buildPlan(bomFile(gone), snap, bomOpt)
	require.Empty(t, problems)
	assert.Empty(t, plan.BOMs.Actions, "an archived file BOM is never created")
}

func TestBOMPlanWarnsAndKeepsOneOfTwoIdenticalActiveBOMs(t *testing.T) {
	a, b := fBOM(1, "P-1", "x", fLine("C-1", 2)), fBOM(2, "P-1", "y", fLine("C-1", 2))
	plan, problems := buildPlan(bomFile(a, b), bomSnapshot(), bomOpt)
	require.Empty(t, problems)
	assert.Equal(t, []string{bomCreate}, actionKinds(plan.BOMs))
	assert.Equal(t, "x", plan.BOMs.Actions[0].File.Code)
	require.Len(t, plan.BOMs.Warnings, 1)
	assert.Contains(t, plan.BOMs.Warnings[0], "P-1")
}

func TestBOMPlanScopesTargetBOMsByTemplateAndVariant(t *testing.T) {
	snap := bomSnapshot()
	// A template-level BOM and a BOM of another variant of the same template.
	snap.Variants = append(snap.Variants, targetVariant(3, 5001, "P-1b"))
	snap.BOMs = []odoo.BOM{tBOM(70, 5001, 0, "", tLine(10, 1)), tBOM(71, 5001, 3, "", tLine(10, 1))}

	plan, problems := buildPlan(bomFile(fBOM(1, "P-1", "", fLine("C-1", 5))), snap, bomOpt)
	require.Empty(t, problems)
	assert.Equal(t, []string{bomCreate}, actionKinds(plan.BOMs), "BOMs of the template or another variant are other products")

	tmpl := fBOM(2, "", "", fLine("C-1", 1))
	tmpl.Product = nil
	tmpl.Template = FileBOMTemplate{SourceID: 9, DefaultCodes: []string{"P-1", "P-1b"}}
	plan, problems = buildPlan(bomFile(tmpl), snap, bomOpt)
	require.Empty(t, problems)
	assert.Empty(t, plan.BOMs.Actions)
	assert.Equal(t, 1, plan.BOMs.Unchanged, "the template-level BOM matches the target's template-level BOM")
}

func TestBOMPlanListsEveryProblemAndAppliesNothing(t *testing.T) {
	bad := []FileBOM{
		fBOM(1, "NOPE", "", fLine("C-1", 1)),
		fBOM(2, "P-1", "", fLine("MISSING", 1), fLine("ALSO-MISSING", 1)),
		fBOM(3, "P-1", "", fLine("C-1", 1)),
		fBOM(4, "", "", fLine("C-1", 1)),
		fBOM(5, "P-2", "", fLine("", 1)),
	}
	bad[2].UoM = "dozen"
	bad[2].Lines[0].UoM = "dozen"

	_, problems := buildPlan(bomFile(bad...), bomSnapshot(), bomOpt)
	text := ""
	for _, p := range problems {
		text += p + "\n"
	}
	assert.Contains(t, text, `bom 1: product "NOPE" does not exist in the target`)
	assert.Contains(t, text, `bom 2: component "MISSING" does not exist in the target`)
	assert.Contains(t, text, `bom 2: component "ALSO-MISSING" does not exist in the target`)
	assert.Contains(t, text, `unit of measure "dozen"`)
	assert.Contains(t, text, "bom 4: product has no default_code")
	assert.Contains(t, text, "bom 5: component has no default_code")
}

func TestBOMPlanSkipsUncodedBOMsWhenAsked(t *testing.T) {
	opt := bomOpt
	opt.EmptyCode = emptyCodeSkip
	plan, problems := buildPlan(bomFile(fBOM(4, "", "", fLine("C-1", 1)), fBOM(1, "P-1", "", fLine("C-1", 1))), bomSnapshot(), opt)
	require.Empty(t, problems)
	assert.Equal(t, []string{bomCreate}, actionKinds(plan.BOMs))
	require.Len(t, plan.BOMs.Skipped, 1)
	assert.Contains(t, plan.BOMs.Skipped[0], "bom 4")
}

func TestBOMPlanResolvesProductsCreatedInTheSameRun(t *testing.T) {
	f := sampleFile()
	f.Section = "all"
	f.BOMs = []FileBOM{fBOM(1, "A-1", "", fLine("273001101", 1))}

	plan, problems := buildPlan(f, emptySnapshot(), importOptions{EmptyCode: emptyCodeReject, SaleOK: saleOKAll, BOMs: true})
	require.Empty(t, problems)
	assert.Equal(t, []string{bomCreate}, actionKinds(plan.BOMs))
}

func TestBOMPlanSectionsAreIndependent(t *testing.T) {
	plan, problems := buildPlan(bomFile(fBOM(1, "P-1", "", fLine("C-1", 1))), bomSnapshot(), bomOpt)
	require.Empty(t, problems)
	assert.Empty(t, plan.Templates, "NoProducts plans no templates")

	f := sampleFile()
	f.BOMs = []FileBOM{fBOM(1, "A-1", "", fLine("273001101", 1))}
	plan, problems = buildPlan(f, emptySnapshot(), strict)
	require.Empty(t, problems)
	assert.Nil(t, plan.BOMs, "BOMs false plans no BOMs")
	assert.NotEmpty(t, plan.Templates)
}

func TestBOMPlanRejectsATemplateBOMSpanningTemplates(t *testing.T) {
	tmpl := fBOM(1, "", "", fLine("C-1", 1))
	tmpl.Product = nil
	tmpl.Template = FileBOMTemplate{SourceID: 9, DefaultCodes: []string{"P-1", "P-2"}}
	_, problems := buildPlan(bomFile(tmpl), bomSnapshot(), bomOpt)
	require.Len(t, problems, 1)
	assert.Contains(t, problems[0], "more than one template")
}

func TestBOMPlanSkipWithholdsArchivesOfThatProduct(t *testing.T) {
	snap := bomSnapshot()
	snap.Variants = append(snap.Variants, targetVariant(12, 6003, "")) // an uncoded component
	snap.BOMs = []odoo.BOM{tBOM(70, 5001, 1, "", tLine(10, 1)), tBOM(71, 5001, 1, "", tLine(12, 1)), tBOM(72, 5001, 1, "", tLine(11, 9))}
	opt := bomOpt
	opt.EmptyCode = emptyCodeSkip

	plan, problems := buildPlan(bomFile(
		fBOM(1, "P-1", "", fLine("C-1", 1)),
		fBOM(2, "P-1", "", fLine("", 1)), // skipped: its component has no code
	), snap, opt)
	require.Empty(t, problems)
	assert.Empty(t, plan.BOMs.Actions, "the target copy of a skipped BOM and the unlisted BOM 72 are not archived")
	assert.Equal(t, 1, plan.BOMs.Unchanged)
	require.Len(t, plan.BOMs.Skipped, 1)
	require.Len(t, plan.BOMs.Warnings, 1)
	assert.Contains(t, plan.BOMs.Warnings[0], "P-1")
}

func TestBOMPlanRejectsDuplicateTargetCodes(t *testing.T) {
	snap := bomSnapshot()
	snap.Variants = append(snap.Variants, targetVariant(50, 5009, "P-1"), targetVariant(51, 6009, "C-1"))
	_, problems := buildPlan(bomFile(fBOM(1, "P-1", "", fLine("C-1", 1))), snap, bomOpt)
	text := ""
	for _, p := range problems {
		text += p + "\n"
	}
	assert.Contains(t, text, `default_code "P-1" matches several variants in the target`)
	assert.Contains(t, text, `default_code "C-1" matches several variants in the target`)
}

func TestBOMPlanSkipsBOMsOfProductsSkippedOnTheProductsSide(t *testing.T) {
	f := sampleFile()
	f.Templates[1].SaleOK = false // 273001101 is skipped by -sale-ok only
	f.Section = "all"
	f.BOMs = []FileBOM{fBOM(1, "A-1", "", fLine("273001101", 1)), fBOM(2, "A-2", "", fLine("A-1", 1))}

	plan, problems := buildPlan(f, emptySnapshot(), importOptions{EmptyCode: emptyCodeReject, SaleOK: saleOKOnly, BOMs: true})
	require.Empty(t, problems)
	assert.Equal(t, []string{bomCreate}, actionKinds(plan.BOMs))
	require.Len(t, plan.BOMs.Skipped, 1)
	assert.Contains(t, plan.BOMs.Skipped[0], "bom 1")
	assert.Contains(t, plan.BOMs.Skipped[0], "273001101")
}

func TestBOMPlanDoesNotValidateArchivedFileBOMsAgainstTheTarget(t *testing.T) {
	gone := fBOM(1, "P-1", "", fLine("MISSING", 1), fLine("", 1))
	gone.Active, gone.UoM = false, "dozen"
	unknown := fBOM(2, "NOPE", "", fLine("C-1", 1))
	unknown.Active = false
	noCodes := fBOM(3, "", "", fLine("C-1", 1))
	noCodes.Active, noCodes.Product = false, nil

	plan, problems := buildPlan(bomFile(gone, unknown, noCodes), bomSnapshot(), bomOpt)
	require.Empty(t, problems)
	assert.Empty(t, plan.BOMs.Actions)
}

func TestBOMPlanArchivesDuplicateTargetBOMsOfIdenticalContent(t *testing.T) {
	snap := bomSnapshot()
	snap.BOMs = []odoo.BOM{tBOM(70, 5001, 1, "", tLine(10, 2)), tBOM(71, 5001, 1, "", tLine(10, 2))}
	plan, problems := buildPlan(bomFile(fBOM(1, "P-1", "", fLine("C-1", 2))), snap, bomOpt)
	require.Empty(t, problems)
	require.Len(t, plan.BOMs.Actions, 1)
	assert.Equal(t, bomArchive, plan.BOMs.Actions[0].Kind)
	assert.Equal(t, 71, plan.BOMs.Actions[0].TargetID, "the first by id is kept")
	assert.Equal(t, 1, plan.BOMs.Unchanged)
}
