package main

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/d2jvkpn/odoo-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func emptySnap() *PartnerSnapshot {
	return &PartnerSnapshot{
		Tags: map[string]int{"VIP": 5}, TagNames: map[int]string{5: "VIP"},
		Countries: map[string]int{"Norway": 160},
		States:    map[int]map[string]int{160: {"Oslo": 1}},
	}
}

func targetPartner(id int, name, ref, vat, email string, parent int) odoo.SyncPartner {
	p := odoo.SyncPartner{Active: true}
	p.ID, p.Name, p.Type = id, name, "contact"
	p.Ref, p.VAT, p.Email = odoo.OptionalString(ref), odoo.OptionalString(vat), odoo.OptionalString(email)
	if parent != 0 {
		p.Parent = odoo.Many2One{ID: parent, Name: "parent"}
	}
	return p
}

func opFor(t *testing.T, plan *PartnerPlan, source int) PartnerOp {
	t.Helper()
	for _, op := range plan.Ops {
		if op.Source.SourceID == source {
			return op
		}
	}
	t.Fatalf("no op for source %d", source)
	return PartnerOp{}
}

func TestPlanCreatesEverythingInAnEmptyTargetParentsFirst(t *testing.T) {
	f := samplePartners()
	f.Partners[0], f.Partners[1] = f.Partners[1], f.Partners[0] // child listed before its parent
	plan, problems := buildPartnerPlan(f, emptySnap(), partnerOptions{EmptyKey: emptyKeyReject})
	require.Empty(t, problems)
	require.Len(t, plan.Ops, 3)
	for _, op := range plan.Ops {
		assert.Equal(t, opCreate, op.Kind)
	}
	assert.Equal(t, 160, opFor(t, plan, 10).CountryID)
	assert.Equal(t, 1, opFor(t, plan, 10).StateID)
	assert.Less(t, indexOf(plan, 10), indexOf(plan, 11))
	assert.Empty(t, plan.NewTags)
}

func indexOf(plan *PartnerPlan, source int) int {
	for i, op := range plan.Ops {
		if op.Source.SourceID == source {
			return i
		}
	}
	return -1
}

func TestPlanMatchesByRefThenVATThenEmailAndFallsThrough(t *testing.T) {
	f := &PartnersFile{Format: partnersFormat, Partners: []FilePartner{
		{SourceID: 1, Name: "ByRef", Type: "contact", Active: true, Ref: "r1", VAT: "v-other"},
		{SourceID: 2, Name: "ByVAT", Type: "contact", Active: true, Ref: "r-new", VAT: "v2"},
		{SourceID: 3, Name: "ByEmail", Type: "contact", Active: true, Email: "E3@X.com"},
	}}
	snap := emptySnap()
	snap.Partners = []odoo.SyncPartner{
		targetPartner(90, "ByRef", "r1", "", "", 0),
		targetPartner(91, "ByVAT", "", "v2", "", 0),
		targetPartner(92, "ByEmail", "", "", "e3@x.com", 0),
		targetPartner(93, "A child", "r1", "", "", 90), // a child never matches a top-level partner
	}
	plan, problems := buildPartnerPlan(f, snap, partnerOptions{EmptyKey: emptyKeyReject})
	require.Empty(t, problems)
	assert.Equal(t, 90, opFor(t, plan, 1).TargetID)
	assert.Equal(t, "ref", opFor(t, plan, 1).Matched)
	assert.Equal(t, 91, opFor(t, plan, 2).TargetID)
	assert.Equal(t, "vat", opFor(t, plan, 2).Matched)
	assert.Equal(t, 92, opFor(t, plan, 3).TargetID)
	assert.Equal(t, "email", opFor(t, plan, 3).Matched)
}

func TestPlanDetectsUnchangedUpdatedArchivedAndRestored(t *testing.T) {
	f := &PartnersFile{Format: partnersFormat, Partners: []FilePartner{
		{SourceID: 1, Name: "Same", Type: "contact", Active: true, IsCompany: true, VAT: "v1"},
		{SourceID: 2, Name: "New name", Type: "contact", Active: true, VAT: "v2", Phone: "123", Categories: []string{"VIP"}},
		{SourceID: 3, Name: "Gone", Type: "contact", Active: false, VAT: "v3"},
		{SourceID: 4, Name: "Back", Type: "contact", Active: true, VAT: "v4"},
	}}
	snap := emptySnap()
	same := targetPartner(90, "Same", "", "v1", "", 0)
	same.IsCompany = true
	back := targetPartner(93, "Back", "", "v4", "", 0)
	back.Active = false
	snap.Partners = []odoo.SyncPartner{
		same,
		targetPartner(91, "Old name", "", "v2", "", 0),
		targetPartner(92, "Gone", "", "v3", "", 0),
		back,
	}
	plan, problems := buildPartnerPlan(f, snap, partnerOptions{EmptyKey: emptyKeyReject})
	require.Empty(t, problems)
	assert.Equal(t, opUnchanged, opFor(t, plan, 1).Kind)
	two := opFor(t, plan, 2)
	assert.Equal(t, opUpdate, two.Kind)
	assert.ElementsMatch(t, []string{"name", "phone", "category_id"}, two.Changed)
	assert.True(t, opFor(t, plan, 3).Archive)
	assert.True(t, opFor(t, plan, 4).Restore)
}

