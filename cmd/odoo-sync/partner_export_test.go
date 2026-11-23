package main

import (
	"context"
	"testing"
	"time"

	"github.com/d2jvkpn/odoo-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakePartnerSource struct {
	partners []odoo.SyncPartner
	tags     []odoo.PartnerTag
	states   []odoo.StateInfo
}

func (f fakePartnerSource) ListSyncPartners(context.Context) ([]odoo.SyncPartner, error) {
	return f.partners, nil
}
func (f fakePartnerSource) ListPartnerTags(context.Context) ([]odoo.PartnerTag, error) {
	return f.tags, nil
}
func (f fakePartnerSource) ListStatesByIDs(context.Context, []int) ([]odoo.StateInfo, error) {
	return f.states, nil
}

func srcPartner(id int, name string, parent int, active bool) odoo.SyncPartner {
	p := odoo.SyncPartner{Active: active}
	p.ID, p.Name, p.Type = id, name, "contact"
	p.Parent = odoo.Many2One{ID: parent, Name: "parent"}
	if parent == 0 {
		p.Parent = odoo.Many2One{}
	}
	p.CategoryIDs = []int{}
	return p
}

func TestBuildPartnerExportMapsFieldsAndSortsTags(t *testing.T) {
	alice := srcPartner(10, "Alice Co.", 0, true)
	alice.IsCompany, alice.Ref, alice.Email = true, "user-42", "a@x.com"
	alice.Country = odoo.Many2One{ID: 160, Name: "Norway"}
	alice.State = odoo.Many2One{ID: 1, Name: "Oslo (NO)"}
	alice.CategoryIDs = []int{6, 5}
	child := srcPartner(11, "Warehouse", 10, false)
	child.Type = "delivery"
	src := fakePartnerSource{
		partners: []odoo.SyncPartner{child, alice}, // out of order on purpose
		tags:     []odoo.PartnerTag{{ID: 5, Name: "VIP"}, {ID: 6, Name: "Retail"}},
		states:   []odoo.StateInfo{{ID: 1, Name: "Oslo", Code: "03", Country: odoo.Many2One{ID: 160, Name: "Norway"}}},
	}

	f, warnings, err := buildPartnerExport(context.Background(), src, "src", "http://src", time.Date(2026, 11, 22, 0, 0, 0, 0, time.UTC), false)
	require.NoError(t, err)
	assert.Empty(t, warnings)
	assert.Equal(t, "2026-11-22T00:00:00Z", f.ExportedAt)
	require.Len(t, f.Partners, 2)
	a, c := f.Partners[0], f.Partners[1]
	assert.Equal(t, 10, a.SourceID)
	assert.Equal(t, "Norway", a.Country)
	assert.Equal(t, "Oslo", a.State)
	assert.Equal(t, []string{"Retail", "VIP"}, a.Categories)
	assert.True(t, a.IsCompany)
	assert.Equal(t, 11, c.SourceID)
	assert.Equal(t, 10, c.ParentSourceID)
	assert.False(t, c.Active)
	assert.Empty(t, f.Validate())
}

func TestBuildPartnerExportDropsAStateOfAnotherCountryWithAWarning(t *testing.T) {
	p := srcPartner(1, "Mixed", 0, true)
	p.Country = odoo.Many2One{ID: 48, Name: "China"}
	p.State = odoo.Many2One{ID: 13, Name: "California (US)"}
	src := fakePartnerSource{
		partners: []odoo.SyncPartner{p},
		states:   []odoo.StateInfo{{ID: 13, Name: "California", Country: odoo.Many2One{ID: 233, Name: "United States"}}},
	}
	f, warnings, err := buildPartnerExport(context.Background(), src, "s", "u", time.Now(), false)
	require.NoError(t, err)
	assert.Empty(t, f.Partners[0].State)
	assert.Equal(t, "China", f.Partners[0].Country)
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], "state")
	assert.Empty(t, f.Validate())
	f, warnings, err = buildPartnerExport(context.Background(), src, "s", "u", time.Now(), true)
	require.NoError(t, err)
	assert.Equal(t, "China", f.Partners[0].Country)
	assert.Empty(t, f.Partners[0].State)
	require.Len(t, warnings, 1)
}

func TestBuildPartnerExportInfersMissingCountryFromStateWhenRequested(t *testing.T) {
	p := srcPartner(18, "杭州门店", 0, true)
	p.State = odoo.Many2One{ID: 31, Name: "浙江省"}
	src := fakePartnerSource{
		partners: []odoo.SyncPartner{p},
		states: []odoo.StateInfo{{
			ID: 31, Name: "浙江省", Country: odoo.Many2One{ID: 48, Name: "China"},
		}},
	}
	f, warnings, err := buildPartnerExport(context.Background(), src, "s", "u", time.Now(), true)
	require.NoError(t, err)
	require.Len(t, f.Partners, 1)
	assert.Equal(t, "China", f.Partners[0].Country)
	assert.Equal(t, "浙江省", f.Partners[0].State)
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], "inferred country")
	assert.Empty(t, f.Validate())
}

func TestBuildPartnerExportWarnsAboutAnUnknownTag(t *testing.T) {
	p := srcPartner(1, "Tagged", 0, true)
	p.CategoryIDs = []int{99}
	f, warnings, err := buildPartnerExport(context.Background(), fakePartnerSource{partners: []odoo.SyncPartner{p}}, "s", "u", time.Now(), false)
	require.NoError(t, err)
	assert.Empty(t, f.Partners[0].Categories)
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], "tag 99")
}

func TestBuildPartnerExportFailsOnABlankNameOrMissingParent(t *testing.T) {
	_, _, err := buildPartnerExport(context.Background(), fakePartnerSource{partners: []odoo.SyncPartner{srcPartner(1, " ", 0, true)}}, "s", "u", time.Now(), false)
	assert.ErrorContains(t, err, "has no name")
	_, _, err = buildPartnerExport(context.Background(), fakePartnerSource{partners: []odoo.SyncPartner{srcPartner(2, "Child", 77, true)}}, "s", "u", time.Now(), false)
	assert.ErrorContains(t, err, "parent 77")
}

func TestBuildPartnerExportDropsDuplicateTagNames(t *testing.T) {
	p := srcPartner(10, "Alice Co.", 0, true)
	p.CategoryIDs = []int{5, 7, 6}
	src := fakePartnerSource{
		partners: []odoo.SyncPartner{p},
		tags:     []odoo.PartnerTag{{ID: 5, Name: "VIP"}, {ID: 6, Name: "Retail"}, {ID: 7, Name: "VIP"}},
	}

	f, _, err := buildPartnerExport(context.Background(), src, "src", "http://src", time.Now(), false)
	require.NoError(t, err)
	assert.Equal(t, []string{"Retail", "VIP"}, f.Partners[0].Categories)
}
