package main

import "fmt"

// catalogPolicyProblems finds import blockers that only depend on the file and import options.
// Target-dependent checks remain in buildPlan.
func catalogPolicyProblems(f *CatalogFile, section string, opt importOptions) []string {
	var problems []string
	if opt.EmptyCode == emptyCodeSkip {
		return nil
	}
	if section == "products" {
		for _, t := range f.Templates {
			if opt.SaleOK == saleOKOnly && !t.SaleOK {
				continue
			}
			for _, id := range uncodedVariants(t) {
				problems = append(problems, fmt.Sprintf("template %d %q: variant %d has no default_code", t.SourceID, t.Name, id))
			}
		}
		return problems
	}
	for _, b := range f.BOMs {
		if !b.Active {
			continue
		}
		label := fmt.Sprintf("bom %d", b.SourceID)
		codes, _ := bomProductCodes(&b)
		if len(codes) == 0 {
			problems = append(problems, label+": product has no default_code")
		} else {
			for _, code := range codes {
				if code == "" {
					problems = append(problems, label+": product has no default_code")
					break
				}
			}
		}
		for _, line := range b.Lines {
			if line.Component == "" {
				problems = append(problems, label+": component has no default_code")
				break
			}
		}
	}
	return problems
}

func partnerPolicyProblems(f *PartnersFile, policy string) []string {
	var problems []string
	if policy == emptyKeySkip {
		return nil
	}
	for _, p := range f.Partners {
		if p.ParentSourceID == 0 && partnerKey(p) == "" {
			problems = append(problems, fmt.Sprintf(
				"partner %d %q: it has no ref, vat or email, so it cannot be matched (use -empty-key skip to skip it)",
				p.SourceID, p.Name,
			))
		}
	}
	return problems
}
