package main

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/d2jvkpn/odoo-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeWriter struct {
	nextID      int
	calls       []string
	creates     []odoo.CreatePartnerRequest
	patches     map[int]odoo.UpdatePartnerRequest
	failCreate  map[string]bool // by partner name
	failTag     bool
	failUpdate  map[int]bool // by target id
	failActive  map[int]bool // by target id
	afterCreate func()       // runs after each successful create
}

func newFakeWriter() *fakeWriter {
	return &fakeWriter{nextID: 100, patches: map[int]odoo.UpdatePartnerRequest{}, failCreate: map[string]bool{},
		failUpdate: map[int]bool{}, failActive: map[int]bool{}}
}

func (f *fakeWriter) CreatePartner(_ context.Context, in odoo.CreatePartnerRequest) (int, error) {
	if f.failCreate[in.Name] {
		return 0, errors.New("boom")
	}
	f.nextID++
	f.creates = append(f.creates, in)
	f.calls = append(f.calls, fmt.Sprintf("create %s=%d", in.Name, f.nextID))
	if f.afterCreate != nil {
		f.afterCreate()
	}
	return f.nextID, nil
}
func (f *fakeWriter) UpdatePartner(_ context.Context, id int, p odoo.UpdatePartnerRequest) error {
	if f.failUpdate[id] {
		return errors.New("update boom")
	}
	f.patches[id] = p
	f.calls = append(f.calls, fmt.Sprintf("update %d", id))
	return nil
}
func (f *fakeWriter) SetPartnerActive(_ context.Context, id int, active bool) error {
	if f.failActive[id] {
		return errors.New("active boom")
	}
	f.calls = append(f.calls, fmt.Sprintf("active %d=%v", id, active))
	return nil
}
func (f *fakeWriter) CreatePartnerTag(_ context.Context, name string) (int, error) {
	if f.failTag {
		return 0, errors.New("tag boom")
	}
	f.nextID++
	f.calls = append(f.calls, "tag "+name)
	return f.nextID, nil
}

func TestApplyCreatesParentsBeforeChildrenAndResolvesIDs(t *testing.T) {
	snap := emptySnap()
	plan, problems := buildPartnerPlan(samplePartners(), snap, partnerOptions{EmptyKey: emptyKeyReject})
	require.Empty(t, problems)
	w := newFakeWriter()

	res := applyPartnerPlan(context.Background(), w, snap, plan)
	assert.Equal(t, 3, res.Created)
	assert.Zero(t, res.Failed)
	require.Len(t, w.creates, 3)
	assert.Equal(t, "Alice Co.", w.creates[0].Name)
	assert.Equal(t, 160, w.creates[0].CountryID)
	assert.Equal(t, odoo.ReplaceIDs{5}, w.creates[0].CategoryIDs)
	assert.Equal(t, "Bob Ltd.", w.creates[1].Name) // top-level partners first, then children
	assert.Equal(t, "Warehouse", w.creates[2].Name)
	assert.Equal(t, 101, w.creates[2].ParentID) // Alice Co. got id 101
}

func TestApplySkipsChildrenOfAFailedParentAndKeepsGoing(t *testing.T) {
	snap := emptySnap()
	plan, problems := buildPartnerPlan(samplePartners(), snap, partnerOptions{EmptyKey: emptyKeyReject})
	require.Empty(t, problems)
	w := newFakeWriter()
	w.failCreate["Alice Co."] = true

	res := applyPartnerPlan(context.Background(), w, snap, plan)
	assert.Equal(t, 1, res.Created) // Bob Ltd.
	assert.Equal(t, 2, res.Failed)  // Alice Co. and its child
	require.Len(t, res.Failures, 2)
	assert.Contains(t, res.Failures[0], "search the target")
	assert.Contains(t, res.Failures[1], "parent 10 was not written")
}

