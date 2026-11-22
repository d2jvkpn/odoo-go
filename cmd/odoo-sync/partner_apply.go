package main

import (
	"context"
	"fmt"
	"maps"

	"github.com/d2jvkpn/odoo-go"
)

// partnerWriter is what the importer writes to the target Odoo. None of these calls retries.
type partnerWriter interface {
	CreatePartner(ctx context.Context, input odoo.CreatePartnerRequest) (int, error)
	UpdatePartner(ctx context.Context, id int, patch odoo.UpdatePartnerRequest) error
	SetPartnerActive(ctx context.Context, id int, active bool) error
	CreatePartnerTag(ctx context.Context, name string) (int, error)
}

// PartnerResult counts what an import did. Failures name each partner that could not be written.
type PartnerResult struct {
	Created, Updated, Unchanged, Skipped, Archived, Restored, Failed int
	Failures, Warnings                                               []string
}

func (r *PartnerResult) fail(format string, args ...any) {
	r.Failed++
	r.Failures = append(r.Failures, fmt.Sprintf(format, args...))
}

func tagIDsFor(names []string, ids map[string]int) ([]int, error) {
	out := make([]int, 0, len(names))
	for _, name := range names {
		id, ok := ids[name]
		if !ok {
			return nil, fmt.Errorf("tag %q was not created", name)
		}
		out = append(out, id)
	}
	return out, nil
}

func createRequest(op PartnerOp, parentID int, tagIDs []int) odoo.CreatePartnerRequest {
	p := op.Source
	return odoo.CreatePartnerRequest{
		Name: p.Name, Email: p.Email, Phone: p.Phone, Ref: p.Ref, Street: p.Street, Street2: p.Street2,
		City: p.City, Zip: p.Zip, StateID: op.StateID, CountryID: op.CountryID, Website: p.Website,
		IsCompany: p.IsCompany, Type: p.Type, ParentID: parentID, Function: p.Function, VAT: p.VAT,
		CompanyRegistry: p.CompanyRegistry, Lang: p.Lang, Comment: p.Comment,
		CategoryIDs: odoo.ReplaceIDs(tagIDs), Barcode: p.Barcode,
	}
}

// patchFor writes the changed fields of the op, taking the values from the file.
func patchFor(op PartnerOp, tagIDs []int) odoo.UpdatePartnerRequest {
	var (
		p     = op.Source
		patch odoo.UpdatePartnerRequest
	)

	for _, name := range op.Changed {
		switch name {
		case "name":
			patch.Name = &p.Name
		case "ref":
			patch.Ref = &p.Ref
		case "email":
			patch.Email = &p.Email
		case "phone":
			patch.Phone = &p.Phone
		case "website":
			patch.Website = &p.Website
		case "street":
			patch.Street = &p.Street
		case "street2":
			patch.Street2 = &p.Street2
		case "city":
			patch.City = &p.City
		case "zip":
			patch.Zip = &p.Zip
		case "vat":
			patch.VAT = &p.VAT
		case "company_registry":
			patch.CompanyRegistry = &p.CompanyRegistry
		case "function":
			patch.Function = &p.Function
		case "lang":
			patch.Lang = &p.Lang
		case "comment":
			patch.Comment = &p.Comment
		case "barcode":
			patch.Barcode = &p.Barcode
		case "type":
			patch.Type = &p.Type
		case "is_company":
			patch.IsCompany = &p.IsCompany
		case "country_id":
			patch.CountryID = &op.CountryID
		case "state_id":
			patch.StateID = &op.StateID
		case "category_id":
			ids := odoo.ReplaceIDs(tagIDs)
			patch.CategoryIDs = &ids
		}
	}
	return patch
}

// archiveItem is a queued activation change: the target id, and the source partner it is for.
type archiveItem struct {
	id, source int
	label      string
}

