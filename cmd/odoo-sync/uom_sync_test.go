package main

import (
	"context"
	"testing"
	"time"

	"github.com/d2jvkpn/odoo-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExportAndImportCreateMissingUnitBeforeProduct(t *testing.T) {
	src := fakeSource{
		variants: []odoo.CatalogVariant{{
			ID: 10, Template: odoo.Many2One{ID: 1, Name: "土豆丝"}, Name: "土豆丝",
			DefaultCode: "P-1", UoM: odoo.Many2One{ID: 2, Name: "包"}, Active: true,
		}},
		uoms: []odoo.SyncUoM{
			{ID: 1, Name: "个", RelativeFactor: 1, Active: true},
			{ID: 2, Name: "包", RelativeTo: odoo.Many2One{ID: 1, Name: "个"}, RelativeFactor: 12, Active: true},
		},
	}
	f, _, err := buildExport(context.Background(), src, "source", "http://source", time.Now(), "products")
	require.NoError(t, err)
	assert.Equal(t, []FileUoM{
		{Name: "个", RelativeFactor: 1},
		{Name: "包", RelativeTo: "个", RelativeFactor: 12},
	}, f.Units)

	snap := emptySnapshot()
	snap.UoMs = map[string]int{"个": 7}
	snap.UnitDetails = map[string]odoo.SyncUoM{"个": {ID: 7, Name: "个", RelativeFactor: 1, Active: true}}
	plan, problems := buildPlan(f, snap, strict)
	require.Empty(t, problems)
	assert.Equal(t, []FileUoM{{Name: "包", RelativeTo: "个", RelativeFactor: 12}}, plan.NewUoMs)

	fake := &fakeTarget{generated: map[int][]odoo.Product{900: {{ID: 101}}}}
	_, err = applyPlan(context.Background(), fake, snap, plan)
	require.NoError(t, err)
	assert.Equal(t, "uom:包:parent=7:factor=12", fake.calls[0])
	assert.Contains(t, fake.calls, "template:土豆丝:category=0:uom=801")
}

func TestImportRefusesConflictingExistingUnit(t *testing.T) {
	f := sampleFile()
	f.Units = []FileUoM{{Name: "kg", RelativeFactor: 1}}
	snap := emptySnapshot()
	snap.UnitDetails = map[string]odoo.SyncUoM{
		"kg": {ID: 1, Name: "kg", RelativeTo: odoo.Many2One{ID: 2, Name: "g"}, RelativeFactor: 1000},
	}
	_, problems := buildPlan(f, snap, strict)
	require.NotEmpty(t, problems)
	assert.Contains(t, problems[0], "kg")
}

func TestImportWithNoCreateDepsReportsMissingUnit(t *testing.T) {
	f := sampleFile()
	f.Units = []FileUoM{{Name: "kg", RelativeFactor: 1}}
	snap := emptySnapshot()
	delete(snap.UoMs, "kg")
	opt := strict
	opt.NoCreateDeps = true
	_, problems := buildPlan(f, snap, opt)
	assert.Contains(t, problems, `unit "kg" does not exist in the target`)
}

func TestValidateRejectsUnitReferenceCycle(t *testing.T) {
	f := sampleFile()
	f.Units = []FileUoM{
		{Name: "包", RelativeTo: "个", RelativeFactor: 12},
		{Name: "个", RelativeTo: "包", RelativeFactor: 1},
	}
	problems := f.Validate()
	require.NotEmpty(t, problems)
	assert.Contains(t, problems[0], "cycle")
}
