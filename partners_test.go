package odoo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSearchAndGetPartner(t *testing.T) {
	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/json/2/res.partner/search_read", r.URL.Path)
		params := requestParams(t, r)
		assert.EqualValues(t, 1, params["limit"])
		fmt.Fprint(w, `[{"id":7,"name":"Ann","email":"a@x.io","phone":false,"ref":"u-1",`+
			`"street":false,"city":false,"zip":false,"country_id":false}]`)
	})
	got, err := client.GetPartner(context.Background(), 7)
	require.NoError(t, err)
	assert.Equal(t, "u-1", string(got.Ref))
	assert.Empty(t, got.Phone)
	assert.Zero(t, got.Country.ID)

	_, err = client.GetPartner(context.Background(), 0)
	assert.ErrorIs(t, err, ErrInvalidArgument)
}

func TestGetPartnerNotFound(t *testing.T) {
	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `[]`) })
	_, err := client.GetPartner(context.Background(), 7)
	assert.ErrorIs(t, err, ErrPartnerNotFound)
}

func TestCreatePartner(t *testing.T) {
	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/json/2/res.partner/create", r.URL.Path)
		assertJSON(t, requestParams(t, r),
			`{"vals_list":[{"name":"Ann","email":"a@x.io","ref":"u-1","country_id":233}]}`)
		fmt.Fprint(w, "[7]")
	})
	id, err := client.CreatePartner(context.Background(),
		CreatePartnerRequest{Name: "Ann", Email: "a@x.io", Ref: "u-1", CountryID: 233})
	require.NoError(t, err)
	assert.Equal(t, 7, id)
}

func TestCreatePartnerRejectsBadInput(t *testing.T) {
	var calls int

	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) { calls++ })
	for name, in := range map[string]CreatePartnerRequest{
		"blank name":  {Name: " "},
		"neg country": {Name: "A", CountryID: -1},
	} {
		_, err := client.CreatePartner(context.Background(), in)
		assert.ErrorIs(t, err, ErrInvalidArgument, name)
	}
	assert.Zero(t, calls)
}

func TestUpdatePartner(t *testing.T) {
	var writes []map[string]any

	client := orderTestClient(t, recordingHandler(t, "res.partner", &writes))
	phone := "123"
	require.NoError(t, client.UpdatePartner(context.Background(), 7, UpdatePartnerRequest{Phone: &phone}))
	assertJSON(t, writes[0], `{"ids":[7],"vals":{"phone":"123"}}`)
}

func upsertHandler(t *testing.T, found string, paths *[]string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		*paths = append(*paths, r.URL.Path)
		switch r.URL.Path {
		case "/json/2/res.partner/search_read":
			assertJSON(t, requestParams(t, r)["domain"], `[["ref","=","u-1"]]`)
			fmt.Fprint(w, found)
		case "/json/2/res.partner/create":
			// the ref passed to UpsertPartnerByRef must reach Odoo, or the next upsert misses it
			assertJSON(t, requestParams(t, r), `{"vals_list":[{"name":"Ann","ref":"u-1","city":"Oslo"}]}`)
			fmt.Fprint(w, "[99]")
		case "/json/2/res.partner/write":
			assertJSON(t, requestParams(t, r), `{"ids":[7],"vals":{"name":"Ann","ref":"u-1","city":"Oslo"}}`)
			fmt.Fprint(w, "true")
		}
	}
}

const partnerRow = `{"id":%d,"name":"Ann","email":false,"phone":false,"ref":"u-1",` +
	`"street":false,"city":false,"zip":false,"country_id":false}`