func TestApplyUpdatesOnlyChangedFieldsThenRestoresThenArchives(t *testing.T) {
	f := &PartnersFile{Format: partnersFormat, Partners: []FilePartner{
		{SourceID: 1, Name: "New name", Type: "contact", Active: true, VAT: "v1", Phone: "", Categories: []string{"VIP"}},
		{SourceID: 2, Name: "Gone", Type: "contact", Active: false, VAT: "v2"},
		{SourceID: 3, Name: "Back", Type: "contact", Active: true, VAT: "v3"},
		{SourceID: 4, Name: "Fresh archived", Type: "contact", Active: false, VAT: "v4"},
	}}
	snap := emptySnap()
	old := targetPartner(90, "Old name", "", "v1", "", 0)
	old.Phone = "999"
	back := targetPartner(93, "Back", "", "v3", "", 0)
	back.Active = false
	snap.Partners = []odoo.SyncPartner{old, targetPartner(91, "Gone", "", "v2", "", 0), back}
	plan, problems := buildPartnerPlan(f, snap, partnerOptions{EmptyKey: emptyKeyReject})
	require.Empty(t, problems)
	w := newFakeWriter()

	res := applyPartnerPlan(context.Background(), w, snap, plan)
	assert.Zero(t, res.Failed)
	assert.Equal(t, 1, res.Updated)
	assert.Equal(t, 1, res.Created)
	assert.Equal(t, 1, res.Restored)
	assert.Equal(t, 2, res.Archived)
	patch := w.patches[90]
	require.NotNil(t, patch.Name)
	assert.Equal(t, "New name", *patch.Name)
	require.NotNil(t, patch.Phone)
	assert.Equal(t, "", *patch.Phone) // the file has no phone, so the target's is cleared
	require.NotNil(t, patch.CategoryIDs)
	assert.Equal(t, odoo.ReplaceIDs{5}, *patch.CategoryIDs)
	assert.Nil(t, patch.Email)
	assert.Equal(t, []string{"update 90", "create Fresh archived=101", "active 91=false", "active 101=false", "active 93=true"}, w.calls)
}

func TestApplyCreatesMissingTagsFirstAndFailsPartnersNeedingAFailedTag(t *testing.T) {
	f := &PartnersFile{Format: partnersFormat, Partners: []FilePartner{
		{SourceID: 1, Name: "Tagged", Type: "contact", Active: true, Ref: "a", Categories: []string{"New"}},
		{SourceID: 2, Name: "Plain", Type: "contact", Active: true, Ref: "b"},
	}}
	snap := emptySnap()
	plan, problems := buildPartnerPlan(f, snap, partnerOptions{EmptyKey: emptyKeyReject})
	require.Empty(t, problems)

	w := newFakeWriter()
	res := applyPartnerPlan(context.Background(), w, snap, plan)
	assert.Equal(t, []string{"tag New", "create Tagged=102", "create Plain=103"}, w.calls)
	assert.Equal(t, 2, res.Created)

	w = newFakeWriter()
	w.failTag = true
	res = applyPartnerPlan(context.Background(), w, snap, plan)
	assert.Equal(t, 1, res.Created) // Plain
	assert.Equal(t, 2, res.Failed)  // the tag and Tagged
	assert.Contains(t, res.Failures[len(res.Failures)-1], `tag "New" was not created`)
	assert.NotContains(t, res.Failures[len(res.Failures)-1], "search the target") // no request was sent
}

func cascadeFile(companyActive, childActive bool) *PartnersFile {
	return &PartnersFile{Format: partnersFormat, Partners: []FilePartner{
		{SourceID: 1, Name: "C", Type: "contact", Active: companyActive, IsCompany: true, VAT: "vc"},
		{SourceID: 2, ParentSourceID: 1, Name: "K", Type: "contact", Active: childActive},
	}}
}

