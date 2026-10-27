package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExampleResponseFilesMatchCLIOutput runs the CLI against Odoo-shaped fixtures and compares its output with
// the response files in examples/, so the documented output cannot drift from the real one.
func TestExampleResponseFilesMatchCLIOutput(t *testing.T) {
	var (
		server *httptest.Server
		config string
	)

	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		switch {
		case strings.HasSuffix(r.URL.Path, "product.product/search_read") &&
			bytes.Contains(body, []byte("qty_available")):
			fmt.Fprint(w, `[{"id":21,"qty_available":5,"virtual_available":3,"free_qty":2}]`)
		case strings.HasSuffix(r.URL.Path, "product.template/create"):
			fmt.Fprint(w, "[9]")
		case strings.HasSuffix(r.URL.Path, "product.template/read"):
			fmt.Fprint(w, `[{"id":9,"product_variant_id":[21,"Green Tea 100g"]}]`)
		case strings.HasSuffix(r.URL.Path, "product.product/search_read"):
			fmt.Fprint(w, `[{"id":21,"name":"Green Tea 100g","default_code":"TEA-GREEN-100","list_price":12.5,`+
				`"categ_id":[4,"All / Saleable / Tea"],"sale_ok":true,"is_storable":true}]`)
		case strings.HasSuffix(r.URL.Path, "product.category/create"):
			fmt.Fprint(w, "[9]")
		case strings.HasSuffix(r.URL.Path, "res.partner/search_read"):
			fmt.Fprint(w, `[]`)
		case strings.HasSuffix(r.URL.Path, "res.partner/create"):
			fmt.Fprint(w, "[15]")
		case strings.HasSuffix(r.URL.Path, "sale.order/create"):
			fmt.Fprint(w, "[42]")
		case strings.HasSuffix(r.URL.Path, "sale.order/search_read"):
			fmt.Fprint(w, `[{"id":42,"name":"S00042","partner_id":[15,"Azure Interior"],`+
				`"date_order":"2026-10-27 10:00:00","state":"draft","amount_untaxed":150,"amount_tax":15,`+
				`"amount_total":165,"currency_id":[1,"USD"],"client_order_ref":"WX202610270001","invoice_status":"no","delivery_status":"pending","origin":"web","note":"<p>Leave at door</p>","order_line":[11,12]}]`)
		case strings.HasSuffix(r.URL.Path, "sale.order.line/read"):
			fmt.Fprint(w, `[{"id":11,"product_id":[101,"Green Tea 100g"],"name":"Green Tea 100g",`+
				`"product_uom_qty":2,"price_unit":25,"price_subtotal":50,"display_type":false},`+
				`{"id":12,"product_id":[102,"Black Tea 100g"],"name":"Black Tea 100g",`+
				`"product_uom_qty":5,"price_unit":20,"price_subtotal":100,"display_type":false}]`)
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	config = writeFile(t, "local.yaml", configYAML(server.URL, ""))

	for _, tc := range []struct {
		name     string
		args     []string
		response string
	}{
		{"product create", []string{"products", "create", "-file", "../../examples/product.json"},
			"product.response.json"},
		{"category create", []string{"categories", "create", "-file", "../../examples/category.json"},
			"category.response.json"},
		{"product get", []string{"products", "get", "-id", "21"}, "product-get.response.json"},
		{"order create", []string{"orders", "create", "-file", "../../examples/sale-order.json"},
			"sale-order.response.json"},
		{"order get", []string{"orders", "get", "-id", "42"}, "sale-order-get.response.json"},
		{"partner upsert", []string{"partners", "upsert", "-ref", "web-1001", "-file",
			"../../examples/partner.json"}, "partner.response.json"},
		{"stock get", []string{"stock", "get", "-ids", "21"}, "stock.response.json"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var (
				out  bytes.Buffer
				want []byte
				err  error
			)

			err = run(context.Background(), append([]string{"-config", config}, tc.args...), &out)
			require.NoError(t, err)
			want, err = os.ReadFile("../../examples/" + tc.response)
			require.NoError(t, err)
			assert.JSONEq(t, string(want), out.String())
		})
	}
}
