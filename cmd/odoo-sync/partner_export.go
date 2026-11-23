package main

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/d2jvkpn/odoo-go"
)

// partnerSource is what the partner exporter reads from an Odoo database.
type partnerSource interface {
	ListSyncPartners(ctx context.Context) ([]odoo.SyncPartner, error)
	ListPartnerTags(ctx context.Context) ([]odoo.PartnerTag, error)
	ListStatesByIDs(ctx context.Context, ids []int) ([]odoo.StateInfo, error)
}

// buildPartnerExport reads every partner, archived ones included, and returns the file sorted by
// source id, plus warnings. A state whose country is not the partner's country, and a tag that is
// archived, are left out with a warning so the file always validates. With inferCountryFromState,
// a missing partner country is filled in the file from the state record's country.
func buildPartnerExport(
	ctx context.Context,
	src partnerSource,
	name, baseURL string,
	now time.Time,
	inferCountryFromState bool,
) (*PartnersFile, []string, error) {
	var (
		partners []odoo.SyncPartner
		tags     []odoo.PartnerTag
		states   []odoo.StateInfo
		stateIDs []int
		tagNames = make(map[int]string)
		stateOf  = make(map[int]odoo.StateInfo)
		inFile   = make(map[int]bool)
		warnings []string
		out      *PartnersFile
		err      error
	)

	if partners, err = src.ListSyncPartners(ctx); err != nil {
		return nil, nil, err
	}
	if tags, err = src.ListPartnerTags(ctx); err != nil {
		return nil, nil, err
	}
	for _, t := range tags {
		tagNames[t.ID] = t.Name
	}
	for _, p := range partners {
		inFile[p.ID] = true
		if p.State.ID != 0 {
			stateIDs = append(stateIDs, p.State.ID)
		}
	}
	sort.Ints(stateIDs)
	if states, err = src.ListStatesByIDs(ctx, stateIDs); err != nil {
		return nil, nil, err
	}
	for _, s := range states {
		stateOf[s.ID] = s
	}
	sort.Slice(partners, func(i, j int) bool { return partners[i].ID < partners[j].ID })
	out = &PartnersFile{
		Format: partnersFormat, ExportedAt: now.UTC().Format(time.RFC3339),
		Source: FileSource{Name: name, BaseURL: baseURL},
	}
	for _, p := range partners {
		if strings.TrimSpace(p.Name) == "" {
			return nil, nil, fmt.Errorf("partner %d has no name; give it one in the source", p.ID)
		}
		if p.Parent.ID != 0 && !inFile[p.Parent.ID] {
			return nil, nil, fmt.Errorf("partner %d %q: parent %d is not in the export", p.ID, p.Name, p.Parent.ID)
		}
		fp := FilePartner{
			SourceID: p.ID, ParentSourceID: p.Parent.ID, Ref: string(p.Ref), Name: p.Name,
			IsCompany: p.IsCompany, Type: p.Type, Active: p.Active, Email: string(p.Email),
			Phone: string(p.Phone), Website: string(p.Website), Street: string(p.Street),
			Street2: string(p.Street2), City: string(p.City), Zip: string(p.Zip), Country: p.Country.Name,
			VAT: string(p.VAT), CompanyRegistry: string(p.CompanyRegistry), Function: string(p.Function),
			Lang: string(p.Lang), Comment: string(p.Comment), Barcode: string(p.Barcode),
		}
		if p.State.ID != 0 {
			switch s, ok := stateOf[p.State.ID]; {
			case !ok:
				warnings = append(warnings, fmt.Sprintf("partner %d %q: state %d was not found and is not exported", p.ID, p.Name, p.State.ID))
			case s.Country.ID == 0 || s.Country.Name == "":
				warnings = append(warnings, fmt.Sprintf("partner %d %q: state %q has no country and is not exported", p.ID, p.Name, s.Name))
			case p.Country.ID == 0 && inferCountryFromState:
				fp.Country, fp.State = s.Country.Name, s.Name
				warnings = append(warnings, fmt.Sprintf(
					"partner %d %q: inferred country %q from state %q in the export",
					p.ID, p.Name, s.Country.Name, s.Name,
				))
			case s.Country.ID != p.Country.ID:
				warnings = append(warnings, fmt.Sprintf("partner %d %q: state %q belongs to country %q, not to the partner's country %q, and is not exported",
					p.ID, p.Name, s.Name, s.Country.Name, p.Country.Name))
			default:
				fp.State = s.Name
			}
		}
		for _, id := range p.CategoryIDs {
			if tag, ok := tagNames[id]; ok {
				fp.Categories = append(fp.Categories, tag)
			} else {
				warnings = append(warnings, fmt.Sprintf("partner %d %q: tag %d is archived or unknown and is not exported", p.ID, p.Name, id))
			}
		}
		sort.Strings(fp.Categories)
		fp.Categories = slices.Compact(fp.Categories) // two tags may share a name under different parents
		out.Partners = append(out.Partners, fp)
	}
	return out, warnings, nil
}