func TestPlanMatchesChildrenByParentTypeAndName(t *testing.T) {
	snap := emptySnap()
	snap.Partners = []odoo.SyncPartner{
		targetPartner(90, "Alice Co.", "user-42", "", "", 0),
		func() odoo.SyncPartner {
			c := targetPartner(91, "Warehouse", "", "", "", 90)
			c.Type = "delivery"
			return c
		}(),
	}
	plan, problems := buildPartnerPlan(samplePartners(), snap, partnerOptions{EmptyKey: emptyKeyReject})
	require.Empty(t, problems)
	child := opFor(t, plan, 11)
	assert.Equal(t, 91, child.TargetID)
	assert.Equal(t, "parent+type+name", child.Matched)
}

func TestPlanStopsOnAmbiguousAndDoubleMatches(t *testing.T) {
	f := &PartnersFile{Format: partnersFormat, Partners: []FilePartner{
		{SourceID: 1, Name: "Ambiguous", Type: "contact", Active: true, Email: "a@x.com"},
		{SourceID: 2, Name: "First", Type: "contact", Active: true, Ref: "r2", VAT: "v"},
		{SourceID: 3, Name: "Second", Type: "contact", Active: true, Ref: "r2"},
	}}
	snap := emptySnap()
	snap.Partners = []odoo.SyncPartner{
		targetPartner(90, "X", "", "", "a@x.com", 0),
		targetPartner(91, "Y", "", "", "A@x.com", 0),
		targetPartner(92, "Z", "r2", "v", "", 0),
	}
	_, problems := buildPartnerPlan(f, snap, partnerOptions{EmptyKey: emptyKeyReject})
	joined := ""
	for _, p := range problems {
		joined += p + "\n"
	}
	assert.Contains(t, joined, `email "a@x.com" matches 2 target partners (ids 90, 91)`)
	assert.Contains(t, joined, "both match target partner 92")
}

func TestPlanEmptyKeyRejectsOrSkipsWithChildren(t *testing.T) {
	f := &PartnersFile{Format: partnersFormat, Partners: []FilePartner{
		{SourceID: 1, Name: "Keyless", Type: "contact", Active: true},
		{SourceID: 2, ParentSourceID: 1, Name: "Keyless child", Type: "other", Active: true},
		{SourceID: 3, Name: "Keyed", Type: "contact", Active: true, Ref: "r3"},
	}}
	_, problems := buildPartnerPlan(f, emptySnap(), partnerOptions{EmptyKey: emptyKeyReject})
	require.Len(t, problems, 1)
	assert.Contains(t, problems[0], "no ref, vat or email")

	plan, problems := buildPartnerPlan(f, emptySnap(), partnerOptions{EmptyKey: emptyKeySkip})
	require.Empty(t, problems)
	assert.Equal(t, opSkip, opFor(t, plan, 1).Kind)
	assert.Equal(t, opSkip, opFor(t, plan, 2).Kind)
	assert.Contains(t, opFor(t, plan, 2).Reason, "parent")
	assert.Equal(t, opCreate, opFor(t, plan, 3).Kind)
}

func TestPlanReportsMissingCountryStateAndTags(t *testing.T) {
	f := &PartnersFile{Format: partnersFormat, Partners: []FilePartner{
		{SourceID: 1, Name: "A", Type: "contact", Active: true, Ref: "a", Country: "Atlantis"},
		{SourceID: 2, Name: "B", Type: "contact", Active: true, Ref: "b", Country: "Norway", State: "Nowhere"},
		{SourceID: 3, Name: "C", Type: "contact", Active: true, Ref: "c", Categories: []string{"New", "New", "VIP"}},
	}}
	_, problems := buildPartnerPlan(f, emptySnap(), partnerOptions{EmptyKey: emptyKeyReject})
	assert.Len(t, problems, 2)
	assert.Contains(t, problems[0], `country "Atlantis"`)
	assert.Contains(t, problems[1], `state "Nowhere"`)

	f.Partners = f.Partners[2:]
	plan, problems := buildPartnerPlan(f, emptySnap(), partnerOptions{EmptyKey: emptyKeyReject})
	require.Empty(t, problems)
	assert.Equal(t, []string{"New"}, plan.NewTags)

	_, problems = buildPartnerPlan(f, emptySnap(), partnerOptions{EmptyKey: emptyKeyReject, NoCreateDeps: true})
	require.Len(t, problems, 1)
	assert.Contains(t, problems[0], `tag "New"`)
}