func cascadeSnap() *PartnerSnapshot {
	snap := emptySnap()
	c := targetPartner(90, "C", "", "vc", "", 0)
	c.IsCompany = true
	snap.Partners = []odoo.SyncPartner{c, targetPartner(91, "K", "", "", "", 90)}
	return snap
}

func TestApplyRestoresActiveChildOfAnArchivedCompany(t *testing.T) {
	snap := cascadeSnap()
	plan, problems := buildPartnerPlan(cascadeFile(false, true), snap, partnerOptions{EmptyKey: emptyKeyReject})
	require.Empty(t, problems)
	w := newFakeWriter()

	res := applyPartnerPlan(context.Background(), w, snap, plan)
	assert.Zero(t, res.Failed)
	assert.Equal(t, []string{"active 90=false", "active 91=true"}, w.calls)
	assert.Equal(t, 1, res.Archived)
	assert.Equal(t, 1, res.Restored)
}

func TestApplyRestoresChildCreatedUnderAnArchivedCompany(t *testing.T) {
	snap := emptySnap()
	c := targetPartner(90, "C", "", "vc", "", 0)
	c.IsCompany = true
	snap.Partners = []odoo.SyncPartner{c}
	plan, problems := buildPartnerPlan(cascadeFile(false, true), snap, partnerOptions{EmptyKey: emptyKeyReject})
	require.Empty(t, problems)
	w := newFakeWriter()

	res := applyPartnerPlan(context.Background(), w, snap, plan)
	assert.Zero(t, res.Failed)
	assert.Equal(t, []string{"create K=101", "active 90=false", "active 101=true"}, w.calls)
	assert.Equal(t, 1, res.Archived)
	assert.Equal(t, 1, res.Restored)
}

func TestApplyDoesNotRestoreInactiveChildOfAnArchivedCompany(t *testing.T) {
	snap := cascadeSnap()
	plan, problems := buildPartnerPlan(cascadeFile(false, false), snap, partnerOptions{EmptyKey: emptyKeyReject})
	require.Empty(t, problems)
	w := newFakeWriter()

	res := applyPartnerPlan(context.Background(), w, snap, plan)
	assert.Zero(t, res.Failed)
	assert.Equal(t, []string{"active 90=false", "active 91=false"}, w.calls)
	assert.Zero(t, res.Restored)
}

func TestApplyStopsWhenContextIsCancelledBeforeTheCall(t *testing.T) {
	snap := emptySnap()
	plan, problems := buildPartnerPlan(samplePartners(), snap, partnerOptions{EmptyKey: emptyKeyReject})
	require.Empty(t, problems)
	w := newFakeWriter()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	res := applyPartnerPlan(ctx, w, snap, plan)
	assert.Empty(t, w.calls)
	assert.Equal(t, 1, res.Failed)
	require.Len(t, res.Failures, 1)
	assert.Contains(t, res.Failures[0], "interrupted: context canceled")
	assert.Contains(t, res.Failures[0], "3 operations were not attempted")
}

func TestApplyStopsWhenContextIsCancelledAfterTheFirstCreate(t *testing.T) {
	snap := emptySnap()
	plan, problems := buildPartnerPlan(samplePartners(), snap, partnerOptions{EmptyKey: emptyKeyReject})
	require.Empty(t, problems)
	w := newFakeWriter()
	ctx, cancel := context.WithCancel(context.Background())
	w.afterCreate = cancel

	res := applyPartnerPlan(ctx, w, snap, plan)
	assert.Equal(t, 1, res.Created)
	assert.Len(t, w.calls, 1)
	assert.Equal(t, 1, res.Failed)
	require.Len(t, res.Failures, 1)
	assert.Contains(t, res.Failures[0], "interrupted: context canceled; 2 operations were not attempted")
}

