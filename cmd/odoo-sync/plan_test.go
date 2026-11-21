package main

import (
	"bytes"
	"testing"

	"github.com/d2jvkpn/odoo-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var strict = importOptions{EmptyCode: emptyCodeReject, SaleOK: saleOKAll}

func targetVariant(id, tmpl int, code string) odoo.CatalogVariant {
	return odoo.CatalogVariant{
		ID: id, Template: odoo.Many2One{ID: tmpl, Name: "t"}, Name: "西红柿块", DefaultCode: odoo.OptionalString(code),
		ListPrice: 1, Category: odoo.Many2One{ID: 7, Name: "蔬菜 / 番茄类"}, SaleOK: true, IsStorable: true,
		Type: "consu", UoM: odoo.Many2One{ID: 1, Name: "kg"}, Active: true,
	}
}

func emptySnapshot() *Snapshot {
	return &Snapshot{
		Categories: map[string]int{"蔬菜": 6, "蔬菜 / 番茄类": 7},
		Attributes: map[string]SnapAttribute{"规格": {ID: 3, CreateVariant: "always", Values: map[string]int{"1kg": 10, "1.5kg": 11}}},
		UoMs:       map[string]int{"kg": 1},
		Values:     map[int]odoo.ProductAttributeValue{},
	}
}

// matchingSnapshot is a target that already holds everything sampleFile describes.
func matchingSnapshot() *Snapshot {
	s := emptySnapshot()
	s.Categories["水果"] = 8
	a1, a2 := targetVariant(1, 5001, "A-1"), targetVariant(2, 5001, "A-2")
	a1.ValueIDs, a2.ValueIDs = []int{30}, []int{31}
	d := targetVariant(3, 5002, "273001101")
	d.Name, d.ListPrice, d.Category, d.IsStorable = "冬枣散装", 0, odoo.Many2One{ID: 8, Name: "水果"}, false
	s.Variants = []odoo.CatalogVariant{a1, a2, d}
	s.Values = map[int]odoo.ProductAttributeValue{
		30: {ID: 30, Attribute: odoo.Many2One{ID: 3, Name: "规格"}, Value: "1kg"},
		31: {ID: 31, Attribute: odoo.Many2One{ID: 3, Name: "规格"}, Value: "1.5kg", PriceExtra: 2.5},
	}
	return s
}

func TestPlanCreatesNewTemplatesAndTheirDependencies(t *testing.T) {
	snap := emptySnapshot()
	delete(snap.Attributes, "规格")
	plan, problems := buildPlan(sampleFile(), snap, strict)
	require.Empty(t, problems)
	assert.Equal(t, []string{"水果"}, plan.NewCategories)
	assert.Equal(t, []string{"规格"}, plan.NewAttributes)
	assert.Equal(t, map[string][]string{"规格": {"1.5kg", "1kg"}}, plan.NewValues)
	require.Len(t, plan.Templates, 2)
	assert.True(t, plan.Templates[0].Creates())
	assert.True(t, plan.Templates[1].Creates())
}

func TestPlanCreatesMissingCategoryLevelsParentFirst(t *testing.T) {
	f := sampleFile()
	f.Templates[1].Category = "果蔬 / 水果 / 枣类"
	plan, problems := buildPlan(f, emptySnapshot(), strict)
	require.Empty(t, problems)
	assert.Equal(t, []string{"果蔬", "果蔬 / 水果", "果蔬 / 水果 / 枣类"}, plan.NewCategories)
}

func TestPlanHasNoChangesWhenTargetMatchesFile(t *testing.T) {
	plan, problems := buildPlan(sampleFile(), matchingSnapshot(), strict)
	require.Empty(t, problems)
	require.Len(t, plan.Templates, 2)
	for _, tp := range plan.Templates {
		assert.False(t, tp.Creates())
		assert.False(t, tp.Changes(), "template %d", tp.Template.SourceID)
	}
	assert.Empty(t, plan.NewCategories)
	assert.Empty(t, plan.NewAttributes)
}

func TestPlanReportsFieldDifferences(t *testing.T) {
	snap := matchingSnapshot()
	for i := range snap.Variants[:2] {
		snap.Variants[i].ListPrice = 3
	}
	v := snap.Values[31]
	v.PriceExtra = 1
	snap.Values[31] = v
	f := sampleFile()
	f.Templates[0].Variants[0].Barcode = "B1"

	plan, problems := buildPlan(f, snap, strict)
	require.Empty(t, problems)
	tomato := plan.Templates[0]
	assert.Equal(t, []Diff{{Field: fieldListPrice, Old: "3", New: "1"}}, tomato.Diffs)
	assert.Equal(t, []PriceExtraPlan{{ValueID: 31, Attribute: "规格", Value: "1.5kg", Old: 1, New: 2.5}}, tomato.PriceExtras)
	assert.Equal(t, []Diff{{Field: fieldBarcode, Old: "", New: "B1"}}, tomato.Variants[0].Diffs)
	assert.Equal(t, 5001, tomato.TargetID)
	assert.True(t, tomato.Changes())
}

func TestPlanRejectsUncodedVariantByDefault(t *testing.T) {
	f := sampleFile()
	f.Templates[0].Variants[1].DefaultCode = ""
	plan, problems := buildPlan(f, emptySnapshot(), strict)
	require.Len(t, problems, 1)
	assert.Contains(t, problems[0], "variant 12663 has no default_code")
	assert.Len(t, plan.Templates, 1)
}

func TestPlanSkipsWholeTemplateWithUncodedVariant(t *testing.T) {
	f := sampleFile()
	f.Templates[0].Variants[1].DefaultCode = ""
	plan, problems := buildPlan(f, emptySnapshot(), importOptions{EmptyCode: emptyCodeSkip, SaleOK: saleOKAll})
	require.Empty(t, problems)
	require.Len(t, plan.Templates, 1)
	assert.Equal(t, 1487, plan.Templates[0].Template.SourceID)
	require.Len(t, plan.Skipped, 1)
	assert.Contains(t, plan.Skipped[0], "template 1643")
	assert.Empty(t, plan.NewAttributes)
}

func TestPlanSaleOKOnlySkipsTemplatesThatAreNotForSale(t *testing.T) {
	f := sampleFile()
	f.Templates[1].SaleOK = false
	plan, problems := buildPlan(f, emptySnapshot(), importOptions{EmptyCode: emptyCodeReject, SaleOK: saleOKOnly})
	require.Empty(t, problems)
	require.Len(t, plan.Templates, 1)
	assert.Len(t, plan.Skipped, 1)
	all, _ := buildPlan(f, emptySnapshot(), strict)
	assert.Len(t, all.Templates, 2)
}

func TestPlanRestoresArchivedTemplate(t *testing.T) {
	snap := matchingSnapshot()
	snap.Variants[2].Active = false
	plan, problems := buildPlan(sampleFile(), snap, strict)
	require.Empty(t, problems)
	require.Len(t, plan.Templates, 2)
	require.NotNil(t, plan.Templates[1].SetActive)
	assert.True(t, *plan.Templates[1].SetActive)
	assert.False(t, plan.Templates[1].Creates())
}

func TestPlanArchivesTemplateWhenEveryFileVariantIsInactive(t *testing.T) {
	f := sampleFile()
	f.Templates[0].Variants[0].Active, f.Templates[0].Variants[1].Active = false, false
	plan, problems := buildPlan(f, matchingSnapshot(), strict)
	require.Empty(t, problems)
	require.NotNil(t, plan.Templates[0].SetActive)
	assert.False(t, *plan.Templates[0].SetActive)
	assert.Nil(t, plan.Templates[0].Variants[0].SetActive)
}

func TestPlanArchivesOneVariantOfAnActiveTemplate(t *testing.T) {
	f := sampleFile()
	f.Templates[0].Variants[1].Active = false
	plan, problems := buildPlan(f, matchingSnapshot(), strict)
	require.Empty(t, problems)
	assert.Nil(t, plan.Templates[0].SetActive)
	require.NotNil(t, plan.Templates[0].Variants[1].SetActive)
	assert.False(t, *plan.Templates[0].Variants[1].SetActive)
}

func TestPlanRejectsUnsafeMatches(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(s *Snapshot)
		opt    importOptions
		want   string
	}{
		{"only some variants exist", func(s *Snapshot) { s.Variants = s.Variants[:1] }, strict, "1 of 2 variants exist"},
		{"code matches two variants", func(s *Snapshot) {
			s.Variants = append(s.Variants, targetVariant(9, 5009, "A-1"))
		}, strict, `default_code "A-1" matches 2 variants`},
		{"variants in different templates", func(s *Snapshot) { s.Variants[1].Template.ID = 5009 }, strict, "different templates"},
		{"unit is missing", func(s *Snapshot) { delete(s.UoMs, "kg") }, strict, `unit of measure "kg"`},
		{"category would be created", func(s *Snapshot) { delete(s.Categories, "水果") },
			importOptions{EmptyCode: emptyCodeReject, SaleOK: saleOKAll, NoCreateDeps: true}, `category "水果"`},
		{"attribute would be created", func(s *Snapshot) { delete(s.Attributes, "规格") },
			importOptions{EmptyCode: emptyCodeReject, SaleOK: saleOKAll, NoCreateDeps: true}, `attribute "规格"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snap := matchingSnapshot()
			tt.mutate(snap)
			_, problems := buildPlan(sampleFile(), snap, tt.opt)
			require.NotEmpty(t, problems)
			assert.Contains(t, problems[0], tt.want)
		})
	}
}

func TestPrintPlanSummarizesWhatWouldHappen(t *testing.T) {
	snap := emptySnapshot()
	delete(snap.Attributes, "规格")
	plan, problems := buildPlan(sampleFile(), snap, strict)
	require.Empty(t, problems)

	var out bytes.Buffer
	printPlan(&out, plan)
	text := out.String()
	assert.Contains(t, text, `create category "水果"`)
	assert.Contains(t, text, `create attribute "规格"`)
	assert.Contains(t, text, `create template 1487 "冬枣散装" with 1 variant(s)`)
	assert.Contains(t, text, "summary: 2 to create, 0 to update, 0 unchanged, 0 skipped")
}

func TestPlanRestoredTemplateKeepsInactiveVariantsArchived(t *testing.T) {
	snap := matchingSnapshot()
	snap.Variants[0].Active, snap.Variants[1].Active = false, false
	f := sampleFile()
	f.Templates[0].Variants[1].Active = false

	plan, problems := buildPlan(f, snap, strict)
	require.Empty(t, problems)
	tomato := plan.Templates[0]
	require.NotNil(t, tomato.SetActive)
	assert.True(t, *tomato.SetActive)
	assert.Nil(t, tomato.Variants[0].SetActive)
	require.NotNil(t, tomato.Variants[1].SetActive, "Odoo restores every variant with the template; the archived one must be archived again")
	assert.False(t, *tomato.Variants[1].SetActive)
}

func TestPlanRejectsAttributesThatCannotBeUsedForANewTemplate(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(a *SnapAttribute)
		want   string
	}{
		{"does not always create variants", func(a *SnapAttribute) { a.CreateVariant = "dynamic" }, `create_variant "dynamic"`},
		{"is archived", func(a *SnapAttribute) { a.Archived = true }, "is archived in the target"},
		{"value is archived", func(a *SnapAttribute) { a.ArchivedValues = map[string]bool{"1kg": true} }, `value "1kg" of attribute "规格" is archived`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snap := emptySnapshot()
			a := snap.Attributes["规格"]
			tt.mutate(&a)
			snap.Attributes["规格"] = a
			_, problems := buildPlan(sampleFile(), snap, strict)
			require.NotEmpty(t, problems)
			assert.Contains(t, problems[0], tt.want)
		})
	}
}

func TestPlanAllowsOddAttributesWhenTheTemplateAlreadyExists(t *testing.T) {
	snap := matchingSnapshot()
	a := snap.Attributes["规格"]
	a.CreateVariant = "dynamic"
	snap.Attributes["规格"] = a
	_, problems := buildPlan(sampleFile(), snap, strict)
	assert.Empty(t, problems)
}