type fakePartnerSnapshotAPI struct{ stateCalls []int }

func (f *fakePartnerSnapshotAPI) ListActiveLanguages(context.Context) ([]string, error) {
	return []string{"en_US", "fr_FR"}, nil
}

func (f *fakePartnerSnapshotAPI) ListSyncPartners(context.Context) ([]odoo.SyncPartner, error) {
	return []odoo.SyncPartner{targetPartner(90, "X", "r", "", "", 0)}, nil
}
func (f *fakePartnerSnapshotAPI) ListPartnerTags(context.Context) ([]odoo.PartnerTag, error) {
	return []odoo.PartnerTag{{ID: 5, Name: "VIP"}}, nil
}
func (f *fakePartnerSnapshotAPI) ListCountries(context.Context) ([]odoo.Country, error) {
	return []odoo.Country{{ID: 160, Name: "Norway"}, {ID: 48, Name: "China"}}, nil
}
func (f *fakePartnerSnapshotAPI) ListCountryStates(_ context.Context, id int) ([]odoo.CountryState, error) {
	f.stateCalls = append(f.stateCalls, id)
	return []odoo.CountryState{{ID: 1, Name: "Oslo", Code: "03"}}, nil
}

func TestLoadPartnerSnapshotReadsStatesOnlyForCountriesTheFileUses(t *testing.T) {
	api := &fakePartnerSnapshotAPI{}
	snap, err := loadPartnerSnapshot(context.Background(), api, samplePartners())
	require.NoError(t, err)
	assert.Equal(t, []int{160}, api.stateCalls)
	assert.Equal(t, 160, snap.Countries["Norway"])
	assert.Equal(t, map[string]int{"Oslo": 1}, snap.States[160])
	assert.Equal(t, 5, snap.Tags["VIP"])
	assert.Equal(t, "VIP", snap.TagNames[5])
	assert.Len(t, snap.Partners, 1)
	assert.Equal(t, map[string]bool{"en_US": true, "fr_FR": true}, snap.Langs)
}

func TestPlanReportsLanguagesNotInstalledInTheTarget(t *testing.T) {
	f := &PartnersFile{Format: partnersFormat, Partners: []FilePartner{
		{SourceID: 1, Name: "Bad", Type: "contact", Active: true, Ref: "a", Lang: "fr_FR"},
		{SourceID: 2, Name: "Good", Type: "contact", Active: true, Ref: "b", Lang: "en_US"},
		{SourceID: 3, Name: "Skipped", Type: "contact", Active: true, Lang: "de_DE"},
	}}
	snap := emptySnap()
	snap.Langs = map[string]bool{"en_US": true}

	_, problems := buildPartnerPlan(f, snap, partnerOptions{EmptyKey: emptyKeySkip})
	require.Len(t, problems, 1)
	assert.Equal(t, `partner 1 "Bad": language "fr_FR" is not installed in the target`, problems[0])

	snap.Langs = nil // not loaded: not checked
	_, problems = buildPartnerPlan(f, snap, partnerOptions{EmptyKey: emptyKeySkip})
	assert.Empty(t, problems)
}

func TestPrintPartnerPlan(t *testing.T) {
	plan, problems := buildPartnerPlan(samplePartners(), emptySnap(), partnerOptions{EmptyKey: emptyKeyReject})
	require.Empty(t, problems)
	var out bytes.Buffer
	printPartnerPlan(&out, plan)
	assert.Contains(t, out.String(), `create partner 10 "Alice Co."`)
	assert.Contains(t, out.String(), "summary: 3 to create, 0 to update, 0 unchanged, 0 skipped, 0 to archive, 0 to restore")
}

func TestPlanSkippedKeylessPartnerDoesNoLookups(t *testing.T) {
	f := &PartnersFile{Format: partnersFormat, Partners: []FilePartner{
		{SourceID: 1, Name: "K", Type: "contact", Active: true, Country: "Atlantis", Categories: []string{"New"}},
	}}
	for _, opt := range []partnerOptions{
		{EmptyKey: emptyKeySkip},
		{EmptyKey: emptyKeySkip, NoCreateDeps: true},
	} {
		plan, problems := buildPartnerPlan(f, emptySnap(), opt)
		require.Empty(t, problems)
		assert.Empty(t, plan.NewTags)
		assert.Equal(t, opSkip, opFor(t, plan, 1).Kind)
	}
}

