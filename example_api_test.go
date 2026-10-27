package odoo_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/d2jvkpn/odoo-go"
)

// exampleClient returns a client for a local server that answers each /json/2/<model>/<method> path
// with the given JSON body, so the examples run without Odoo credentials. In an application, use
// odoo.LoadClient or odoo.NewClient with your Odoo URL and API key.
func exampleClient(replies map[string]string) (*odoo.Client, func()) {
	var server *httptest.Server

	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := replies[r.URL.Path]
		if !ok {
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, body)
	}))
	return odoo.NewClient(server.URL, "example-api-key", "mydb"), server.Close
}

func ExampleClient_SearchProducts() {
	client, closeServer := exampleClient(map[string]string{
		"/json/2/product.product/search_read": `[{"id":7,"name":"Desk","default_code":"DESK-1",
			"list_price":100,"categ_id":[1,"All"],"sale_ok":true,"is_storable":true}]`,
	})
	defer closeServer()

	products, err := client.SearchProducts(context.Background(), odoo.ProductQuery{
		Domain: []any{[]any{"sale_ok", "=", true}}, // Odoo domain; nil matches every product
		Limit:  20,                                 // zero means 100
		Order:  "name asc",                         // empty means "id"
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	for _, p := range products {
		fmt.Printf("%d %s %s %.2f %s\n", p.ID, p.Name, p.DefaultCode, p.ListPrice, p.Category.Name)
	}
	// Output: 7 Desk DESK-1 100.00 All
}

func ExampleClient_AllProducts() {
	client, closeServer := exampleClient(map[string]string{
		"/json/2/product.product/search_read": `[
			{"id":1,"name":"Desk","default_code":false,"list_price":100,"categ_id":[1,"All"]},
			{"id":2,"name":"Chair","default_code":false,"list_price":40,"categ_id":[1,"All"]}]`,
	})
	defer closeServer()

	// AllProducts fetches page after page; Limit is the page size. The first error ends the loop.
	for p, err := range client.AllProducts(context.Background(), odoo.ProductQuery{Limit: 100}) {
		if err != nil {
			fmt.Println(err)
			return
		}
		fmt.Println(p.ID, p.Name)
	}
	// Output:
	// 1 Desk
	// 2 Chair
}

func ExampleClient_CreateSaleOrder() {
	client, closeServer := exampleClient(map[string]string{
		"/json/2/sale.order/create":         `[42]`,
		"/json/2/sale.order/search_read":    `[{"state":"draft"}]`,
		"/json/2/sale.order/action_confirm": `true`,
	})
	defer closeServer()

	ctx := context.Background()
	price := 99.5 // nil lets Odoo compute the price; a pointer to zero creates a free line
	orderID, err := client.CreateSaleOrder(ctx, odoo.CreateSaleOrderRequest{
		PartnerID:      7,          // required
		ClientOrderRef: "web-1001", // the shop's own order number, not a unique key
		Origin:         "web",
		Lines: []odoo.CreateSaleOrderLine{ // at least one line is required
			{ProductID: 3, Quantity: 2, UnitPrice: &price},
			{ProductID: 5, Quantity: 1},
		},
	})
	if err != nil {
		// Writes are never retried. After a transport error the order may or may not exist: look it
		// up (for example by ClientOrderRef) before resubmitting, or you may create a duplicate.
		fmt.Println(err)
		return
	}
	fmt.Println("created quotation", orderID)

	// CreateSaleOrder only creates a draft quotation; confirm it separately.
	if err = client.ConfirmSaleOrder(ctx, orderID); err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println("confirmed")
	// Output:
	// created quotation 42
	// confirmed
}

func ExampleClient_UpsertPartnerByRef() {
	client, closeServer := exampleClient(map[string]string{
		"/json/2/res.partner/search_read": `[]`, // no partner with this ref yet
		"/json/2/res.partner/create":      `[9]`,
	})
	defer closeServer()

	// Ref is your own customer identifier. The partner is found by ref and updated, or created.
	id, created, err := client.UpsertPartnerByRef(context.Background(), "user-42", odoo.CreatePartnerRequest{
		Name:  "Alice",
		Email: "alice@example.com",
	})
	if err != nil {
		// odoo.ErrAmbiguousPartner means two partners already share this ref.
		fmt.Println(err)
		return
	}
	fmt.Println(id, created)
	// Output: 9 true
}

func ExampleClient_SetProductStock() {
	client, closeServer := exampleClient(map[string]string{
		"/json/2/product.product/search_read":        `[{"is_storable":true}]`,
		"/json/2/stock.quant/create":                 `[11]`,
		"/json/2/stock.quant/action_apply_inventory": `true`,
		"/json/2/stock.location/search_read":         `[{"id":8,"complete_name":"WH/Stock"}]`,
	})
	defer closeServer()

	ctx := context.Background()
	locations, err := client.ListStockLocations(ctx) // valid targets for SetProductStock
	if err != nil {
		fmt.Println(err)
		return
	}

	// SetProductStock sets the on-hand quantity (an inventory adjustment, not an increment).
	err = client.SetProductStock(ctx, 7, locations[0].ID, 25)
	if errors.Is(err, odoo.ErrProductNotStorable) {
		fmt.Println("enable is_storable on the product first")
		return
	}
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println("stock set at", locations[0].Name)
	// Output: stock set at WH/Stock
}

func ExampleClient_UpdateProduct() {
	client, closeServer := exampleClient(map[string]string{
		"/json/2/product.product/search_read": `[{"id":7}]`, // the product must exist
		"/json/2/product.product/write":       `true`,
	})
	defer closeServer()

	// Pointer fields distinguish "leave unchanged" (nil) from an explicit zero or empty value.
	price := 120.0
	notForSale := false
	err := client.UpdateProduct(context.Background(), 7, odoo.UpdateProductRequest{
		ListPrice: &price,
		SaleOK:    &notForSale, // sent as false; Name, CategoryID and the rest stay unchanged
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println("updated")
	// Output: updated
}

// Errors from the SDK can be tested with errors.Is (sentinel errors) and errors.As (*HTTPError).
func Example_errorHandling() {
	client, closeServer := exampleClient(map[string]string{
		"/json/2/product.product/search_read": `[]`,
	})
	defer closeServer()

	_, err := client.GetProduct(context.Background(), 999)
	fmt.Println(errors.Is(err, odoo.ErrProductNotFound))

	// A rejected API key is an *HTTPError carrying the status code and the start of the body.
	rejecting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "invalid key", http.StatusUnauthorized)
	}))
	defer rejecting.Close()

	_, err = odoo.NewClient(rejecting.URL, "wrong-key", "").GetProduct(context.Background(), 1)
	var httpErr *odoo.HTTPError
	if errors.As(err, &httpErr) {
		fmt.Println(httpErr.StatusCode)
	}
	// Output:
	// true
	// 401
}