func TestApplyFailedArchiveSkipsCascadeRestoresAndNamesThePartner(t *testing.T) {
	snap := cascadeSnap()
	plan, problems := buildPartnerPlan(cascadeFile(false, true), snap, partnerOptions{EmptyKey: emptyKeyReject})
	require.Empty(t, problems)
	w := newFakeWriter()
	w.failActive[90] = true

	res := applyPartnerPlan(context.Background(), w, snap, plan)
	assert.Equal(t, 1, res.Failed)
	assert.Zero(t, res.Archived)
	assert.Zero(t, res.Restored)
	assert.Empty(t, w.calls)
	require.Len(t, res.Failures, 1)
	assert.Contains(t, res.Failures[0], `partner 1 "C": archive target 90: active boom`)
}

func TestApplyFailedRestoreNamesThePartner(t *testing.T) {
	snap := cascadeSnap()
	plan, problems := buildPartnerPlan(cascadeFile(false, true), snap, partnerOptions{EmptyKey: emptyKeyReject})
	require.Empty(t, problems)
	w := newFakeWriter()
	w.failActive[91] = true

	res := applyPartnerPlan(context.Background(), w, snap, plan)
	assert.Equal(t, 1, res.Failed)
	require.Len(t, res.Failures, 1)
	assert.Contains(t, res.Failures[0], `partner 2 "K": restore target 91: active boom`)
}

func TestApplyFailedUpdateCountsFailedAndSkipsItsOwnRestoreAndArchive(t *testing.T) {
	f := &PartnersFile{Format: partnersFormat, Partners: []FilePartner{
		{SourceID: 1, Name: "New", Type: "contact", Active: true, VAT: "v1"},
		{SourceID: 2, Name: "Gone", Type: "contact", Active: false, VAT: "v2", Phone: "1"},
	}}
	snap := emptySnap()
	back := targetPartner(90, "Old", "", "v1", "", 0)
	back.Active = false
	snap.Partners = []odoo.SyncPartner{back, targetPartner(91, "Gone", "", "v2", "", 0)}
	plan, problems := buildPartnerPlan(f, snap, partnerOptions{EmptyKey: emptyKeyReject})
	require.Empty(t, problems)
	require.True(t, opFor(t, plan, 1).Restore)
	require.True(t, opFor(t, plan, 2).Archive)
	w := newFakeWriter()
	w.failUpdate[90], w.failUpdate[91] = true, true

	res := applyPartnerPlan(context.Background(), w, snap, plan)
	assert.Equal(t, 2, res.Failed)
	assert.Zero(t, res.Restored)
	assert.Zero(t, res.Archived)
	assert.Empty(t, w.calls)
	assert.Contains(t, res.Failures[0], `partner 1 "New": update target 90`)
}

func TestApplyRestoresGrandchildOfAnArchivedCompanyOnce(t *testing.T) {
	f := &PartnersFile{Format: partnersFormat, Partners: []FilePartner{
		{SourceID: 1, Name: "C", Type: "contact", Active: false, IsCompany: true, VAT: "vc"},
		{SourceID: 2, ParentSourceID: 1, Name: "K", Type: "contact", Active: true},
		{SourceID: 3, ParentSourceID: 2, Name: "G", Type: "contact", Active: true},
	}}
	snap := emptySnap()
	c := targetPartner(90, "C", "", "vc", "", 0)
	c.IsCompany = true
	k := targetPartner(91, "K", "", "", "", 90)
	k.Active = false // explicit restore, and also a cascade restore under C
	snap.Partners = []odoo.SyncPartner{c, k, targetPartner(92, "G", "", "", "", 91)}
	plan, problems := buildPartnerPlan(f, snap, partnerOptions{EmptyKey: emptyKeyReject})
	require.Empty(t, problems)
	require.True(t, opFor(t, plan, 2).Restore)
	w := newFakeWriter()

	res := applyPartnerPlan(context.Background(), w, snap, plan)
	assert.Zero(t, res.Failed)
	assert.Equal(t, 2, res.Restored)
	assert.Equal(t, []string{"active 90=false", "active 91=true", "active 92=true"}, w.calls)
}