// applyPartnerPlan writes the plan: missing tags, then partners parents first, then archives, then
// restores, including active descendants of an archived partner, which Odoo archives too. A failure
// is recorded and the run continues; a partner whose parent was not written is reported as failed
// too. Nothing is retried.
func applyPartnerPlan(ctx context.Context, w partnerWriter, snap *PartnerSnapshot, plan *PartnerPlan) *PartnerResult {
	var (
		res     = &PartnerResult{}
		tagIDs  = maps.Clone(snap.Tags)
		targets = make(map[int]int) // source id -> target id, for matched and created partners
		restore []archiveItem
		archive []archiveItem
	)

	// interrupted records one failure when ctx is done and reports whether it was; n counts the
	// operations that will not be attempted.
	interrupted := func(n int) bool {
		if ctx.Err() == nil {
			return false
		}
		res.fail("interrupted: %v; %d operations were not attempted", ctx.Err(), n)
		return true
	}
	writes := 0 // partner operations that write
	for _, op := range plan.Ops {
		if op.Kind != opSkip {
			writes++
		}
	}
	for i, name := range plan.NewTags {
		if interrupted(len(plan.NewTags) - i + writes) {
			return res
		}
		id, err := w.CreatePartnerTag(ctx, name)
		if err != nil {
			res.fail("create tag %q: %v", name, err)
			continue
		}
		tagIDs[name] = id
	}
	for _, op := range plan.Ops {
		if op.TargetID != 0 {
			targets[op.Source.SourceID] = op.TargetID
		}
	}
	for _, op := range plan.Ops {
		label := fmt.Sprintf("partner %d %q", op.Source.SourceID, op.Source.Name)
		if op.Kind == opSkip {
			res.Skipped++
			res.Warnings = append(res.Warnings, label+": skipped: "+op.Reason)
			continue
		}
		if interrupted(writes + len(archive) + len(restore)) {
			return res
		}
		writes--
		parentID := 0
		if op.Source.ParentSourceID != 0 {
			if parentID = targets[op.Source.ParentSourceID]; parentID == 0 {
				res.fail("%s: skipped because its parent %d was not written", label, op.Source.ParentSourceID)
				continue
			}
		}
		id := op.TargetID
		switch op.Kind {
		case opCreate:
			ids, err := tagIDsFor(op.Source.Categories, tagIDs)
			if err != nil { // no request was sent
				res.fail("%s: create: %v", label, err)
				continue
			}
			if id, err = w.CreatePartner(ctx, createRequest(op, parentID, ids)); err != nil {
				res.fail("%s: create: %v (the outcome may be unknown; search the target by its ref, vat or email before running again)", label, err)
				continue
			}
			targets[op.Source.SourceID] = id
			res.Created++
		case opUpdate:
			if len(op.Changed) > 0 {
				ids, err := tagIDsFor(op.Source.Categories, tagIDs)
				if err == nil {
					err = w.UpdatePartner(ctx, id, patchFor(op, ids))
				}
				if err != nil {
					res.fail("%s: update target %d: %v", label, id, err)
					continue
				}
				res.Updated++
			}
		case opUnchanged:
			res.Unchanged++
		}
		if op.Restore {
			restore = append(restore, archiveItem{id: id, source: op.Source.SourceID, label: label})
		}
		if op.Archive {
			archive = append(archive, archiveItem{id: id, source: op.Source.SourceID, label: label})
		}
	}
	// Archive first: Odoo archives the child contacts of an archived company, so the restores
	// that follow also bring back every active descendant of a partner archived in this run.
	archived := make(map[int]bool) // source ids archived in this run
	for i, a := range archive {
		if interrupted(len(archive) - i + len(restore)) {
			return res
		}
		if err := w.SetPartnerActive(ctx, a.id, false); err != nil {
			res.fail("%s: archive target %d: %v", a.label, a.id, err)
			continue
		}
		archived[a.source] = true
		res.Archived++
	}

	parents := make(map[int]int, len(plan.Ops))
	for _, op := range plan.Ops {
		parents[op.Source.SourceID] = op.Source.ParentSourceID
	}
	queued := make(map[int]bool, len(restore))
	for _, r := range restore {
		queued[r.id] = true
	}
	for _, op := range plan.Ops {
		id := targets[op.Source.SourceID]
		if op.Kind == opSkip || !op.Source.Active || id == 0 || queued[id] {
			continue
		}
		for anc := parents[op.Source.SourceID]; anc != 0; anc = parents[anc] {
			if archived[anc] {
				queued[id] = true
				restore = append(restore, archiveItem{id: id, source: op.Source.SourceID,
					label: fmt.Sprintf("partner %d %q", op.Source.SourceID, op.Source.Name)})
				break
			}
		}
	}
	for i, r := range restore {
		if interrupted(len(restore) - i) {
			return res
		}
		if err := w.SetPartnerActive(ctx, r.id, true); err != nil {
			res.fail("%s: restore target %d: %v", r.label, r.id, err)
			continue
		}
		res.Restored++
	}
	return res
}
