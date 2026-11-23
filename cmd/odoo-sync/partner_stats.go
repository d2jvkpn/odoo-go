package main

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

// printPartnerStats summarizes record structure, exact name reuse, and direct children by parent kind.
func printPartnerStats(out io.Writer, f *PartnersFile) {
	var (
		groups              = make(map[string]map[string]int)
		counts              = make(map[string]int)
		companyCounts       = make(map[string]map[bool]int)
		byID                = make(map[int]FilePartner, len(f.Partners))
		childCounts         = make(map[int]int)
		childrenByGroup     = make(map[string]map[string]int)
		parentsWithChildren = make(map[string]int)
		children            int
	)

	for _, p := range f.Partners {
		byID[p.SourceID] = p
		group := "top_level"
		if p.ParentSourceID != 0 {
			group = p.Type
			children++
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
	fmt.Fprintf(out, "Partner statistics (%d records)\n\n", len(f.Partners))
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ParentType\tRecords\tWithChildren\tStats")
	for _, group := range []string{"top_level", "delivery", "contact", "invoice", "other"} {
		if group != "top_level" && counts[group] == 0 {
			continue
		}
		var duplicateNames, duplicateRecords int
		for _, n := range groups[group] {
			if n > 1 {
				duplicateNames++
				duplicateRecords += n - 1
			}
		}
		types := childrenByGroup[group]
		stats := make([]string, 0, 6)
		if n := companyCounts[group][true]; n > 0 {
			stats = append(stats, fmt.Sprintf("is_company=true:%d", n))
		}
		if n := companyCounts[group][false]; n > 0 {
			stats = append(stats, fmt.Sprintf("is_company=false:%d", n))
		}
		if n := len(groups[group]); n > 0 {
			stats = append(stats, fmt.Sprintf("names_unique=%d", n))
		}
		if duplicateNames > 0 {
			stats = append(stats, fmt.Sprintf("duplicate_names=%d", duplicateNames))
		}
		if duplicateRecords > 0 {
			stats = append(stats, fmt.Sprintf("duplicate_records=%d", duplicateRecords))
		}
		for _, childType := range []string{"contact", "delivery", "invoice", "other"} {
			if n := types[childType]; n > 0 {
				stats = append(stats, fmt.Sprintf("child_%s=%d", childType, n))
			}
		}
		fmt.Fprintf(w, "%s\t%d\t%d\t%s\n",
			group, counts[group], parentsWithChildren[group], strings.Join(stats, ", "))
	}
	w.Flush()
}
