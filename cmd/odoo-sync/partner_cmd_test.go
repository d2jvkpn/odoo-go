package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const sourcePartnersJSON = `[` +
	`{"id":10,"name":"Alice Co.","ref":"user-42","email":"a@x.com","is_company":true,"type":"contact","active":true,` +
	`"parent_id":false,"state_id":false,"country_id":[160,"Norway"],"category_id":[5]},` +
	`{"id":11,"name":"Warehouse","ref":false,"type":"delivery","active":true,"parent_id":[10,"Alice Co."],` +
	`"state_id":false,"country_id":false,"category_id":[]}]`

func partnerSourceRoutes() map[string]string {
	return map[string]string{
		"/json/2/res.partner/search_read":          sourcePartnersJSON,
		"/json/2/res.partner.category/search_read": `[{"id":5,"name":"VIP","display_name":"VIP"}]`,
	}
}

func partnerTargetRoutes(partners string) map[string]string {
	return map[string]string{
		"/json/2/res.partner/search_read":          partners,
		"/json/2/res.partner.category/search_read": `[{"id":5,"name":"VIP","display_name":"VIP"}]`,
		"/json/2/res.country/search_read":          `[{"id":160,"name":"Norway","code":"NO"}]`,
		"/json/2/res.lang/search_read":             `[{"id":1,"code":"en_US"}]`,
		"/json/2/res.partner/create":               `[90]`,
	}
}

func exportPartnersFile(t *testing.T) string {
	t.Helper()
	source := fakeOdoo(t, partnerSourceRoutes(), &[]string{})
	file := filepath.Join(t.TempDir(), "partners.json")
	var out bytes.Buffer
	config := writeConnection(t, "src.yaml", source)
	require.NoError(t, run(context.Background(), []string{"partners", "export", "-source", config, "-out", file}, &out))
	assert.Contains(t, out.String(), "exported 2 partners")
	data, err := os.ReadFile(file)
	require.NoError(t, err)
	assert.Contains(t, string(data), fmt.Sprintf(`"filename": %q`, config))
	assert.NotContains(t, string(data), `"name": "src"`)
	return file
}

func TestPartnersExportCheckAndDryRunImportWriteNothing(t *testing.T) {
	file := exportPartnersFile(t)
	var out bytes.Buffer
	require.NoError(t, run(context.Background(), []string{"partners", "check", "-in", file}, &out))
	assert.Contains(t, out.String(), "ok: 2 partners")

	var writes []string
	target := fakeOdoo(t, partnerTargetRoutes(`[]`), &writes)
	out.Reset()
	require.NoError(t, run(context.Background(), []string{
		"partners", "import", "-target", writeConnection(t, "dst.yaml", target), "-in", file, "-dry-run",
	}, &out))
	assert.Contains(t, out.String(), `create partner 10 "Alice Co."`)
	assert.Contains(t, out.String(), "summary: 2 to create")
	assert.Empty(t, writes)
}

func TestPartnersExportCanInferCountryFromOdooState(t *testing.T) {
	routes := partnerSourceRoutes()
	routes["/json/2/res.partner/search_read"] = `[{"id":18,"name":"杭州门店","ref":"HZ-18",` +
		`"type":"contact","active":true,"parent_id":false,"country_id":false,"state_id":[31,"浙江省"],` +
		`"category_id":[]}]`
	routes["/json/2/res.country.state/search_read"] = `[{"id":31,"name":"浙江省","code":"浙",` +
		`"country_id":[48,"China"]}]`
	var writes []string
	source := fakeOdoo(t, routes, &writes)
	config := writeConnection(t, "src.yaml", source)
	path := filepath.Join(t.TempDir(), "partners.json")
	var out bytes.Buffer
	require.NoError(t, run(context.Background(), []string{
		"partners", "export", "--source", config, "--out", path, "--infer-country-from-state",
	}, &out))
	f, err := readPartnersFile(path)
	require.NoError(t, err)
	require.Len(t, f.Partners, 1)
	assert.Equal(t, "China", f.Partners[0].Country)
	assert.Equal(t, "浙江省", f.Partners[0].State)
	assert.Contains(t, out.String(), `inferred country "China" from state "浙江省"`)
	assert.Empty(t, writes)

	out.Reset()
	require.NoError(t, run(context.Background(), []string{
		"partners", "export", "--source", config, "--out", path,
	}, &out))
	f, err = readPartnersFile(path)
	require.NoError(t, err)
	assert.Empty(t, f.Partners[0].Country)
	assert.Empty(t, f.Partners[0].State)
	assert.Contains(t, out.String(), "is not exported")
}