func TestUpsertPartnerByRef(t *testing.T) {
	input := CreatePartnerRequest{Name: "Ann", City: "Oslo"}

	t.Run("miss creates", func(t *testing.T) {
		var paths []string

		client := orderTestClient(t, upsertHandler(t, `[]`, &paths))
		id, created, err := client.UpsertPartnerByRef(context.Background(), "u-1", input)
		require.NoError(t, err)
		assert.Equal(t, 99, id)
		assert.True(t, created)
		assert.Equal(t, []string{"/json/2/res.partner/search_read", "/json/2/res.partner/create"}, paths)
	})
	t.Run("hit updates", func(t *testing.T) {
		var paths []string

		client := orderTestClient(t, upsertHandler(t, "["+fmt.Sprintf(partnerRow, 7)+"]", &paths))
		id, created, err := client.UpsertPartnerByRef(context.Background(), "u-1", input)
		require.NoError(t, err)
		assert.Equal(t, 7, id)
		assert.False(t, created)
		assert.Equal(t, []string{"/json/2/res.partner/search_read", "/json/2/res.partner/write"}, paths)
	})
	t.Run("two matches", func(t *testing.T) {
		var paths []string

		rows := "[" + fmt.Sprintf(partnerRow, 7) + "," + fmt.Sprintf(partnerRow, 8) + "]"
		client := orderTestClient(t, upsertHandler(t, rows, &paths))
		_, _, err := client.UpsertPartnerByRef(context.Background(), "u-1", input)
		assert.ErrorIs(t, err, ErrAmbiguousPartner)
		assert.Equal(t, []string{"/json/2/res.partner/search_read"}, paths)
	})
	t.Run("blank ref", func(t *testing.T) {
		client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) { t.Error("request sent") })
		_, _, err := client.UpsertPartnerByRef(context.Background(), " ", input)
		assert.ErrorIs(t, err, ErrInvalidArgument)
	})
}

