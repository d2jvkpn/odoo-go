package main

import (
	"fmt"
	"math"
)

// planUnits checks the conversion definitions for units used by this import and orders new
// units so each reference unit is created before its children. Old files without units retain
// the previous behavior: their missing units are reported by the product and BOM planners.
func planUnits(f *CatalogFile, snap *Snapshot, opt importOptions) ([]FileUoM, []string) {
	var (
		out      []FileUoM
		problems []string
		byName   = make(map[string]FileUoM, len(f.Units))
		needed   = make(map[string]bool)
		state    = make(map[string]int)
	)
	for _, u := range f.Units {
		byName[u.Name] = u
	}
	if !opt.NoProducts {
		for _, t := range f.Templates {
			if opt.SaleOK == saleOKOnly && !t.SaleOK {
				continue
			}
			if opt.EmptyCode == emptyCodeSkip && len(uncodedVariants(t)) > 0 {
				continue
			}
			needed[t.UoM] = true
		}
	}
	if opt.BOMs {
		for _, b := range f.BOMs {
			needed[b.UoM] = true
			for _, l := range b.Lines {
				needed[l.UoM] = true
			}
		}
	}
	var visit func(string)
	visit = func(name string) {
		if state[name] == 2 || state[name] == 1 {
			return // Validate reports cycles before planning.
		}
		u, exists := byName[name]
		if !exists {
			return // The ordinary missing-unit check reports an old file's missing unit.
		}
		state[name] = 1
		if u.RelativeTo != "" {
			visit(u.RelativeTo)
		}
		if snap.DuplicateUoMs[name] {
			problems = append(problems, fmt.Sprintf("unit %q matches multiple target units", name))
		} else if existing, found := snap.UnitDetails[name]; found {
			if !existing.Active {
				problems = append(problems, fmt.Sprintf("unit %q is archived in the target", name))
			} else if existing.RelativeTo.Name != u.RelativeTo ||
				math.Abs(existing.RelativeFactor-u.RelativeFactor) > 1e-9*math.Max(1, u.RelativeFactor) {
				problems = append(problems, fmt.Sprintf("unit %q has a different reference or factor in the target", name))
			}
		} else if snap.UoMs[name] != 0 {
			problems = append(problems, fmt.Sprintf("unit %q has no conversion details in the target", name))
		} else if opt.NoCreateDeps {
			problems = append(problems, fmt.Sprintf("unit %q does not exist in the target", name))
		} else {
			out = append(out, u)
		}
		state[name] = 2
	}
	for _, name := range sortedKeys(needed) {
		visit(name)
	}
	return out, problems
}

func plannedUnits(snap *Snapshot, units []FileUoM) *Snapshot {
	copy := *snap
	copy.UoMs = make(map[string]int, len(snap.UoMs)+len(units))
	for name, id := range snap.UoMs {
		copy.UoMs[name] = id
	}
	for _, u := range units {
		copy.UoMs[u.Name] = -1
	}
	return &copy
}
