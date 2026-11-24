package main

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

// PartnerExportStats is the structured form of the partner statistics printed by check.
type PartnerExportStats struct {
	Records int                 `json:"records"`
	Groups  []PartnerGroupStats `json:"groups"`
}

// PartnerGroupStats describes top-level partners or child partners of one type and their children.
type PartnerGroupStats struct {
	ParentType       string `json:"parent_type"`
	Records          int    `json:"records"`
	WithChildren     int    `json:"with_children"`
	IsCompanyTrue    int    `json:"is_company_true,omitzero"`
	IsCompanyFalse   int    `json:"is_company_false,omitzero"`
	NamesUnique      int    `json:"names_unique"`
	DuplicateNames   int    `json:"duplicate_names,omitzero"`
	DuplicateRecords int    `json:"duplicate_records,omitzero"`
	ChildContact     int    `json:"child_contact,omitzero"`
	ChildDelivery    int    `json:"child_delivery,omitzero"`
	ChildInvoice     int    `json:"child_invoice,omitzero"`
	ChildOther       int    `json:"child_other,omitzero"`
}

func calculatePartnerStats(f *PartnersFile) PartnerExportStats {
	var (
		groups              = make(map[string]map[string]int)
		counts              = make(map[string]int)
		companyCounts       = make(map[string]map[bool]int)
		byID                = make(map[int]FilePartner, len(f.Partners))
		childCounts         = make(map[int]int)
		childrenByGroup     = make(map[string]map[string]int)
		parentsWithChildren = make(map[string]int)
		stats               = PartnerExportStats{Records: len(f.Partners)}
	)

	for _, p := range f.Partners {
		byID[p.SourceID] = p
		group := "top_level"
		if p.ParentSourceID != 0 {
			group = p.Type
		}
		if groups[group] == nil {
			groups[group] = make(map[string]int)
			companyCounts[group] = make(map[bool]int)
		}
		groups[group][p.Name]++
		counts[group]++
		companyCounts[group][p.IsCompany]++
	}
	for _, p := range f.Partners {
		if p.ParentSourceID == 0 {
			continue
		}
		parent := byID[p.ParentSourceID]
		group := "top_level"
		if parent.ParentSourceID != 0 {
			group = parent.Type
		}
		if childCounts[parent.SourceID] == 0 {
			parentsWithChildren[group]++
		}
		childCounts[parent.SourceID]++
		if childrenByGroup[group] == nil {
			childrenByGroup[group] = make(map[string]int)
		}
		childrenByGroup[group][p.Type]++
	}
	for _, name := range []string{"top_level", "delivery", "contact", "invoice", "other"} {
		if name != "top_level" && counts[name] == 0 {
			continue
		}
		group := PartnerGroupStats{
			ParentType: name, Records: counts[name], WithChildren: parentsWithChildren[name],
			IsCompanyTrue: companyCounts[name][true], IsCompanyFalse: companyCounts[name][false],
			NamesUnique: len(groups[name]), ChildContact: childrenByGroup[name]["contact"],
			ChildDelivery: childrenByGroup[name]["delivery"], ChildInvoice: childrenByGroup[name]["invoice"],
			ChildOther: childrenByGroup[name]["other"],
		}
		for _, count := range groups[name] {
			if count > 1 {
				group.DuplicateNames++
				group.DuplicateRecords += count - 1
			}
		}
		stats.Groups = append(stats.Groups, group)
	}
	return stats
}

// printPartnerStats summarizes record structure, exact name reuse, and direct children by parent kind.
func printPartnerStats(out io.Writer, f *PartnersFile) {
	var (
		stats PartnerExportStats
		w     *tabwriter.Writer
		items []string
	)

	stats = calculatePartnerStats(f)
	fmt.Fprintf(out, "Partner statistics (%d records)\n\n", stats.Records)
	w = tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ParentType\tRecords\tWithChildren\tStats")
	for _, group := range stats.Groups {
		items = items[:0]
		if group.IsCompanyTrue > 0 {
			items = append(items, fmt.Sprintf("is_company=true:%d", group.IsCompanyTrue))
		}
		if group.IsCompanyFalse > 0 {
			items = append(items, fmt.Sprintf("is_company=false:%d", group.IsCompanyFalse))
		}
		if group.NamesUnique > 0 {
			items = append(items, fmt.Sprintf("names_unique=%d", group.NamesUnique))
		}
		if group.DuplicateNames > 0 {
			items = append(items, fmt.Sprintf("duplicate_names=%d", group.DuplicateNames))
		}
		if group.DuplicateRecords > 0 {
			items = append(items, fmt.Sprintf("duplicate_records=%d", group.DuplicateRecords))
		}
		for _, child := range []struct {
			name  string
			count int
		}{
			{"contact", group.ChildContact},
			{"delivery", group.ChildDelivery},
			{"invoice", group.ChildInvoice},
			{"other", group.ChildOther},
		} {
			if child.count > 0 {
				items = append(items, fmt.Sprintf("child_%s=%d", child.name, child.count))
			}
		}
		fmt.Fprintf(w, "%s\t%d\t%d\t%s\n",
			group.ParentType, group.Records, group.WithChildren, strings.Join(items, ", "))
	}
	w.Flush()
}