func TestPartnersImportCreatesPartners(t *testing.T) {
	file := exportPartnersFile(t)
	var writes []string
	target := fakeOdoo(t, partnerTargetRoutes(`[]`), &writes)
	var out bytes.Buffer
	require.NoError(t, run(context.Background(), []string{
		"partners", "import", "-target", writeConnection(t, "dst.yaml", target), "-in", file,
	}, &out))
	assert.Equal(t, []string{"/json/2/res.partner/create", "/json/2/res.partner/create"}, writes)
	assert.Contains(t, out.String(), "created 2, updated 0, unchanged 0, skipped 0, archived 0, restored 0, failed 0")
}

func TestPartnersImportStopsOnProblemsBeforeAnyWrite(t *testing.T) {
	file := exportPartnersFile(t)
	routes := partnerTargetRoutes(`[]`)
	routes["/json/2/res.country/search_read"] = `[]`
	var writes []string
	target := fakeOdoo(t, routes, &writes)
	var out bytes.Buffer
	err := run(context.Background(), []string{"partners", "import", "-target", writeConnection(t, "dst.yaml", target), "-in", file}, &out)
	require.Error(t, err)
	assert.Contains(t, out.String(), `country "Norway" does not exist in the target`)
	assert.Empty(t, writes)
}

func TestPartnersCommandsRejectBadFlagsBeforeAnyRequest(t *testing.T) {
	ctx := context.Background()
	var out bytes.Buffer
	assert.ErrorContains(t, run(ctx, []string{"partners"}, &out), "missing command")
	assert.ErrorContains(t, run(ctx, []string{"partners", "nope"}, &out), "unknown command")
	assert.ErrorContains(t, run(ctx, []string{"partners", "export", "-source", "s.yaml"}, &out), "-out")
	assert.ErrorContains(t, run(ctx, []string{"partners", "import", "-in", "p.json"}, &out), "-target")
	assert.ErrorContains(t, run(ctx, []string{"partners", "import", "-target", "t.yaml", "-in", "p.json", "-empty-key", "maybe"}, &out), "-empty-key")
	assert.ErrorContains(t, run(ctx, []string{"partners", "check"}, &out), "-in")
}

func TestPartnersImportExitsNonZeroAfterAFailedWrite(t *testing.T) {
	file := exportPartnersFile(t)
	routes := partnerTargetRoutes(`[]`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/json/2/res.partner/create" {
			http.Error(w, `{"name":"odoo.exceptions.ValidationError","message":"boom"}`, http.StatusInternalServerError)
			return
		}
		body, ok := routes[r.URL.Path]
		if !ok {
			t.Errorf("unexpected path %s", r.URL.Path)
			http.Error(w, "{}", http.StatusNotFound)
			return
		}
		fmt.Fprint(w, body)
	}))
	t.Cleanup(server.Close)

	var out bytes.Buffer
	err := run(context.Background(), []string{"partners", "import", "-target", writeConnection(t, "dst.yaml", server.URL), "-in", file}, &out)
	require.Error(t, err)
	assert.ErrorContains(t, err, "partner operations failed")
	assert.Contains(t, out.String(), "failed:")
	assert.Contains(t, out.String(), "created 0, updated 0, unchanged 0, skipped 0, archived 0, restored 0, failed 2")
	assert.Contains(t, out.String(), "skipped because its parent 10 was not written")
}
