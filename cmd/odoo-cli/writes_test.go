package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cliServer records request paths and answers by path suffix.
func cliServer(t *testing.T, answers map[string]string) (string, *[]string) {
	var paths []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		for suffix, body := range answers {
			if strings.HasSuffix(r.URL.Path, suffix) {
				fmt.Fprint(w, body)
				return
			}
		}
		http.Error(w, "unexpected "+r.URL.Path, http.StatusNotFound)
	}))
	t.Cleanup(server.Close)
	return writeFile(t, "local.yaml", configYAML(server.URL, "db")), &paths
}

func runCLI(t *testing.T, args ...string) (string, error) {
	var out bytes.Buffer

	err := run(context.Background(), args, &out)
	return out.String(), err
}

func TestOrdersConfirmRequiresYes(t *testing.T) {
	config, paths := cliServer(t, nil)

	out, err := runCLI(t, "-config", config, "orders", "confirm", "-id", "5")
	require.NoError(t, err)
	assert.JSONEq(t, `{"action":"confirm","id":5,"executed":false}`, out)
	assert.Empty(t, *paths)

	// no config file needed when nothing is executed
	out, err = runCLI(t, "-config", "/nonexistent.yaml", "orders", "cancel", "-id", "5")
	require.NoError(t, err)
	assert.JSONEq(t, `{"action":"cancel","id":5,"executed":false}`, out)
}

func TestOrdersConfirmExecutes(t *testing.T) {
	config, paths := cliServer(t, map[string]string{
		"sale.order/search_read":    `[{"state":"draft"}]`,
		"sale.order/action_confirm": `true`,
	})
	out, err := runCLI(t, "-config", config, "orders", "confirm", "-id", "5", "-yes")
	require.NoError(t, err)
	assert.JSONEq(t, `{"action":"confirm","id":5,"executed":true}`, out)
	assert.Len(t, *paths, 2)

	_, err = runCLI(t, "-config", config, "orders", "confirm", "-yes")
	assert.Error(t, err) // missing -id
}

func TestOrdersUpdate(t *testing.T) {
	config, paths := cliServer(t, map[string]string{
		"sale.order/search_read": `[{"id":42}]`,
		"sale.order/write":       `true`,
	})
	out, err := runCLI(t, "-config", config, "orders", "update", "-id", "42",
		"-file", writeFile(t, "u.json", `{"origin":"web","note":"<p>x</p>"}`))
	require.NoError(t, err)
	assert.JSONEq(t, `{"id":42}`, out)
	assert.Len(t, *paths, 2)

	before := len(*paths)
	for name, body := range map[string]string{"empty": `{}`, "unknown": `{"state":"sale"}`} {
		_, err = runCLI(t, "-config", config, "orders", "update", "-id", "42",
			"-file", writeFile(t, "b.json", body))
		assert.Error(t, err, name)
	}
	assert.Len(t, *paths, before) // both fail locally, before any request
}

func TestPartnersCommands(t *testing.T) {
	config, _ := cliServer(t, map[string]string{
		"res.partner/search_read": `[]`,
		"res.partner/create":      `[99]`,
	})
	file := writeFile(t, "p.json", `{"name":"Ann","city":"Oslo"}`)

	out, err := runCLI(t, "-config", config, "partners", "upsert", "-ref", "u-1", "-file", file)
	require.NoError(t, err)
	assert.JSONEq(t, `{"id":99,"created":true}`, out)

	out, err = runCLI(t, "-config", config, "partners", "create", "-file", file)
	require.NoError(t, err)
	assert.JSONEq(t, `{"id":99}`, out)

	for _, args := range [][]string{
		{"partners", "upsert", "-file", file},                                      // no -ref
		{"partners", "create", "-file", writeFile(t, "b.json", `{"nme":"x"}`)},     // unknown field
		{"partners", "create", "-file", writeFile(t, "c.json", `{"name":"a"} {}`)}, // trailing data
	} {
		_, err = runCLI(t, append([]string{"-config", config}, args...)...)
		assert.Error(t, err, "%v", args)
	}
}

func TestStockGet(t *testing.T) {
	config, _ := cliServer(t, map[string]string{
		"product.product/search_read": `[{"id":21,"qty_available":5,"virtual_available":3,"free_qty":2}]`,
	})
	out, err := runCLI(t, "-config", config, "stock", "get", "-ids", "21,22")
	require.NoError(t, err)
	assert.JSONEq(t, `[{"id":21,"qty_available":5,"virtual_available":3,"free_qty":2}]`, out)

	for _, ids := range []string{"", "a,b", "0"} {
		_, err = runCLI(t, "-config", config, "stock", "get", "-ids", ids)
		assert.Error(t, err, ids)
	}
}

