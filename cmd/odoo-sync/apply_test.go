package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/d2jvkpn/odoo-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeTarget records every write as a string and answers reads from canned data.
type fakeTarget struct {
	calls        []string
	generated    map[int][]odoo.Product // template ID -> the variants Odoo generates
	values       map[int]odoo.ProductAttributeValue
	nextTemplate int
	failTemplate string // name of a template whose creation fails
	failCategory bool
	failVariant  int // ID of a variant whose write fails
}

func (f *fakeTarget) record(format string, args ...any) {
	f.calls = append(f.calls, fmt.Sprintf(format, args...))
}

func (f *fakeTarget) CreateProductCategory(_ context.Context, _ odoo.CategoryType, in odoo.CreateProductCategoryRequest) (int, error) {
	if f.failCategory {
		return 0, errors.New("boom")
	}
	f.record("category:%s:parent=%d", in.Name, in.ParentID)
	return 800 + len(f.calls), nil
}

func (f *fakeTarget) CreateProductAttribute(_ context.Context, name string) (int, error) {
	f.record("attribute:%s", name)
	return 3, nil
}

func (f *fakeTarget) CreateProductAttributeValues(_ context.Context, attributeID int, names []string) ([]int, error) {
	f.record("values:%d:%v", attributeID, names)
	ids := make([]int, len(names))
	for i := range names {
		ids[i] = 10 + i
	}
	return ids, nil
}

func (f *fakeTarget) CreateProductTemplate(_ context.Context, in odoo.CreateTemplateRequest) (int, error) {
	if in.Name == f.failTemplate {
		return 0, errors.New("create refused")
	}
	f.nextTemplate++
	f.record("template:%s:category=%d:uom=%d", in.Name, in.CategoryID, in.UoMID)
	return 900 + f.nextTemplate - 1, nil
}

func (f *fakeTarget) AddProductAttributeLine(_ context.Context, templateID, attributeID int, valueIDs []int) (int, error) {
	f.record("line:%d:%d:%v", templateID, attributeID, valueIDs)
	return 1, nil
}

func (f *fakeTarget) ProductVariants(_ context.Context, templateID int) ([]odoo.Product, error) {
	return f.generated[templateID], nil
}

func (f *fakeTarget) ProductAttributeValues(context.Context, []int) (map[int]odoo.ProductAttributeValue, error) {
	return f.values, nil
}

func (f *fakeTarget) SetVariantPriceExtra(_ context.Context, id int, extra float64) error {
	f.record("price:%d:%v", id, extra)
	return nil
}

func (f *fakeTarget) UpdateProductTemplate(_ context.Context, id int, patch odoo.TemplatePatch) error {
	data, _ := json.Marshal(patch)
	f.record("update-template:%d:%s", id, data)
	return nil
}

func (f *fakeTarget) UpdateVariantFields(_ context.Context, id int, patch odoo.VariantPatch) error {
	if id == f.failVariant {
		return errors.New("duplicate barcode")
	}
	data, _ := json.Marshal(patch)
	f.record("update-variant:%d:%s", id, data)
	return nil
}

func (f *fakeTarget) SetProductActive(_ context.Context, id int, active bool) error {
	f.record("variant-active:%d:%t", id, active)
	return nil
}

func (f *fakeTarget) SetProductTemplateActive(_ context.Context, id int, active bool) error {
	f.record("template-active:%d:%t", id, active)
	return nil
}

func ptav(id int, value string) odoo.ProductAttributeValue {
	return odoo.ProductAttributeValue{ID: id, Attribute: odoo.Many2One{ID: 3, Name: "规格"}, Value: value}
}

