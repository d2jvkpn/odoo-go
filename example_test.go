package odoo_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/d2jvkpn/odoo"
)

func ExampleClient_Call() {
	// A local server makes the example runnable without Odoo credentials.
	// In an application, use your Odoo URL and an API key from configuration.
	var (
		server   *httptest.Server
		client   *odoo.Client
		err      error
		products []struct {
			ID        int     `json:"id"`
			Name      string  `json:"name"`
			ListPrice float64 `json:"list_price"`
		}
	)

	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[{"id":7,"name":"Desk","list_price":100}]`)
	}))
	defer server.Close()

	client = odoo.NewClient(server.URL, "example-api-key", "mydb")
	err = client.Call(context.Background(), "product.product", "search_read", map[string]any{
		"domain": []any{[]any{"sale_ok", "=", true}},
		"fields": []string{"id", "name", "list_price"},
		"limit":  100,
	}, &products)
	if err != nil {
		fmt.Println(err)
		return
	}
	for _, product := range products {
		fmt.Printf("%d: %s (%.2f)\n", product.ID, product.Name, product.ListPrice)
	}
	// Output: 7: Desk (100.00)
}
