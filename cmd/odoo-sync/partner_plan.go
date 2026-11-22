package main

import (
	"context"
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"

	"github.com/d2jvkpn/odoo-go"
)

// partnerSnapshotAPI is what loadPartnerSnapshot reads from the target Odoo.
type partnerSnapshotAPI interface {
	ListSyncPartners(ctx context.Context) ([]odoo.SyncPartner, error)
	ListPartnerTags(ctx context.Context) ([]odoo.PartnerTag, error)
	ListCountries(ctx context.Context) ([]odoo.Country, error)
	ListCountryStates(ctx context.Context, countryID int) ([]odoo.CountryState, error)
	ListActiveLanguages(ctx context.Context) ([]string, error)
}

// PartnerSnapshot is everything the planner needs to know about the target, as plain data.
type PartnerSnapshot struct {
	Partners  []odoo.SyncPartner     // active and archived
	Tags      map[string]int         // tag name -> res.partner.category ID
	TagNames  map[int]string         // tag ID -> name
	Countries map[string]int         // country name -> res.country ID
	States    map[int]map[string]int // country ID -> state name -> res.country.state ID
	Langs     map[string]bool        // installed language codes; nil means not loaded, so not checked
}

// loadPartnerSnapshot reads the target. It only reads. States are read only for the countries that
// the file gives a state for.
func loadPartnerSnapshot(ctx context.Context, api partnerSnapshotAPI, f *PartnersFile) (*PartnerSnapshot, error) {
	var (
		snap = &PartnerSnapshot{
			Tags: make(map[string]int), TagNames: make(map[int]string),
			Countries: make(map[string]int), States: make(map[int]map[string]int),
		}
		tags      []odoo.PartnerTag
		countries []odoo.Country
		states    []odoo.CountryState
		err       error
	)

	if snap.Partners, err = api.ListSyncPartners(ctx); err != nil {
		return nil, fmt.Errorf("read target partners: %w", err)
	}
	if tags, err = api.ListPartnerTags(ctx); err != nil {
		return nil, fmt.Errorf("read target tags: %w", err)
	}
	for _, t := range tags {
		if _, dup := snap.Tags[t.Name]; !dup {
			snap.Tags[t.Name] = t.ID
		}
		snap.TagNames[t.ID] = t.Name
	}
	if countries, err = api.ListCountries(ctx); err != nil {
		return nil, fmt.Errorf("read target countries: %w", err)
	}
	for _, c := range countries {
		snap.Countries[c.Name] = c.ID
	}
	langs, err := api.ListActiveLanguages(ctx)
	if err != nil {
		return nil, fmt.Errorf("read target languages: %w", err)
	}
	snap.Langs = make(map[string]bool, len(langs))
	for _, code := range langs {
		snap.Langs[code] = true
	}
	needed := make(map[int]bool)
	for _, p := range f.Partners {
		if id, ok := snap.Countries[p.Country]; ok && p.State != "" {
			needed[id] = true
		}
	}
	ids := make([]int, 0, len(needed))
	for id := range needed {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	for _, id := range ids {
		if states, err = api.ListCountryStates(ctx, id); err != nil {
			return nil, fmt.Errorf("read target states: %w", err)
		}
		snap.States[id] = make(map[string]int, len(states))
		for _, s := range states {
			snap.States[id][s.Name] = s.ID
		}
	}
	return snap, nil
}

const (
	emptyKeyReject = "reject"
	emptyKeySkip   = "skip"

	opCreate    = "create"
	opUpdate    = "update"
	opUnchanged = "unchanged"
	opSkip      = "skip"
)

// partnerOptions are the import options that change the plan.
type partnerOptions struct {
	EmptyKey     string // emptyKeyReject or emptyKeySkip
	NoCreateDeps bool   // fail instead of creating missing tags
}

// PartnerOp is what the importer does for one partner of the file.
type PartnerOp struct {
	Kind      string // opCreate, opUpdate, opUnchanged or opSkip
	Source    FilePartner
	TargetID  int    // the matched target partner; zero when creating or skipping
	Matched   string // "ref", "vat", "email" or "parent+type+name"
	CountryID int
	StateID   int
	Changed   []string // update: JSON names of the fields to write
	Restore   bool     // the target is archived and the file is not
	Archive   bool     // the file is archived and the target is not
	Reason    string   // skip: why
}

// PartnerPlan lists the operations, parents before children, and the tags to create first.
type PartnerPlan struct {
	Ops     []PartnerOp
	NewTags []string
}

type childKey struct {
	parent int
	typ    string
	name   string
}

type partnerIndex struct {
	ref, vat, email map[string][]*odoo.SyncPartner // top-level target partners only
	children        map[childKey][]*odoo.SyncPartner
}

func newPartnerIndex(partners []odoo.SyncPartner) *partnerIndex {
	ix := &partnerIndex{
		ref: map[string][]*odoo.SyncPartner{}, vat: map[string][]*odoo.SyncPartner{},
		email: map[string][]*odoo.SyncPartner{}, children: map[childKey][]*odoo.SyncPartner{},
	}
	for i := range partners {
		p := &partners[i]
		if p.Parent.ID != 0 {
			k := childKey{p.Parent.ID, p.Type, p.Name}
			ix.children[k] = append(ix.children[k], p)
			continue
		}
		if v := strings.TrimSpace(string(p.Ref)); v != "" {
			ix.ref[v] = append(ix.ref[v], p)
		}
		if v := strings.TrimSpace(string(p.VAT)); v != "" {
			ix.vat[v] = append(ix.vat[v], p)
		}
		if v := strings.ToLower(strings.TrimSpace(string(p.Email))); v != "" {
			ix.email[v] = append(ix.email[v], p)
		}
	}
	return ix
}

func joinIDs(ps []*odoo.SyncPartner) string {
	ids := make([]string, 0, len(ps))
	for _, p := range ps {
		ids = append(ids, fmt.Sprint(p.ID))
	}
	return strings.Join(ids, ", ")
}

type topMatch struct {
	partner  *odoo.SyncPartner
	by       string
	hasKey   bool
	conflict string
}

// matchTops matches the top-level partners of the file against the target in priority passes over
// the whole file: first every partner with a ref, then the unmatched ones by vat and then by email,
// each time among the target partners that no earlier pass claimed. A key that is empty in the
// file is skipped. The first key that matches exactly one target partner wins; a key that matches
// several is a conflict. Two file partners may still match the same target within one pass; the
// plan reports that as a double match.
func matchTops(partners []FilePartner, ix *partnerIndex) map[int]topMatch {
	var (
		out     = make(map[int]topMatch)
		claimed = make(map[int]bool) // target ids matched in earlier passes
	)

	keys := []struct {
		name  string
		value func(FilePartner) string
		index map[string][]*odoo.SyncPartner
	}{
		{"ref", func(p FilePartner) string { return strings.TrimSpace(p.Ref) }, ix.ref},
		{"vat", func(p FilePartner) string { return strings.TrimSpace(p.VAT) }, ix.vat},
		{"email", func(p FilePartner) string { return strings.ToLower(strings.TrimSpace(p.Email)) }, ix.email},
	}
	for _, p := range partners {
		if p.ParentSourceID != 0 {
			continue
		}
		var m topMatch
		for _, k := range keys {
			m.hasKey = m.hasKey || k.value(p) != ""
		}
		out[p.SourceID] = m
	}
	for _, k := range keys {
		matched := make(map[int]bool)
		for _, p := range partners {
			m, top := out[p.SourceID]
			if !top || m.partner != nil || m.conflict != "" || k.value(p) == "" {
				continue
			}
			var found []*odoo.SyncPartner
			for _, t := range k.index[k.value(p)] {
				if !claimed[t.ID] {
					found = append(found, t)
				}
			}
			switch len(found) {
			case 0:
			case 1:
				m.partner, m.by = found[0], k.name
				matched[found[0].ID] = true
			default:
				m.conflict = fmt.Sprintf("%s %q matches %d target partners (ids %s)", k.name, k.value(p), len(found), joinIDs(found))
			}
			out[p.SourceID] = m
		}
		for id := range matched {
			claimed[id] = true
		}
	}
	return out
}

// diffPartner returns the JSON names of the fields whose value in the file differs from the target.
func diffPartner(want FilePartner, countryID, stateID int, have odoo.SyncPartner, haveTags []string) []string {
	var changed []string

	add := func(name string, differs bool) {
		if differs {
			changed = append(changed, name)
		}
	}
	add("name", have.Name != want.Name)
	add("ref", string(have.Ref) != want.Ref)
	add("email", string(have.Email) != want.Email)
	add("phone", string(have.Phone) != want.Phone)
	add("website", string(have.Website) != want.Website)
	add("street", string(have.Street) != want.Street)
	add("street2", string(have.Street2) != want.Street2)
	add("city", string(have.City) != want.City)
	add("zip", string(have.Zip) != want.Zip)
	add("vat", string(have.VAT) != want.VAT)
	add("company_registry", string(have.CompanyRegistry) != want.CompanyRegistry)
	add("function", string(have.Function) != want.Function)
	add("lang", want.Lang != "" && string(have.Lang) != want.Lang) // Odoo fills in a default lang
	add("comment", string(have.Comment) != want.Comment)
	add("barcode", string(have.Barcode) != want.Barcode)
	add("type", have.Type != want.Type)
	add("is_company", have.IsCompany != want.IsCompany)
	add("country_id", have.Country.ID != countryID)
	add("state_id", have.State.ID != stateID)
	wantTags := slices.Compact(slices.Sorted(slices.Values(want.Categories)))
	add("category_id", !slices.Equal(slices.Compact(slices.Sorted(slices.Values(haveTags))), wantTags))
	return changed
}

// buildPartnerPlan plans the whole import. Any returned problem means nothing may be written.
func buildPartnerPlan(f *PartnersFile, snap *PartnerSnapshot, opt partnerOptions) (*PartnerPlan, []string) {
	var (
		plan    = &PartnerPlan{}
		ix      = newPartnerIndex(snap.Partners)
		claimed = make(map[int]int)  // target id -> source id
		target  = make(map[int]int)  // source id -> matched target id
		skipped = make(map[int]bool) // source ids
		newTag  = make(map[string]bool)
		order   = slices.Clone(f.Partners)
	)

	depth, problems := partnerDepths(f.Partners)
	if len(problems) > 0 {
		return nil, problems
	}
	tops := matchTops(f.Partners, ix)
	sort.SliceStable(order, func(i, j int) bool {
		if depth[order[i].SourceID] != depth[order[j].SourceID] {
			return depth[order[i].SourceID] < depth[order[j].SourceID]
		}
		return order[i].SourceID < order[j].SourceID
	})
	for _, p := range order {
		var (
			label = fmt.Sprintf("partner %d %q", p.SourceID, p.Name)
			op    = PartnerOp{Source: p}
			have  *odoo.SyncPartner
		)

		if p.ParentSourceID != 0 && skipped[p.ParentSourceID] {
			op.Kind, op.Reason = opSkip, fmt.Sprintf("its parent %d is skipped", p.ParentSourceID)
			skipped[p.SourceID] = true
			plan.Ops = append(plan.Ops, op)
			continue
		}
		var m topMatch
		if p.ParentSourceID == 0 {
			m = tops[p.SourceID]
			if m.conflict == "" && !m.hasKey && opt.EmptyKey == emptyKeySkip {
				op.Kind, op.Reason = opSkip, "it has no ref, vat or email"
				skipped[p.SourceID] = true
				plan.Ops = append(plan.Ops, op)
				continue
			}
		}
		if p.Country != "" {
			id, ok := snap.Countries[p.Country]
			if !ok {
				problems = append(problems, fmt.Sprintf("%s: country %q does not exist in the target", label, p.Country))
			}
			op.CountryID = id
		}
		if p.State != "" && op.CountryID != 0 {
			id, ok := snap.States[op.CountryID][p.State]
			if !ok {
				problems = append(problems, fmt.Sprintf("%s: state %q does not exist in country %q in the target", label, p.State, p.Country))
			}
			op.StateID = id
		}
		if p.Lang != "" && snap.Langs != nil && !snap.Langs[p.Lang] {
			problems = append(problems, fmt.Sprintf("%s: language %q is not installed in the target", label, p.Lang))
		}
		for _, name := range p.Categories {
			if _, ok := snap.Tags[name]; ok || newTag[name] {
				continue
			}
			if opt.NoCreateDeps {
				problems = append(problems, fmt.Sprintf("%s: tag %q does not exist in the target", label, name))
				newTag[name] = true // report each missing tag once
				continue
			}
			newTag[name] = true
			plan.NewTags = append(plan.NewTags, name)
		}
		if p.ParentSourceID == 0 {
			switch {
			case m.conflict != "":
				problems = append(problems, fmt.Sprintf("%s: %s", label, m.conflict))
				continue
			case !m.hasKey:
				problems = append(problems, fmt.Sprintf("%s: it has no ref, vat or email, so it cannot be matched (use -empty-key skip to skip it)", label))
				continue
			}
			have, op.Matched = m.partner, m.by
		} else if parent := target[p.ParentSourceID]; parent != 0 {
			found := ix.children[childKey{parent, p.Type, p.Name}]
			if len(found) > 1 {
				problems = append(problems, fmt.Sprintf("%s: parent, type and name match %d target partners (ids %s)", label, len(found), joinIDs(found)))
				continue
			}
			if len(found) == 1 {
				have, op.Matched = found[0], "parent+type+name"
			}
		}
		if have == nil {
			op.Kind = opCreate
			op.Archive = !p.Active
			plan.Ops = append(plan.Ops, op)
			continue
		}
		if first, dup := claimed[have.ID]; dup {
			problems = append(problems, fmt.Sprintf("%s: partners %d and %d both match target partner %d", label, first, p.SourceID, have.ID))
			continue
		}
		claimed[have.ID], target[p.SourceID], op.TargetID = p.SourceID, have.ID, have.ID
		haveTags := make([]string, 0, len(have.CategoryIDs))
		for _, id := range have.CategoryIDs {
			name, ok := snap.TagNames[id]
			if !ok {
				name = fmt.Sprintf("#%d", id)
			}
			haveTags = append(haveTags, name)
		}
		op.Changed = diffPartner(p, op.CountryID, op.StateID, *have, haveTags)
		op.Archive, op.Restore = !p.Active && have.Active, p.Active && !have.Active
		op.Kind = opUnchanged
		if len(op.Changed) > 0 || op.Archive || op.Restore {
			op.Kind = opUpdate
		}
		plan.Ops = append(plan.Ops, op)
	}
	if len(problems) > 0 {
		return nil, problems
	}
	return plan, nil
}

// printPartnerPlan prints what the import would do and a summary line.
func printPartnerPlan(out io.Writer, plan *PartnerPlan) {
	var create, update, unchanged, skip, archive, restore int

	for _, name := range plan.NewTags {
		fmt.Fprintf(out, "create tag %q\n", name)
	}
	for _, op := range plan.Ops {
		label := fmt.Sprintf("partner %d %q", op.Source.SourceID, op.Source.Name)
		switch op.Kind {
		case opCreate:
			create++
			fmt.Fprintf(out, "create %s\n", label)
		case opUpdate:
			update++
			fmt.Fprintf(out, "update %s (target %d, matched by %s): %s\n", label, op.TargetID, op.Matched, strings.Join(op.Changed, ", "))
		case opUnchanged:
			unchanged++
		case opSkip:
			skip++
			fmt.Fprintf(out, "skip %s: %s\n", label, op.Reason)
		}
		if op.Archive {
			archive++
			fmt.Fprintf(out, "archive %s\n", label)
		}
		if op.Restore {
			restore++
			fmt.Fprintf(out, "restore %s\n", label)
		}
	}
	fmt.Fprintf(out, "summary: %d to create, %d to update, %d unchanged, %d skipped, %d to archive, %d to restore\n",
		create, update, unchanged, skip, archive, restore)
}