func TestSearchPartnersDecodesUnnamedAddress(t *testing.T) {
	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[{"id":7,"name":false,"street":"1 Main Street","country_id":false},`+
			`{"id":8,"name":"Ann","country_id":[3,"France"]}]`)
	})

	partners, err := client.SearchPartners(context.Background(), PartnerQuery{})
	require.NoError(t, err)
	require.Len(t, partners, 2)
	assert.Equal(t, "", partners[0].Name)
	assert.Equal(t, OptionalString("1 Main Street"), partners[0].Street)
	assert.Equal(t, "Ann", partners[1].Name)
	assert.Equal(t, 3, partners[1].Country.ID)
}

func TestSearchPartnersRequestsAndDecodesBaseFields(t *testing.T) {
	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		fields, ok := requestParams(t, r)["fields"].([]any)
		require.True(t, ok)
		assert.Len(t, fields, len(partnerFields))
		assert.Contains(t, fields, "category_id")
		assert.NotContains(t, fields, "mobile") // removed in Odoo 19
		fmt.Fprint(w, `[{"id":7,"name":"Ann","street2":"Floor 2","state_id":[5,"Oslo"],`+
			`"website":"https://x.io","is_company":true,"type":"invoice","parent_id":[3,"Acme"],`+
			`"function":"CEO","vat":"NO123","company_registry":"R-1","lang":"en_US","comment":"<p>vip</p>",`+
			`"category_id":[1,2],"user_id":[2,"Bob"],"company_id":[1,"My Co"],"barcode":"B1",`+
			`"industry_id":[4,"Retail"],"create_date":"2026-10-01 08:00:00","write_date":"2026-10-02 09:00:00"},`+
			`{"id":8,"name":"Cy","street2":false,"state_id":false,"website":false,"is_company":false,`+
			`"type":"contact","parent_id":false,"function":false,"vat":false,"company_registry":false,`+
			`"lang":false,"comment":false,"category_id":[],"user_id":false,"company_id":false,`+
			`"barcode":false,"industry_id":false,"create_date":"2026-10-01 08:00:00","write_date":false}]`)
	})

	partners, err := client.SearchPartners(context.Background(), PartnerQuery{})
	require.NoError(t, err)
	require.Len(t, partners, 2)

	full := partners[0]
	assert.Equal(t, Many2One{ID: 5, Name: "Oslo"}, full.State)
	assert.Equal(t, Many2One{ID: 3, Name: "Acme"}, full.Parent)
	assert.Equal(t, Many2One{ID: 2, Name: "Bob"}, full.User)
	assert.Equal(t, 4, full.Industry.ID)
	assert.True(t, full.IsCompany)
	assert.Equal(t, "invoice", full.Type)
	assert.Equal(t, OptionalString("NO123"), full.VAT)
	assert.Equal(t, []int{1, 2}, full.CategoryIDs)
	assert.Equal(t, OptionalString("2026-10-02 09:00:00"), full.WriteDate)

	empty := partners[1]
	assert.Empty(t, empty.Street2)
	assert.Zero(t, empty.State.ID)
	assert.Zero(t, empty.Company.ID)
	assert.Empty(t, empty.Lang)
	assert.Empty(t, empty.WriteDate)
	assert.NotNil(t, empty.CategoryIDs)
	assert.Empty(t, empty.CategoryIDs)
}

func TestCreatePartnerSendsBaseFields(t *testing.T) {
	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assertJSON(t, requestParams(t, r), `{"vals_list":[{"name":"Acme","street2":"Floor 2","state_id":5,`+
			`"website":"https://x.io","is_company":true,"type":"invoice","parent_id":3,"function":"CEO",`+
			`"vat":"NO123","company_registry":"R-1","lang":"en_US","comment":"vip","category_id":[[6,0,[1,2]]],`+
			`"user_id":2,"company_id":1,"barcode":"B1","industry_id":4}]}`)
		fmt.Fprint(w, "[7]")
	})
	_, err := client.CreatePartner(context.Background(), CreatePartnerRequest{
		Name: "Acme", Street2: "Floor 2", StateID: 5, Website: "https://x.io", IsCompany: true,
		Type: "invoice", ParentID: 3, Function: "CEO", VAT: "NO123", CompanyRegistry: "R-1", Lang: "en_US",
		Comment: "vip", CategoryIDs: ReplaceIDs{1, 2}, UserID: 2, CompanyID: 1, Barcode: "B1", IndustryID: 4,
	})
	require.NoError(t, err)
}

func TestPartnerRequestsRejectBadBaseFields(t *testing.T) {
	var calls int

	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) { calls++ })
	for name, in := range map[string]CreatePartnerRequest{
		"bad type":     {Name: "A", Type: "home"},
		"neg state":    {Name: "A", StateID: -1},
		"neg parent":   {Name: "A", ParentID: -1},
		"neg user":     {Name: "A", UserID: -1},
		"neg company":  {Name: "A", CompanyID: -1},
		"neg industry": {Name: "A", IndustryID: -1},
		"zero tag":     {Name: "A", CategoryIDs: ReplaceIDs{0}},
	} {
		_, err := client.CreatePartner(context.Background(), in)
		assert.ErrorIs(t, err, ErrInvalidArgument, name)
	}

	bad, neg, zero := "home", -1, ReplaceIDs{0}
	for name, in := range map[string]UpdatePartnerRequest{
		"bad type":   {Type: &bad},
		"neg state":  {StateID: &neg},
		"neg parent": {ParentID: &neg},
		"zero tag":   {CategoryIDs: &zero},
	} {
		err := client.UpdatePartner(context.Background(), 7, in)
		assert.ErrorIs(t, err, ErrInvalidArgument, name)
	}
	assert.Zero(t, calls)
}

func TestUpdatePartnerBaseFields(t *testing.T) {
	var writes []map[string]any

	client := orderTestClient(t, recordingHandler(t, "res.partner", &writes))
	vat, zero, company, none := "NO123", 0, true, ReplaceIDs{}
	err := client.UpdatePartner(context.Background(), 7, UpdatePartnerRequest{
		VAT: &vat, ParentID: &zero, IsCompany: &company, CategoryIDs: &none,
	})
	require.NoError(t, err)
	// a zero relation clears it (false), and an empty tag list removes every tag
	assertJSON(t, writes[0], `{"ids":[7],"vals":{"vat":"NO123","parent_id":false,"is_company":true,`+
		`"category_id":[[6,0,[]]]}}`)
}

func TestUpsertPartnerByRefMergesBaseFields(t *testing.T) {
	var writes []map[string]any

	handler := recordingHandler(t, "res.partner", &writes)
	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/json/2/res.partner/search_read" {
			fmt.Fprintf(w, "["+partnerRow+"]", 7)
			return
		}
		handler(w, r)
	})
	id, created, err := client.UpsertPartnerByRef(context.Background(), "u-1", CreatePartnerRequest{
		Name: "Ann", VAT: "NO123", StateID: 5, IsCompany: true, CategoryIDs: ReplaceIDs{1},
	})
	require.NoError(t, err)
	assert.Equal(t, 7, id)
	assert.False(t, created)
	assertJSON(t, writes[0], `{"ids":[7],"vals":{"name":"Ann","ref":"u-1","vat":"NO123","state_id":5,`+
		`"is_company":true,"category_id":[[6,0,[1]]]}}`)
}

func TestReplaceIDsDecodesPlainList(t *testing.T) {
	var in CreatePartnerRequest

	require.NoError(t, json.Unmarshal([]byte(`{"name":"A","category_id":[3,4]}`), &in))
	assert.Equal(t, ReplaceIDs{3, 4}, in.CategoryIDs)
	assert.Error(t, json.Unmarshal([]byte(`{"name":"A","category_id":[[6,0,[1]]]}`), &in))
}