func TestPlanMatchesInPrioritiesAcrossTheWholeFile(t *testing.T) {
	for _, key := range []string{"vat", "email"} {
		a := FilePartner{SourceID: 1, Name: "A", Type: "contact", Active: true}
		b := FilePartner{SourceID: 2, Name: "B", Type: "contact", Active: true, Ref: "R1"}
		ta := targetPartner(90, "A", "", "", "", 0)
		tb := targetPartner(91, "B", "R1", "", "", 0)
		if key == "vat" {
			a.VAT, b.VAT = "BE1", "BE1"
			ta.VAT, tb.VAT = "BE1", "BE1"
		} else {
			a.Email, b.Email = "x@x.com", "x@x.com"
			ta.Email, tb.Email = "x@x.com", "x@x.com"
		}
		f := &PartnersFile{Format: partnersFormat, Partners: []FilePartner{a, b}}
		snap := emptySnap()
		snap.Partners = []odoo.SyncPartner{ta, tb}

		plan, problems := buildPartnerPlan(f, snap, partnerOptions{EmptyKey: emptyKeyReject})
		require.Empty(t, problems, key)
		assert.Equal(t, 90, opFor(t, plan, 1).TargetID, key)
		assert.Equal(t, key, opFor(t, plan, 1).Matched)
		assert.Equal(t, 91, opFor(t, plan, 2).TargetID, key)
		assert.Equal(t, "ref", opFor(t, plan, 2).Matched)
	}
}

func TestPlanStillReportsTwoPartnersMatchingTheSameUnclaimedTarget(t *testing.T) {
	f := &PartnersFile{Format: partnersFormat, Partners: []FilePartner{
		{SourceID: 1, Name: "A", Type: "contact", Active: true, VAT: "v"},
		{SourceID: 2, Name: "B", Type: "contact", Active: true, VAT: "v"},
	}}
	snap := emptySnap()
	snap.Partners = []odoo.SyncPartner{targetPartner(90, "X", "", "v", "", 0)}
	_, problems := buildPartnerPlan(f, snap, partnerOptions{EmptyKey: emptyKeyReject})
	require.Len(t, problems, 1)
	assert.Contains(t, problems[0], "partners 1 and 2 both match target partner 90")
}

func TestPlanTreatsDuplicateFileTagsAsOneLink(t *testing.T) {
	f := &PartnersFile{Format: partnersFormat, Partners: []FilePartner{
		{SourceID: 1, Name: "A", Type: "contact", Active: true, VAT: "v", Categories: []string{"VIP", "VIP"}},
	}}
	snap := emptySnap()
	have := targetPartner(90, "A", "", "v", "", 0)
	have.CategoryIDs = []int{5}
	snap.Partners = []odoo.SyncPartner{have}
	plan, problems := buildPartnerPlan(f, snap, partnerOptions{EmptyKey: emptyKeyReject})
	require.Empty(t, problems)
	assert.Equal(t, opUnchanged, opFor(t, plan, 1).Kind)
}

func TestPlanDoesNotCompareAnEmptyLang(t *testing.T) {
	f := &PartnersFile{Format: partnersFormat, Partners: []FilePartner{
		{SourceID: 1, Name: "A", Type: "contact", Active: true, VAT: "v1"},
		{SourceID: 2, Name: "B", Type: "contact", Active: true, VAT: "v2", Lang: "fr_FR"},
	}}
	snap := emptySnap()
	a := targetPartner(90, "A", "", "v1", "", 0)
	a.Lang = "en_US"
	b := targetPartner(91, "B", "", "v2", "", 0)
	b.Lang = "en_US"
	snap.Partners = []odoo.SyncPartner{a, b}
	plan, problems := buildPartnerPlan(f, snap, partnerOptions{EmptyKey: emptyKeyReject})
	require.Empty(t, problems)
	assert.Equal(t, opUnchanged, opFor(t, plan, 1).Kind)
	assert.Equal(t, []string{"lang"}, opFor(t, plan, 2).Changed)
}

type noLanguagesAPI struct{ fakePartnerSnapshotAPI }

func (*noLanguagesAPI) ListActiveLanguages(context.Context) ([]string, error) {
	return nil, errors.New("boom")
}

func TestLoadPartnerSnapshotWrapsALanguagesError(t *testing.T) {
	_, err := loadPartnerSnapshot(context.Background(), &noLanguagesAPI{}, samplePartners())
	assert.ErrorContains(t, err, "read target languages")
}
