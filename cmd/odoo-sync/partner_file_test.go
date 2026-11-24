package main

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func samplePartners() *PartnersFile {
	return &PartnersFile{
		Format: partnersFormat, ExportedAt: "2026-11-22T00:00:00Z",
		Source: PartnerFileSource{Filename: "src", BaseURL: "http://src"},
		Partners: []FilePartner{
			{SourceID: 10, Ref: "user-42", Name: "Alice Co.", IsCompany: true, Type: "contact", Active: true,
				Email: "a@x.com", Country: "Norway", State: "Oslo", Categories: []string{"VIP"}},
			{SourceID: 11, ParentSourceID: 10, Name: "Warehouse", Type: "delivery", Active: true},
			{SourceID: 12, Name: "Bob Ltd.", IsCompany: true, Type: "contact", Active: true, VAT: "NO123"},
		},
	}
}

func TestPartnersFileRoundTripAndValidate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "partners.json")
	require.NoError(t, writePartnersFile(path, samplePartners()))
	got, err := readPartnersFile(path)
	require.NoError(t, err)
	assert.Equal(t, samplePartners(), got)
	assert.Empty(t, got.Validate())
}

func TestPartnersFileValidateReportsEveryProblem(t *testing.T) {
	f := samplePartners()
	f.Partners = append(f.Partners,
		FilePartner{SourceID: 10, Name: "Dup id", Type: "contact", Active: true},
		FilePartner{SourceID: 13, Name: " ", Type: "contact", Active: true, Ref: "r13"},
		FilePartner{SourceID: 14, Name: "Bad type", Type: "home", Active: true, Ref: "r14"},
		FilePartner{SourceID: 15, Name: "State only", Type: "contact", Active: true, Ref: "r15", State: "Oslo"},
		FilePartner{SourceID: 16, Name: "Same email", Type: "contact", Active: true, Email: "B@X.com"},
		FilePartner{SourceID: 17, Name: "Same email 2", Type: "contact", Active: true, Email: "b@x.com"},
		FilePartner{SourceID: 18, ParentSourceID: 99, Name: "Orphan", Type: "other", Active: true},
		FilePartner{SourceID: 19, ParentSourceID: 10, Name: "Warehouse", Type: "delivery", Active: true},
		FilePartner{SourceID: 20, ParentSourceID: 21, Name: "Cycle a", Type: "other", Active: true},
		FilePartner{SourceID: 21, ParentSourceID: 20, Name: "Cycle b", Type: "other", Active: true},
	)
	problems := f.Validate()
	joined := ""
	for _, p := range problems {
		joined += p + "\n"
	}
	for _, want := range []string{
		"source_id appears twice", "name is empty", "type must be",
		"state without a country", `email b@x.com`, "parent 99 is not in the file",
		"same parent, type and name", "parent cycle",
	} {
		assert.Contains(t, joined, want)
	}
}

func TestPartnersFileValidateRejectsUnknownFormat(t *testing.T) {
	f := samplePartners()
	f.Format = 2
	assert.Equal(t, []string{"unsupported format 2: this tool reads format 1"}, f.Validate())
}

func TestPartnerKeyOrder(t *testing.T) {
	assert.Equal(t, "ref r", partnerKey(FilePartner{Ref: " r ", VAT: "v", Email: "e"}))
	assert.Equal(t, "vat v", partnerKey(FilePartner{VAT: "v", Email: "e"}))
	assert.Equal(t, "email e@x.com", partnerKey(FilePartner{Email: " E@x.com "}))
	assert.Equal(t, "", partnerKey(FilePartner{}))
}

func TestPartnerDepthsOrdersParentsFirst(t *testing.T) {
	depth, problems := partnerDepths(samplePartners().Partners)
	assert.Empty(t, problems)
	assert.Equal(t, map[int]int{10: 0, 11: 1, 12: 0}, depth)
}
