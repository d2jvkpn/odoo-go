package odoo

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const bomRow = `{"id":7,"code":"CAKE","type":"normal","product_qty":2.0,"product_uom_id":[1,"Units"],` +
	`"product_tmpl_id":[1487,"Cake"],"product_id":false,"active":true,"operation_ids":[],` +
	`"byproduct_ids":[],"bom_line_ids":[70,71]}`

const bomLineRows = `[{"id":70,"bom_id":[7,"Cake"],"product_id":[10,"Flour"],"product_qty":0.5,` +
	`"product_uom_id":[2,"kg"],"bom_product_template_attribute_value_ids":[],"operation_id":false,"sequence":1},` +
	`{"id":71,"bom_id":[7,"Cake"],"product_id":[11,"Egg"],"product_qty":3.0,` +
	`"product_uom_id":[1,"Units"],"bom_product_template_attribute_value_ids":[5],"operation_id":false,"sequence":2}]`

func TestListBOMsAttachesLinesAndDecodesTemplateLevelBOM(t *testing.T) {
	var bomParams map[string]any

	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/json/2/mrp.bom/search_read":
			bomParams = requestParams(t, r)
			fmt.Fprint(w, "["+bomRow+"]")
		case "/json/2/mrp.bom.line/search_read":
			fmt.Fprint(w, bomLineRows)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})
	boms, err := client.ListBOMs(context.Background(), true)
	require.NoError(t, err)
	require.Len(t, boms, 1)
	b := boms[0]
	assert.Equal(t, OptionalString("CAKE"), b.Code)
	assert.Equal(t, 0, b.Variant.ID)
	assert.Equal(t, 1487, b.Template.ID)
	assert.Equal(t, 2.0, b.Quantity)
	require.Len(t, b.Lines, 2)
	assert.Equal(t, "Flour", b.Lines[0].Product.Name)
	assert.Equal(t, []int{5}, b.Lines[1].ValueIDs)
	assertJSON(t, bomParams["context"], `{"active_test":false}`)
}

func TestListBOMsWithoutArchivedOmitsContext(t *testing.T) {
	var params map[string]any

	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/json/2/mrp.bom/search_read" {
			params = requestParams(t, r)
		}
		fmt.Fprint(w, "[]")
	})
	_, err := client.ListBOMs(context.Background(), false)
	require.NoError(t, err)
	assert.NotContains(t, params, "context")
}

func TestListBOMsReadsEveryPage(t *testing.T) {
	page := func(n, start int) string {
		parts := make([]string, n)
		for i := range parts {
			parts[i] = fmt.Sprintf(`{"id":%d,"product_tmpl_id":[1,"t"],"product_id":false,"active":true}`, start+i)
		}
		return "[" + strings.Join(parts, ",") + "]"
	}
	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/json/2/mrp.bom/search_read" {
			fmt.Fprint(w, "[]")
			return
		}
		if int(requestParams(t, r)["offset"].(float64)) == 0 {
			fmt.Fprint(w, page(catalogPageSize, 1))
			return
		}
		fmt.Fprint(w, page(2, catalogPageSize+1))
	})
	boms, err := client.ListBOMs(context.Background(), false)
	require.NoError(t, err)
	assert.Len(t, boms, catalogPageSize+2)
}

func TestGetBOMFindsArchivedAndReportsNotFound(t *testing.T) {
	var params map[string]any
	found := true

	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/json/2/mrp.bom/search_read":
			params = requestParams(t, r)
			if found {
				fmt.Fprint(w, "["+bomRow+"]")
			} else {
				fmt.Fprint(w, "[]")
			}
		default:
			fmt.Fprint(w, bomLineRows)
		}
	})
	b, err := client.GetBOM(context.Background(), 7)
	require.NoError(t, err)
	assert.Len(t, b.Lines, 2)
	assertJSON(t, params["domain"], `[["id","=",7]]`)
	assertJSON(t, params["context"], `{"active_test":false}`)

	found = false
	_, err = client.GetBOM(context.Background(), 8)
	assert.ErrorIs(t, err, ErrBOMNotFound)

	_, err = client.GetBOM(context.Background(), 0)
	assert.ErrorIs(t, err, ErrInvalidArgument)
}

func TestCreateBOMSendsLinesAsCreateCommands(t *testing.T) {
	var bodies []map[string]any

	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/json/2/mrp.bom/create", r.URL.Path)
		bodies = append(bodies, requestParams(t, r))
		fmt.Fprint(w, "[7]")
	})
	lines := []CreateBOMLine{{ProductID: 10, Quantity: 0.5, UoMID: 2}}
	id, err := client.CreateBOM(context.Background(), CreateBOMRequest{
		TemplateID: 1487, Code: "CAKE", Quantity: 2, UoMID: 1, Lines: lines,
	})
	require.NoError(t, err)
	assert.Equal(t, 7, id)
	assertJSON(t, bodies[0], `{"vals_list":[{"product_tmpl_id":1487,"type":"normal","product_qty":2,`+
		`"product_uom_id":1,"code":"CAKE","bom_line_ids":[[0,0,{"product_id":10,"product_qty":0.5,"product_uom_id":2}]]}]}`)

	_, err = client.CreateBOM(context.Background(), CreateBOMRequest{
		TemplateID: 1487, VariantID: 33, Type: "phantom", Quantity: 1, UoMID: 1, Lines: lines,
	})
	require.NoError(t, err)
	assertJSON(t, bodies[1], `{"vals_list":[{"product_tmpl_id":1487,"product_id":33,"type":"phantom","product_qty":1,`+
		`"product_uom_id":1,"bom_line_ids":[[0,0,{"product_id":10,"product_qty":0.5,"product_uom_id":2}]]}]}`)
}