func TestProductAndCategoryWrites(t *testing.T) {
	config, _ := cliServer(t, map[string]string{
		"product.product/search_read":  `[{"id":21}]`,
		"product.product/write":        `true`,
		"product.category/search_read": `[{"id":4}]`,
		"product.category/write":       `true`,
	})
	patch := writeFile(t, "u.json", `{"list_price":0}`)

	out, err := runCLI(t, "-config", config, "products", "update", "-id", "21", "-file", patch)
	require.NoError(t, err)
	assert.JSONEq(t, `{"id":21}`, out)

	out, err = runCLI(t, "-config", config, "products", "archive", "-id", "21")
	require.NoError(t, err)
	assert.JSONEq(t, `{"id":21,"active":false}`, out)

	out, err = runCLI(t, "-config", config, "products", "unarchive", "-id", "21")
	require.NoError(t, err)
	assert.JSONEq(t, `{"id":21,"active":true}`, out)

	cat := writeFile(t, "c.json", `{"name":"Tea"}`)
	out, err = runCLI(t, "-config", config, "categories", "update", "-id", "4", "-file", cat)
	require.NoError(t, err)
	assert.JSONEq(t, `{"id":4}`, out)

	_, err = runCLI(t, "-config", config, "products", "update", "-id", "21",
		"-file", writeFile(t, "e.json", `{}`)) // empty patch
	assert.Error(t, err)
}

func TestStockLocationsAndSet(t *testing.T) {
	config, paths := cliServer(t, map[string]string{
		"stock.location/search_read":         `[{"id":5,"complete_name":"WH/Stock"}]`,
		"product.product/search_read":        `[{"is_storable":true}]`,
		"stock.quant/create":                 `[9]`,
		"stock.quant/action_apply_inventory": `null`,
	})
	out, err := runCLI(t, "-config", config, "stock", "locations")
	require.NoError(t, err)
	assert.JSONEq(t, `[{"id":5,"complete_name":"WH/Stock"}]`, out)

	before := len(*paths)
	out, err = runCLI(t, "-config", config, "stock", "set", "-product-id", "21", "-location-id", "5", "-qty", "30")
	require.NoError(t, err)
	assert.JSONEq(t, `{"product_id":21,"location_id":5,"qty":30,"executed":false}`, out)
	assert.Len(t, *paths, before) // without -yes nothing is sent

	out, err = runCLI(t, "-config", "/nonexistent.yaml", "stock", "set", "-product-id", "21", "-location-id", "5", "-qty", "0")
	require.NoError(t, err) // config is not even read without -yes
	assert.JSONEq(t, `{"product_id":21,"location_id":5,"qty":0,"executed":false}`, out)

	out, err = runCLI(t, "-config", config, "stock", "set", "-product-id", "21", "-location-id", "5", "-qty", "30", "-yes")
	require.NoError(t, err)
	assert.JSONEq(t, `{"product_id":21,"location_id":5,"qty":30,"executed":true}`, out)
	assert.Len(t, *paths, before+3) // read, create, apply

	for _, args := range [][]string{
		{"stock", "set", "-location-id", "5", "-qty", "1", "-yes"},
		{"stock", "set", "-product-id", "21", "-qty", "1", "-yes"},
		{"stock", "set", "-product-id", "21", "-location-id", "5", "-yes"}, // -qty is required
		{"stock", "set", "-product-id", "21", "-location-id", "5", "-qty", "-2", "-yes"},
	} {
		_, err = runCLI(t, append([]string{"-config", config}, args...)...)
		assert.Error(t, err, "%v", args)
	}
	assert.Len(t, *paths, before+3) // rejected locally
}

func TestCountriesList(t *testing.T) {
	config, _ := cliServer(t, map[string]string{
		"res.country/search_read": `[{"id":161,"name":"Norway","code":"NO"}]`,
	})
	out, err := runCLI(t, "-config", config, "countries", "list")
	require.NoError(t, err)
	assert.JSONEq(t, `[{"id":161,"name":"Norway","code":"NO"}]`, out)

	_, err = runCLI(t, "-config", config, "countries", "get")
	assert.Error(t, err)
	_, err = runCLI(t, "-config", config, "countries", "list", "extra")
	assert.Error(t, err)
}