func TestApplyCreatesDependenciesThenTemplateThenFieldsOnTheGeneratedVariants(t *testing.T) {
	snap := emptySnapshot()
	delete(snap.Attributes, "规格")
	plan, problems := buildPlan(sampleFile(), snap, strict)
	require.Empty(t, problems)
	fake := &fakeTarget{
		generated: map[int][]odoo.Product{
			900: {{ID: 101, ValueIDs: []int{30}}, {ID: 102, ValueIDs: []int{31}}},
			901: {{ID: 301, ValueIDs: []int{}}},
		},
		values: map[int]odoo.ProductAttributeValue{30: ptav(30, "1kg"), 31: ptav(31, "1.5kg")},
	}

	res, err := applyPlan(context.Background(), fake, snap, plan)
	require.NoError(t, err)

	assert.Equal(t, []string{
		"category:水果:parent=0",
		"attribute:规格",
		"values:3:[1.5kg 1kg]",
		"template:西红柿块:category=7:uom=1",
		"line:900:3:[11 10]",
		"price:31:2.5",
		`update-variant:101:{"default_code":"A-1"}`,
		`update-variant:102:{"default_code":"A-2"}`,
		"template:冬枣散装:category=801:uom=1",
		`update-variant:301:{"default_code":"273001101"}`,
	}, fake.calls)
	assert.Equal(t, []MapRow{
		{DefaultCode: "A-1", SourceID: 12664, TargetID: 101, TargetTemplateID: 900},
		{DefaultCode: "A-2", SourceID: 12663, TargetID: 102, TargetTemplateID: 900},
		{DefaultCode: "273001101", SourceID: 10688, TargetID: 301, TargetTemplateID: 901},
	}, res.Map)
	assert.Equal(t, 2, res.Created)
	assert.Zero(t, res.Failed)
}

func TestApplyCreatesSingleVariantTemplate(t *testing.T) {
	f := sampleFile()
	f.Templates = f.Templates[1:]
	plan, problems := buildPlan(f, emptySnapshot(), strict)
	require.Empty(t, problems)
	fake := &fakeTarget{generated: map[int][]odoo.Product{900: {{ID: 301}}}}

	res, err := applyPlan(context.Background(), fake, emptySnapshot(), plan)
	require.NoError(t, err)
	assert.Equal(t, []MapRow{{DefaultCode: "273001101", SourceID: 10688, TargetID: 301, TargetTemplateID: 900}}, res.Map)
	assert.Zero(t, res.Failed)
}

func TestApplyWarnsAboutUnlistedGeneratedVariant(t *testing.T) {
	f := sampleFile()
	f.Templates = f.Templates[:1]
	plan, problems := buildPlan(f, emptySnapshot(), strict)
	require.Empty(t, problems)
	fake := &fakeTarget{
		generated: map[int][]odoo.Product{
			900: {{ID: 101, ValueIDs: []int{30}}, {ID: 102, ValueIDs: []int{31}}, {ID: 103, ValueIDs: []int{32}}},
		},
		values: map[int]odoo.ProductAttributeValue{30: ptav(30, "1kg"), 31: ptav(31, "1.5kg"), 32: ptav(32, "2kg")},
	}

	res, err := applyPlan(context.Background(), fake, emptySnapshot(), plan)
	require.NoError(t, err)
	require.Len(t, res.Warnings, 1)
	assert.Contains(t, res.Warnings[0], "variant 103")
	assert.Contains(t, res.Warnings[0], "archived")
	assert.Len(t, res.Map, 2)
	assert.Contains(t, fake.calls, "variant-active:103:false", "a combination the file does not list must not stay on sale")
	assert.Equal(t, 1, res.Archived)
	for _, c := range fake.calls {
		assert.NotContains(t, c, "update-variant:103")
	}
}

func TestApplyArchivesVariantsAndTemplatesAsPlanned(t *testing.T) {
	f := sampleFile()
	f.Templates[0].Variants[1].Active = false
	f.Templates[1].Variants[0].Active = false
	snap := matchingSnapshot()
	snap.Variants[2].ValueIDs = nil
	plan, problems := buildPlan(f, snap, strict)
	require.Empty(t, problems)
	fake := &fakeTarget{}

	res, err := applyPlan(context.Background(), fake, snap, plan)
	require.NoError(t, err)
	assert.Equal(t, []string{"variant-active:2:false", "template-active:5002:false"}, fake.calls)
	assert.Equal(t, 2, res.Archived)
	assert.Equal(t, 2, res.Updated)
}

func TestApplyUpdatesOnlyTheFieldsThatDiffer(t *testing.T) {
	snap := matchingSnapshot()
	for i := range snap.Variants[:2] {
		snap.Variants[i].ListPrice = 3
	}
	v := snap.Values[31]
	v.PriceExtra = 1
	snap.Values[31] = v
	plan, problems := buildPlan(sampleFile(), snap, strict)
	require.Empty(t, problems)
	fake := &fakeTarget{}

	res, err := applyPlan(context.Background(), fake, snap, plan)
	require.NoError(t, err)
	assert.Equal(t, []string{`update-template:5001:{"list_price":1}`, "price:31:2.5"}, fake.calls)
	assert.Equal(t, 1, res.Updated)
	assert.Equal(t, 1, res.Unchanged)
	assert.Len(t, res.Map, 3)
}

