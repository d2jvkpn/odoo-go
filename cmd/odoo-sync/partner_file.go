package main

import (
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"fmt"
	"os"
	"strings"
)

// partnersFormat is the only partner file format this tool reads and writes.
const partnersFormat = 1

// PartnersFile is the partner export file. Source ids are records only and are never used to match.
type PartnersFile struct {
	Format     int               `json:"format"`
	ExportedAt string            `json:"exported_at"`
	Source     PartnerFileSource `json:"source"`
	Partners   []FilePartner     `json:"partners"`
}

// PartnerFileSource records where a partner export came from and its informational statistics.
type PartnerFileSource struct {
	Filename string              `json:"filename"`
	BaseURL  string              `json:"base_url"`
	Stats    *PartnerExportStats `json:"stats,omitempty"`
}

// FilePartner is one res.partner. Country, State and Categories are names, because ids differ per
// database. ParentSourceID is the source id of the parent partner, or zero for a top-level one.
type FilePartner struct {
	SourceID        int      `json:"source_id"`
	ParentSourceID  int      `json:"parent_source_id,omitzero"`
	Ref             string   `json:"ref,omitempty"`
	Name            string   `json:"name"`
	IsCompany       bool     `json:"is_company"`
	Type            string   `json:"type"`
	Active          bool     `json:"active"`
	Email           string   `json:"email,omitempty"`
	Phone           string   `json:"phone,omitempty"`
	Website         string   `json:"website,omitempty"`
	Street          string   `json:"street,omitempty"`
	Street2         string   `json:"street2,omitempty"`
	City            string   `json:"city,omitempty"`
	Zip             string   `json:"zip,omitempty"`
	Country         string   `json:"country,omitempty"`
	State           string   `json:"state,omitempty"`
	VAT             string   `json:"vat,omitempty"`
	CompanyRegistry string   `json:"company_registry,omitempty"`
	Function        string   `json:"function,omitempty"`
	Lang            string   `json:"lang,omitempty"`
	Comment         string   `json:"comment,omitempty"`
	Barcode         string   `json:"barcode,omitempty"`
	Categories      []string `json:"categories,omitempty"`
}

var validPartnerTypes = map[string]bool{"contact": true, "invoice": true, "delivery": true, "other": true}

func readPartnersFile(path string) (*PartnersFile, error) {
	var (
		data []byte
		f    PartnersFile
		err  error
	)

	if data, err = os.ReadFile(path); err != nil {
		return nil, fmt.Errorf("read partners file: %w", err)
	}
	if err = jsonv2.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse partners file %q: %w", path, err)
	}
	return &f, nil
}

func writePartnersFile(path string, f *PartnersFile) error {
	var (
		data []byte
		err  error
	)

	if data, err = jsonv2.Marshal(f, jsontext.WithIndent("  ")); err != nil {
		return fmt.Errorf("encode partners file: %w", err)
	}
	if err = os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write partners file: %w", err)
	}
	return nil
}

// partnerKey is the first non-empty matching key of a top-level partner, as "ref x", "vat x" or
// "email x" (lower case), or "" when it has none.
func partnerKey(p FilePartner) string {
	switch {
	case strings.TrimSpace(p.Ref) != "":
		return "ref " + strings.TrimSpace(p.Ref)
	case strings.TrimSpace(p.VAT) != "":
		return "vat " + strings.TrimSpace(p.VAT)
	case strings.TrimSpace(p.Email) != "":
		return "email " + strings.ToLower(strings.TrimSpace(p.Email))
	}
	return ""
}

// partnerDepths returns the nesting depth of every partner (zero for top level) and a problem for
// each parent that is missing from the file or forms a cycle. Partners with a problem have no entry.
func partnerDepths(partners []FilePartner) (map[int]int, []string) {
	var (
		byID     = make(map[int]FilePartner, len(partners))
		depth    = make(map[int]int, len(partners))
		problems []string
	)

	for _, p := range partners {
		byID[p.SourceID] = p
	}
	for _, p := range partners {
		var (
			seen = map[int]bool{p.SourceID: true}
			cur  = p
			d    int
			ok   = true
		)
		for cur.ParentSourceID != 0 {
			parent, found := byID[cur.ParentSourceID]
			if !found {
				problems = append(problems, fmt.Sprintf("partner %d %q: parent %d is not in the file",
					p.SourceID, p.Name, cur.ParentSourceID))
				ok = false
			} else if seen[parent.SourceID] {
				problems = append(problems, fmt.Sprintf("partner %d %q: parent cycle through partner %d",
					p.SourceID, p.Name, parent.SourceID))
				ok = false
			}
			if !ok {
				break
			}
			seen[parent.SourceID] = true
			cur = parent
			d++
		}
		if ok {
			depth[p.SourceID] = d
		}
	}
	return depth, problems
}

// Validate checks the file on its own, without Odoo, and returns every problem it finds.
func (f *PartnersFile) Validate() []string {
	var (
		problems []string
		ids      = make(map[int]bool)
		keys     = make(map[string]int)
		children = make(map[string]int)
	)

	if f.Format != partnersFormat {
		return []string{fmt.Sprintf("unsupported format %d: this tool reads format %d", f.Format, partnersFormat)}
	}
	for _, p := range f.Partners {
		label := fmt.Sprintf("partner %d %q", p.SourceID, p.Name)
		if ids[p.SourceID] {
			problems = append(problems, label+": source_id appears twice")
		}
		ids[p.SourceID] = true
		if strings.TrimSpace(p.Name) == "" {
			problems = append(problems, label+": name is empty")
		}
		if !validPartnerTypes[p.Type] {
			problems = append(problems, fmt.Sprintf("%s: type must be contact, invoice, delivery or other, got %q", label, p.Type))
		}
		if p.State != "" && p.Country == "" {
			problems = append(problems, label+": state without a country")
		}
		if p.ParentSourceID == 0 {
			if key := partnerKey(p); key != "" {
				if first, dup := keys[key]; dup {
					problems = append(problems, fmt.Sprintf("%s: key %s is also the key of partner %d", label, key, first))
				} else {
					keys[key] = p.SourceID
				}
			}
			continue
		}
		key := fmt.Sprintf("%d|%s|%s", p.ParentSourceID, p.Type, p.Name)
		if first, dup := children[key]; dup {
			problems = append(problems, fmt.Sprintf("%s: same parent, type and name as partner %d", label, first))
		} else {
			children[key] = p.SourceID
		}
	}
	_, parentProblems := partnerDepths(f.Partners)
	return append(problems, parentProblems...)
}