func TestCreateBOMRejectsBadInputWithoutRequests(t *testing.T) {
	var calls int

	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) { calls++ })
	nan := 0.0
	nan = nan / nan
	good := []CreateBOMLine{{ProductID: 10, Quantity: 1, UoMID: 1}}
	valid := CreateBOMRequest{TemplateID: 1, Quantity: 1, UoMID: 1, Lines: good}
	mutate := func(f func(*CreateBOMRequest)) CreateBOMRequest {
		in := valid
		in.Lines = append([]CreateBOMLine(nil), good...)
		f(&in)
		return in
	}
	cases := map[string]CreateBOMRequest{
		"no lines":         mutate(func(r *CreateBOMRequest) { r.Lines = nil }),
		"no template":      mutate(func(r *CreateBOMRequest) { r.TemplateID = 0 }),
		"negative variant": mutate(func(r *CreateBOMRequest) { r.VariantID = -1 }),
		"zero quantity":    mutate(func(r *CreateBOMRequest) { r.Quantity = 0 }),
		"nan quantity":     mutate(func(r *CreateBOMRequest) { r.Quantity = nan }),
		"no uom":           mutate(func(r *CreateBOMRequest) { r.UoMID = 0 }),
		"bad type":         mutate(func(r *CreateBOMRequest) { r.Type = "subcontract" }),
		"line no product":  mutate(func(r *CreateBOMRequest) { r.Lines[0].ProductID = 0 }),
		"line zero qty":    mutate(func(r *CreateBOMRequest) { r.Lines[0].Quantity = 0 }),
		"line no uom":      mutate(func(r *CreateBOMRequest) { r.Lines[0].UoMID = 0 }),
	}
	for name, in := range cases {
		_, err := client.CreateBOM(context.Background(), in)
		assert.ErrorIs(t, err, ErrInvalidArgument, name)
	}
	assert.Zero(t, calls)
}

func TestSetBOMActiveWritesActiveFlag(t *testing.T) {
	var writes []map[string]any

	client := orderTestClient(t, recordingHandler(t, "mrp.bom", &writes))
	require.NoError(t, client.SetBOMActive(context.Background(), 21, false))
	require.Len(t, writes, 1)
	assertJSON(t, writes[0], `{"ids":[21],"vals":{"active":false}}`)
	assert.ErrorIs(t, client.SetBOMActive(context.Background(), 0, true), ErrInvalidArgument)
}

func TestSetBOMActiveReportsNotFound(t *testing.T) {
	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "[]") })
	assert.ErrorIs(t, client.SetBOMActive(context.Background(), 5, true), ErrBOMNotFound)
}

func TestListBOMsGivesEachBOMOnlyItsOwnLines(t *testing.T) {
	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/json/2/mrp.bom/search_read":
			fmt.Fprint(w, `[{"id":7,"product_tmpl_id":[1,"A"],"active":true},{"id":8,"product_tmpl_id":[2,"B"],"active":true}]`)
		default:
			fmt.Fprint(w, `[{"id":70,"bom_id":[7,"A"],"product_id":[10,"x"]},{"id":80,"bom_id":[8,"B"],"product_id":[11,"y"]},`+
				`{"id":71,"bom_id":[7,"A"],"product_id":[12,"z"]},{"id":99,"bom_id":[55,"gone"],"product_id":[13,"w"]}]`)
		}
	})
	boms, err := client.ListBOMs(context.Background(), false)
	require.NoError(t, err)
	require.Len(t, boms, 2)
	ids := func(b BOM) []int {
		var out []int
		for _, l := range b.Lines {
			out = append(out, l.ID)
		}
		return out
	}
	assert.Equal(t, []int{70, 71}, ids(boms[0]))
	assert.Equal(t, []int{80}, ids(boms[1]))
}

func TestGetBOMReadsOnlyItsOwnLines(t *testing.T) {
	var lineParams map[string]any

	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/json/2/mrp.bom/search_read" {
			fmt.Fprint(w, "["+bomRow+"]")
			return
		}
		lineParams = requestParams(t, r)
		fmt.Fprint(w, "[]")
	})
	_, err := client.GetBOM(context.Background(), 7)
	require.NoError(t, err)
	assertJSON(t, lineParams["domain"], `[["bom_id","=",7]]`)
}