func TestApplyContinuesAfterATemplateFailsAndNamesTheOrphan(t *testing.T) {
	plan, problems := buildPlan(sampleFile(), emptySnapshot(), strict)
	require.Empty(t, problems)
	fake := &fakeTarget{
		failTemplate: "冬枣散装",
		generated:    map[int][]odoo.Product{900: {{ID: 101, ValueIDs: []int{30}}, {ID: 102, ValueIDs: []int{31}}}},
		values:       map[int]odoo.ProductAttributeValue{30: ptav(30, "1kg"), 31: ptav(31, "1.5kg")},
	}

	res, err := applyPlan(context.Background(), fake, emptySnapshot(), plan)
	require.NoError(t, err)
	assert.Equal(t, 1, res.Failed)
	assert.Equal(t, 1, res.Created)
	require.Len(t, res.Failures, 1)
	assert.Contains(t, res.Failures[0], "冬枣散装")
}

func TestApplyAbortsWhenADependencyFails(t *testing.T) {
	plan, problems := buildPlan(sampleFile(), emptySnapshot(), strict)
	require.Empty(t, problems)
	plan.NewCategories = []string{"水果"}
	_, err := applyPlan(context.Background(), &fakeTarget{failCategory: true}, emptySnapshot(), plan)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `create category "水果"`)
}

func TestWriteMapCSV(t *testing.T) {
	path := filepath.Join(t.TempDir(), "map.csv")
	require.NoError(t, writeMapCSV(path, []MapRow{{DefaultCode: "B", SourceID: 2, TargetID: 20, TargetTemplateID: 200}, {DefaultCode: "A", SourceID: 1, TargetID: 10, TargetTemplateID: 100}}))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "default_code,source_variant_id,target_variant_id,target_template_id\nA,1,10,100\nB,2,20,200\n", string(data))
}

func TestApplyFailsATemplateWhoseFileVariantsOdooDidNotGenerate(t *testing.T) {
	f := sampleFile()
	f.Templates = f.Templates[:1]
	plan, problems := buildPlan(f, emptySnapshot(), strict)
	require.Empty(t, problems)
	fake := &fakeTarget{
		generated: map[int][]odoo.Product{900: {{ID: 101, ValueIDs: []int{30}}}},
		values:    map[int]odoo.ProductAttributeValue{30: ptav(30, "1kg")},
	}

	res, err := applyPlan(context.Background(), fake, emptySnapshot(), plan)
	require.NoError(t, err)
	assert.Equal(t, 1, res.Failed)
	assert.Zero(t, res.Created)
	require.Len(t, res.Failures, 1)
	assert.Contains(t, res.Failures[0], "did not generate")
	assert.Contains(t, res.Failures[0], "template 900 was created")
	for _, c := range fake.calls {
		assert.NotContains(t, c, "update-variant", "no code is written before every file variant is placed")
	}
}

func TestApplyPartialWriteFailureTellsHowToRecover(t *testing.T) {
	f := sampleFile()
	f.Templates = f.Templates[:1]
	plan, problems := buildPlan(f, emptySnapshot(), strict)
	require.Empty(t, problems)
	fake := &fakeTarget{
		failVariant: 102,
		generated:   map[int][]odoo.Product{900: {{ID: 101, ValueIDs: []int{30}}, {ID: 102, ValueIDs: []int{31}}}},
		values:      map[int]odoo.ProductAttributeValue{30: ptav(30, "1kg"), 31: ptav(31, "1.5kg")},
	}

	res, err := applyPlan(context.Background(), fake, emptySnapshot(), plan)
	require.NoError(t, err)
	require.Len(t, res.Failures, 1)
	assert.Contains(t, res.Failures[0], "clear the default_code")
	assert.NotContains(t, res.Failures[0], "archive it")
}

func TestApplyRestoresATemplateThenArchivesTheVariantsThatStayArchived(t *testing.T) {
	snap := matchingSnapshot()
	snap.Variants[0].Active, snap.Variants[1].Active = false, false
	f := sampleFile()
	f.Templates[0].Variants[1].Active = false
	plan, problems := buildPlan(f, snap, strict)
	require.Empty(t, problems)
	fake := &fakeTarget{}

	_, err := applyPlan(context.Background(), fake, snap, plan)
	require.NoError(t, err)
	assert.Equal(t, []string{"template-active:5001:true", "variant-active:2:false"}, fake.calls)
}
