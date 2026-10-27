# E-commerce P0 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development
> (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use
> checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add partner, stock, order-transition, and update/archive APIs (plus CLI) so an
e-commerce backend can browse, place an order, and confirm it after payment.

**Architecture:** One typed file per Odoo model in the root `odoo` package, sharing small
package-private write helpers (`patchValues`, `recordExists`, `writeRecord`, `updateRecord`).
`cmd/odoo-cli` gains `partners` and `stock` groups and new actions on the existing groups; it
only parses arguments and prints JSON.

**Tech Stack:** Go 1.27, `net/http/httptest`, `testify`, existing `validator` dependency.

**Spec:** `docs/records/superpowers/2026-10-27_ecommerce-p0.design.md`

## Global Constraints

- Odoo 19+ JSON-2 with sales, inventory, and accounting modules installed.
- Reads use `CallRead` (retryable); writes use `Call` and are never retried.
- No exported generic `Write` or `Unlink`; deletion is archive only (`active=false`), products only.
- Accept `context.Context` for network operations; wrap errors with `%w`; document exported APIs.
- Preserve Odoo JSON field names in struct tags and request bodies.
- Tests must not need real credentials or create live orders.
- Run `gofmt` on changed Go files. Keep build outputs in `target/`.
- **Never run `git commit`.** Commit only when the user invokes `/my-commit` or sends
  `:git-commit`. Each task therefore ends with a verification step, not a commit step.
- Harness docs are English, prose wrapped at 100 code points (tables and code blocks exempt).

## Review Focus

- Missing product IDs in a stock query are omitted, not an error (delisted items on list pages).
- An all-nil patch sends no request; a pointer to zero or `false` is sent explicitly.
- Updating or archiving a nonexistent record returns `ErrXxxNotFound` (Odoo's `write` would
  silently succeed), and archived records are still found so they can be unarchived.
- Invalid order state sends no write; a transport failure on confirm/cancel is requested once.
- `UpsertPartnerByRef` with two matches returns `ErrAmbiguousPartner` and writes nothing.
- CLI `orders confirm|cancel` without `-yes` performs no request and does not load credentials.

## File Structure

| File | Responsibility |
|------|----------------|
| `errors.go` (modify) | new sentinel errors |
| `write.go` (create) | `patchValues`, `recordExists`, `writeRecord`, `updateRecord` |
| `products.go`, `categories.go` (modify) | `UpdateProduct`, `SetProductActive`, `UpdateProductCategory` |
| `partners.go` (create) | `Partner` type, search/get/create/update/upsert |
| `stock.go` (create) | `ProductStock`, `GetProductStock` |
| `orders.go` (modify) | status fields, `ConfirmSaleOrder`, `CancelSaleOrder` |
| `cmd/odoo-cli/input.go` (create) | `decodeFile`, `printJSON` |
| `cmd/odoo-cli/{partners,stock,orders,products,categories}.go` | CLI surface |

### Task 1: Errors and write helpers

**Files:**
- Modify: `errors.go`
- Create: `write.go`, `write_test.go`

**Interfaces:**
- Produces:
  - `ErrPartnerNotFound`, `ErrAmbiguousPartner`, `ErrInvalidOrderState` (`error` sentinels)
  - `patchValues(patch any) (map[string]any, error)`
  - `(c *Client) recordExists(ctx context.Context, model string, id int) (bool, error)`
  - `(c *Client) writeRecord(ctx context.Context, model string, id int, vals map[string]any) error`
  - `(c *Client) updateRecord(ctx context.Context, model string, id int, patch any,
    notFound error) error`

- [ ] **Step 1: Write the failing test** — create `write_test.go`:

```go
package odoo

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// failTransport counts requests and always fails, to prove how often a call is attempted.
type failTransport struct{ calls int }

func (f *failTransport) RoundTrip(*http.Request) (*http.Response, error) {
	f.calls++
	return nil, errors.New("boom")
}

func TestPatchValues(t *testing.T) {
	name, zero, off := "Tea", 0.0, false
	vals, err := patchValues(struct {
		Name  *string  `json:"name,omitempty"`
		Price *float64 `json:"list_price,omitempty"`
		Sale  *bool    `json:"sale_ok,omitempty"`
		Skip  *int     `json:"categ_id,omitempty"`
	}{Name: &name, Price: &zero, Sale: &off})
	require.NoError(t, err)
	assertJSON(t, vals, `{"name":"Tea","list_price":0,"sale_ok":false}`)

	_, err = patchValues(struct {
		Name *string `json:"name,omitempty"`
	}{})
	assert.ErrorIs(t, err, ErrInvalidArgument)
}

func TestUpdateRecord(t *testing.T) {
	var paths []string

	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/json/2/res.partner/search_read":
			params := requestParams(t, r)
			assert.Equal(t, map[string]any{"active_test": false}, params["context"])
			fmt.Fprint(w, `[{"id":7}]`)
		case "/json/2/res.partner/write":
			assertJSON(t, requestParams(t, r), `{"ids":[7],"vals":{"name":"B"}}`)
			fmt.Fprint(w, "true")
		}
	})
	name := "B"
	err := client.updateRecord(context.Background(), "res.partner", 7,
		struct {
			Name *string `json:"name,omitempty"`
		}{&name}, ErrPartnerNotFound)
	require.NoError(t, err)
	assert.Equal(t, []string{"/json/2/res.partner/search_read", "/json/2/res.partner/write"}, paths)
}

func TestUpdateRecordRejectsWithoutWriting(t *testing.T) {
	var writes int

	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/json/2/res.partner/write" {
			writes++
		}
		fmt.Fprint(w, `[]`) // search_read finds nothing
	})
	name := "B"
	patch := struct {
		Name *string `json:"name,omitempty"`
	}{&name}

	err := client.updateRecord(context.Background(), "res.partner", 7, patch, ErrPartnerNotFound)
	assert.ErrorIs(t, err, ErrPartnerNotFound)
	err = client.updateRecord(context.Background(), "res.partner", 0, patch, ErrPartnerNotFound)
	assert.ErrorIs(t, err, ErrInvalidArgument)
	err = client.updateRecord(context.Background(), "res.partner", 7, struct {
		Name *string `json:"name,omitempty"`
	}{}, ErrPartnerNotFound)
	assert.ErrorIs(t, err, ErrInvalidArgument)
	assert.Zero(t, writes)
}

func TestWriteRecordIsNotRetried(t *testing.T) {
	transport := &failTransport{}
	client := NewClient("http://odoo.invalid", "k", "")
	client.HTTP = &http.Client{Transport: transport}
	client.Retry = RetryPolicy{MaxRetries: 3}

	err := client.writeRecord(context.Background(), "res.partner", 7, map[string]any{"name": "B"})
	require.Error(t, err)
	assert.Equal(t, 1, transport.calls)
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test -count=1 -run 'TestPatchValues|TestUpdateRecord|TestWriteRecord' .`
Expected: FAIL — `patchValues` / `updateRecord` undefined, `ErrPartnerNotFound` undefined.

- [ ] **Step 3: Implement.** In `errors.go`, extend the `var (...)` block:

```go
	// ErrPartnerNotFound means no partner visible to this user matched.
	ErrPartnerNotFound = errors.New("partner not found")
	// ErrAmbiguousPartner means more than one partner carries the same ref.
	ErrAmbiguousPartner = errors.New("more than one partner has this ref")
	// ErrInvalidOrderState means the order's state does not allow the requested transition.
	ErrInvalidOrderState = errors.New("invalid sales order state")
```

Create `write.go`:

```go
package odoo

import (
	"context"
	"encoding/json"
	"fmt"
)

// patchValues encodes a patch struct into Odoo write values. Fields tagged omitempty and left nil are
// dropped; a patch that sets nothing is rejected so no empty write is ever sent.
func patchValues(patch any) (map[string]any, error) {
	var (
		err  error
		data []byte
		vals map[string]any
	)

	if data, err = json.Marshal(patch); err != nil {
		return nil, fmt.Errorf("encode patch: %w", err)
	}
	if err = json.Unmarshal(data, &vals); err != nil {
		return nil, fmt.Errorf("encode patch: %w", err)
	}
	if len(vals) == 0 {
		return nil, invalidArgument("patch must set at least one field")
	}
	return vals, nil
}

// recordExists reports whether id exists in model. Archived records count, so they can be unarchived.
func (c *Client) recordExists(ctx context.Context, model string, id int) (bool, error) {
	var (
		err  error
		rows []struct {
			ID int `json:"id"`
		}
	)

	err = c.CallRead(ctx, model, "search_read", map[string]any{
		"domain":  []any{[]any{"id", "=", id}},
		"fields":  []string{"id"},
		"limit":   1,
		"context": map[string]any{"active_test": false},
	}, &rows)
	if err != nil {
		return false, fmt.Errorf("check %s %d: %w", model, id, err)
	}
	return len(rows) == 1, nil
}

// writeRecord writes vals to one record. It does not retry and does not check that the record exists.
func (c *Client) writeRecord(ctx context.Context, model string, id int, vals map[string]any) error {
	var err error

	err = c.Call(ctx, model, "write", map[string]any{"ids": []int{id}, "vals": vals}, nil)
	if err != nil {
		return fmt.Errorf("update %s %d: %w", model, id, err)
	}
	return nil
}

// updateRecord validates the patch, checks that the record exists (Odoo's write succeeds silently on a
// missing ID), then writes it. It does not retry.
func (c *Client) updateRecord(
	ctx context.Context,
	model string,
	id int,
	patch any,
	notFound error,
) error {
	var (
		err    error
		vals   map[string]any
		exists bool
	)

	if id <= 0 {
		return invalidArgument("%s ID must be positive", model)
	}
	if vals, err = patchValues(patch); err != nil {
		return err
	}
	if exists, err = c.recordExists(ctx, model, id); err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("%w: %d", notFound, id)
	}
	return c.writeRecord(ctx, model, id, vals)
}
```

- [ ] **Step 4: Verify**

Run: `gofmt -l . && go vet ./... && go test -count=1 .`
Expected: no gofmt output; all PASS.

### Task 2: Product and category updates

**Files:**
- Modify: `products.go`, `categories.go`
- Create: `update_test.go`

**Interfaces:**
- Consumes: `updateRecord`, `ErrProductNotFound`, `ErrProductCategoryNotFound`, `categoryModel`.
- Produces:
  - `type UpdateProductRequest struct { Name *string; ListPrice *float64;
    DefaultCode *string; CategoryID *int; SaleOK *bool }`
  - `(c *Client) UpdateProduct(ctx context.Context, id int, patch UpdateProductRequest) error`
  - `(c *Client) SetProductActive(ctx context.Context, id int, active bool) error`
  - `type UpdateProductCategoryRequest struct { Name *string; ParentID *int }`
  - `(c *Client) UpdateProductCategory(ctx context.Context, kind CategoryType, id int,
    patch UpdateProductCategoryRequest) error`

- [ ] **Step 1: Write the failing test** — create `update_test.go`:

```go
package odoo

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingHandler answers search_read with one row and write with true, recording write bodies.
func recordingHandler(t *testing.T, model string, writes *[]map[string]any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/json/2/" + model + "/search_read":
			fmt.Fprint(w, `[{"id":21}]`)
		case "/json/2/" + model + "/write":
			*writes = append(*writes, requestParams(t, r))
			fmt.Fprint(w, "true")
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}
}

func TestUpdateProduct(t *testing.T) {
	var writes []map[string]any

	client := orderTestClient(t, recordingHandler(t, "product.product", &writes))
	price, off := 0.0, false
	err := client.UpdateProduct(context.Background(), 21,
		UpdateProductRequest{ListPrice: &price, SaleOK: &off})
	require.NoError(t, err)
	require.Len(t, writes, 1)
	assertJSON(t, writes[0], `{"ids":[21],"vals":{"list_price":0,"sale_ok":false}}`)
}

func TestUpdateProductValidation(t *testing.T) {
	var calls int

	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) { calls++ })
	blank, neg, negID := " ", -1.0, -1
	for name, patch := range map[string]UpdateProductRequest{
		"empty":    {},
		"blank":    {Name: &blank},
		"price":    {ListPrice: &neg},
		"category": {CategoryID: &negID},
	} {
		err := client.UpdateProduct(context.Background(), 21, patch)
		assert.ErrorIs(t, err, ErrInvalidArgument, name)
	}
	assert.Zero(t, calls)
}

func TestUpdateProductNotFound(t *testing.T) {
	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `[]`) })
	name := "x"
	err := client.UpdateProduct(context.Background(), 21, UpdateProductRequest{Name: &name})
	assert.ErrorIs(t, err, ErrProductNotFound)
}

func TestSetProductActive(t *testing.T) {
	var writes []map[string]any

	client := orderTestClient(t, recordingHandler(t, "product.product", &writes))
	require.NoError(t, client.SetProductActive(context.Background(), 21, false))
	assertJSON(t, writes[0], `{"ids":[21],"vals":{"active":false}}`)
}

func TestUpdateProductCategory(t *testing.T) {
	var writes []map[string]any

	client := orderTestClient(t, recordingHandler(t, "product.public.category", &writes))
	name, parent := "Shop", 0
	err := client.UpdateProductCategory(context.Background(), WebsiteCategory, 3,
		UpdateProductCategoryRequest{Name: &name, ParentID: &parent})
	require.NoError(t, err)
	assertJSON(t, writes[0], `{"ids":[3],"vals":{"name":"Shop","parent_id":0}}`)

	err = client.UpdateProductCategory(context.Background(), "bad", 3, UpdateProductCategoryRequest{Name: &name})
	assert.ErrorIs(t, err, ErrInvalidArgument)
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test -count=1 -run 'TestUpdateProduct|TestSetProductActive' .`
Expected: FAIL — `UpdateProduct` undefined.

- [ ] **Step 3: Implement.** Append to `products.go`:

```go
// UpdateProductRequest changes selected fields of a product.product variant. Nil fields are left
// unchanged; a pointer to zero or false is sent explicitly.
type UpdateProductRequest struct {
	Name        *string  `json:"name,omitempty"`
	ListPrice   *float64 `json:"list_price,omitempty"`
	DefaultCode *string  `json:"default_code,omitempty"`
	CategoryID  *int     `json:"categ_id,omitempty"`
	SaleOK      *bool    `json:"sale_ok,omitempty"`
}

// UpdateProduct changes one product. It returns ErrProductNotFound for an unknown ID and does not
// retry; after a transport error the outcome may be unknown, so re-read the product before resending.
func (c *Client) UpdateProduct(ctx context.Context, id int, patch UpdateProductRequest) error {
	if patch.Name != nil && strings.TrimSpace(*patch.Name) == "" {
		return invalidArgument("name must not be empty")
	}
	if p := patch.ListPrice; p != nil && (math.IsNaN(*p) || math.IsInf(*p, 0) || *p < 0) {
		return invalidArgument("list_price must be finite and nonnegative")
	}
	if patch.CategoryID != nil && *patch.CategoryID < 0 {
		return invalidArgument("categ_id must not be negative")
	}
	return c.updateRecord(ctx, "product.product", id, patch, ErrProductNotFound)
}

// SetProductActive archives (false) or restores (true) a product. It does not retry.
func (c *Client) SetProductActive(ctx context.Context, id int, active bool) error {
	return c.updateRecord(ctx, "product.product", id, struct {
		Active bool `json:"active"`
	}{active}, ErrProductNotFound)
}
```

Append to `categories.go`:

```go
// UpdateProductCategoryRequest changes selected fields of a category. Nil fields are left unchanged;
// a pointer to zero ParentID detaches the category from its parent.
type UpdateProductCategoryRequest struct {
	Name     *string `json:"name,omitempty"`
	ParentID *int    `json:"parent_id,omitempty"`
}

// UpdateProductCategory changes one category of the selected type. It returns
// ErrProductCategoryNotFound for an unknown ID and does not retry.
func (c *Client) UpdateProductCategory(
	ctx context.Context,
	kind CategoryType,
	id int,
	patch UpdateProductCategoryRequest,
) error {
	var (
		err   error
		model string
	)

	if model, err = categoryModel(kind); err != nil {
		return err
	}
	if patch.Name != nil && strings.TrimSpace(*patch.Name) == "" {
		return invalidArgument("name must not be empty")
	}
	if patch.ParentID != nil && *patch.ParentID < 0 {
		return invalidArgument("parent_id must not be negative")
	}
	return c.updateRecord(ctx, model, id, patch, ErrProductCategoryNotFound)
}
```

- [ ] **Step 4: Verify**

Run: `gofmt -l . && go vet ./... && go test -count=1 .`
Expected: no gofmt output; all PASS.

### Task 3: Partners

**Files:**
- Create: `partners.go`, `partners_test.go`

**Interfaces:**
- Consumes: `updateRecord`, `writeRecord`, `patchValues`, `ErrPartnerNotFound`,
  `ErrAmbiguousPartner`.
- Produces:
  - `type Partner struct { ID int; Name string; Email, Phone, Ref, Street, City, Zip OptionalString;
    Country Many2One }`
  - `type PartnerQuery struct { Domain []any; Limit, Offset int; Order string }`
  - `type CreatePartnerRequest struct { Name, Email, Phone, Ref, Street, City, Zip string;
    CountryID int }`
  - `type UpdatePartnerRequest struct` (pointer fields, same names)
  - `SearchPartners(ctx, PartnerQuery) ([]Partner, error)`, `GetPartner(ctx, id) (*Partner, error)`
  - `CreatePartner(ctx, CreatePartnerRequest) (int, error)`,
    `UpdatePartner(ctx, id, UpdatePartnerRequest) error`
  - `UpsertPartnerByRef(ctx, ref string, CreatePartnerRequest) (id int, created bool, err error)`

- [ ] **Step 1: Write the failing test** — create `partners_test.go`:

```go
package odoo

import (
	"context"
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
		assert.EqualValues(t, 100, params["limit"])
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
		"blank name":   {Name: " "},
		"neg country":  {Name: "A", CountryID: -1},
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
```

- [ ] **Step 2: Run to verify failure**

Run: `go test -count=1 -run 'Partner' .`
Expected: FAIL — `GetPartner` / `CreatePartnerRequest` undefined.

- [ ] **Step 3: Implement** — create `partners.go`:

```go
package odoo

import (
	"context"
	"fmt"
	"strings"
)

// Partner is a res.partner record. Optional text fields are empty when unset in Odoo.
type Partner struct {
	ID      int            `json:"id"`
	Name    string         `json:"name"`
	Email   OptionalString `json:"email"`
	Phone   OptionalString `json:"phone"`
	Ref     OptionalString `json:"ref"`
	Street  OptionalString `json:"street"`
	City    OptionalString `json:"city"`
	Zip     OptionalString `json:"zip"`
	Country Many2One       `json:"country_id"`
}

// PartnerQuery filters and paginates partners. Limit defaults to 100 and Order to "id". Archived
// partners are not returned.
type PartnerQuery struct {
	Domain []any
	Limit  int
	Offset int
	Order  string
}

// SearchPartners returns res.partner records matching the query.
func (c *Client) SearchPartners(ctx context.Context, query PartnerQuery) ([]Partner, error) {
	var (
		err      error
		partners []Partner
	)

	if query.Limit < 0 || query.Offset < 0 {
		return nil, invalidArgument("limit and offset must not be negative")
	}
	if query.Limit == 0 {
		query.Limit = 100
	}
	if query.Order == "" {
		query.Order = "id"
	}
	if query.Domain == nil {
		query.Domain = []any{}
	}
	partners = make([]Partner, 0)
	err = c.CallRead(ctx, "res.partner", "search_read", map[string]any{
		"domain": query.Domain,
		"fields": []string{
			"id", "name", "email", "phone", "ref", "street", "city", "zip", "country_id",
		},
		"limit":  query.Limit,
		"offset": query.Offset,
		"order":  query.Order,
	}, &partners)
	if err != nil {
		return nil, fmt.Errorf("search partners: %w", err)
	}
	return partners, nil
}

// GetPartner retrieves one partner by ID, or ErrPartnerNotFound.
func (c *Client) GetPartner(ctx context.Context, id int) (*Partner, error) {
	var (
		err      error
		partners []Partner
	)

	if id <= 0 {
		return nil, invalidArgument("partner ID must be positive")
	}
	partners, err = c.SearchPartners(ctx, PartnerQuery{
		Domain: []any{[]any{"id", "=", id}},
		Limit:  1,
	})
	if err != nil {
		return nil, err
	}
	if len(partners) == 0 {
		return nil, fmt.Errorf("%w: %d", ErrPartnerNotFound, id)
	}
	return &partners[0], nil
}

// CreatePartnerRequest describes a new customer. Ref is the caller's own identifier (for example the
// e-commerce user ID) and is not unique in Odoo. Empty optional fields are omitted.
type CreatePartnerRequest struct {
	Name      string `json:"name"`
	Email     string `json:"email,omitempty"`
	Phone     string `json:"phone,omitempty"`
	Ref       string `json:"ref,omitempty"`
	Street    string `json:"street,omitempty"`
	City      string `json:"city,omitempty"`
	Zip       string `json:"zip,omitempty"`
	CountryID int    `json:"country_id,omitempty"`
}

func validateCreatePartner(input CreatePartnerRequest) error {
	if strings.TrimSpace(input.Name) == "" {
		return invalidArgument("name must not be empty")
	}
	if input.CountryID < 0 {
		return invalidArgument("country_id must not be negative")
	}
	return nil
}

// CreatePartner creates one partner and returns its ID. It does not retry. After a transport/decode
// error the outcome may be unknown; search by ref before resubmitting, or use UpsertPartnerByRef.
func (c *Client) CreatePartner(ctx context.Context, input CreatePartnerRequest) (int, error) {
	var (
		err error
		ids []int
	)

	if err = validateCreatePartner(input); err != nil {
		return 0, err
	}
	err = c.Call(ctx, "res.partner", "create", map[string]any{"vals_list": []any{input}}, &ids)
	if err != nil {
		return 0, fmt.Errorf("create partner: %w", err)
	}
	if len(ids) != 1 || ids[0] <= 0 {
		return 0, fmt.Errorf(
			"%w: expected one positive ID from create (partner may already exist)", ErrUnexpectedResponse,
		)
	}
	return ids[0], nil
}

// UpdatePartnerRequest changes selected fields of a partner. Nil fields are left unchanged.
type UpdatePartnerRequest struct {
	Name      *string `json:"name,omitempty"`
	Email     *string `json:"email,omitempty"`
	Phone     *string `json:"phone,omitempty"`
	Ref       *string `json:"ref,omitempty"`
	Street    *string `json:"street,omitempty"`
	City      *string `json:"city,omitempty"`
	Zip       *string `json:"zip,omitempty"`
	CountryID *int    `json:"country_id,omitempty"`
}

// UpdatePartner changes one partner. It returns ErrPartnerNotFound for an unknown ID and does not retry.
func (c *Client) UpdatePartner(ctx context.Context, id int, patch UpdatePartnerRequest) error {
	if patch.Name != nil && strings.TrimSpace(*patch.Name) == "" {
		return invalidArgument("name must not be empty")
	}
	if patch.CountryID != nil && *patch.CountryID < 0 {
		return invalidArgument("country_id must not be negative")
	}
	return c.updateRecord(ctx, "res.partner", id, patch, ErrPartnerNotFound)
}

// UpsertPartnerByRef finds the active partner whose ref equals ref and updates it with the non-empty
// fields of input, or creates one when none exists. It returns the partner ID and whether it was created.
// Two matches return ErrAmbiguousPartner and nothing is written. The search is a retryable read; the
// create or write is not retried.
//
// Odoo does not enforce a unique ref, so two concurrent upserts for the same ref can both miss and
// create duplicates. Callers must serialize per ref or add a unique constraint in Odoo.
func (c *Client) UpsertPartnerByRef(
	ctx context.Context,
	ref string,
	input CreatePartnerRequest,
) (int, bool, error) {
	var (
		err    error
		found  []Partner
		id     int
		patch  UpdatePartnerRequest
		values map[string]any
	)

	ref = strings.TrimSpace(ref)
	if ref == "" {
		return 0, false, invalidArgument("ref must not be empty")
	}
	input.Ref = ref
	if err = validateCreatePartner(input); err != nil {
		return 0, false, err
	}
	found, err = c.SearchPartners(ctx, PartnerQuery{
		Domain: []any{[]any{"ref", "=", ref}},
		Limit:  2,
	})
	if err != nil {
		return 0, false, err
	}
	switch len(found) {
	case 0:
		id, err = c.CreatePartner(ctx, input)
		return id, true, err
	case 1:
		patch = UpdatePartnerRequest{Name: &input.Name, Ref: &input.Ref}
		for field, text := range map[*(*string)]string{
			&patch.Email: input.Email, &patch.Phone: input.Phone, &patch.Street: input.Street,
			&patch.City: input.City, &patch.Zip: input.Zip,
		} {
			if text != "" {
				t := text
				*field = &t
			}
		}
		if input.CountryID != 0 {
			patch.CountryID = &input.CountryID
		}
		if values, err = patchValues(patch); err != nil {
			return 0, false, err
		}
		return found[0].ID, false, c.writeRecord(ctx, "res.partner", found[0].ID, values)
	default:
		return 0, false, fmt.Errorf("%w: %q", ErrAmbiguousPartner, ref)
	}
}
```

- [ ] **Step 4: Verify**

Run: `gofmt -l . && go vet ./... && go test -count=1 .`
Expected: no gofmt output; all PASS. (If `gofmt -l` lists `partners_test.go`, run `gofmt -w` on it.)

### Task 4: Stock

**Files:**
- Create: `stock.go`, `stock_test.go`

**Interfaces:**
- Produces:
  - `type ProductStock struct { ID int; QtyAvailable, VirtualAvailable, FreeQty float64 }`
  - `(c *Client) GetProductStock(ctx context.Context, ids []int) ([]ProductStock, error)`

- [ ] **Step 1: Write the failing test** — create `stock_test.go`:

```go
package odoo

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetProductStock(t *testing.T) {
	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/json/2/product.product/search_read", r.URL.Path)
		params := requestParams(t, r)
		assertJSON(t, params["domain"], `[["id","in",[21,22,99]]]`)
		assertJSON(t, params["fields"], `["id","qty_available","virtual_available","free_qty"]`)
		assert.Equal(t, map[string]any{"active_test": false}, params["context"])
		fmt.Fprint(w, `[{"id":21,"qty_available":5,"virtual_available":3,"free_qty":2.5},`+
			`{"id":22,"qty_available":0,"virtual_available":0,"free_qty":0}]`)
	})
	got, err := client.GetProductStock(context.Background(), []int{21, 22, 99})
	require.NoError(t, err)
	assert.Equal(t, []ProductStock{
		{ID: 21, QtyAvailable: 5, VirtualAvailable: 3, FreeQty: 2.5},
		{ID: 22},
	}, got) // ID 99 does not exist and is simply omitted
}

func TestGetProductStockEdges(t *testing.T) {
	var calls int

	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) { calls++ })
	got, err := client.GetProductStock(context.Background(), nil)
	require.NoError(t, err)
	assert.NotNil(t, got)
	assert.Empty(t, got)

	_, err = client.GetProductStock(context.Background(), []int{1, 0})
	assert.ErrorIs(t, err, ErrInvalidArgument)

	ids := make([]int, maxStockIDs+1)
	for i := range ids {
		ids[i] = i + 1
	}
	_, err = client.GetProductStock(context.Background(), ids)
	assert.ErrorIs(t, err, ErrInvalidArgument)
	assert.Zero(t, calls)
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test -count=1 -run 'ProductStock' .`
Expected: FAIL — `GetProductStock` undefined.

- [ ] **Step 3: Implement** — create `stock.go`:

```go
package odoo

import (
	"context"
	"fmt"
)

// maxStockIDs bounds one GetProductStock request.
const maxStockIDs = 200

// ProductStock reports on-hand quantities of one product.product variant in its default unit of
// measure. QtyAvailable is on hand, VirtualAvailable includes incoming and outgoing moves, and FreeQty
// is on hand minus reservations. Requires Odoo's stock module.
type ProductStock struct {
	ID               int     `json:"id"`
	QtyAvailable     float64 `json:"qty_available"`
	VirtualAvailable float64 `json:"virtual_available"`
	FreeQty          float64 `json:"free_qty"`
}

// GetProductStock returns stock for the given product.product IDs in one request, at most 200 IDs. IDs
// that do not exist are omitted from the result rather than reported as errors; archived products are
// included. An empty list returns an empty result without a request.
func (c *Client) GetProductStock(ctx context.Context, ids []int) ([]ProductStock, error) {
	var (
		err   error
		stock []ProductStock
	)

	if len(ids) > maxStockIDs {
		return nil, invalidArgument("at most %d product IDs per request", maxStockIDs)
	}
	for _, id := range ids {
		if id <= 0 {
			return nil, invalidArgument("product ID must be positive")
		}
	}
	stock = make([]ProductStock, 0, len(ids))
	if len(ids) == 0 {
		return stock, nil
	}
	err = c.CallRead(ctx, "product.product", "search_read", map[string]any{
		"domain":  []any{[]any{"id", "in", ids}},
		"fields":  []string{"id", "qty_available", "virtual_available", "free_qty"},
		"order":   "id",
		"context": map[string]any{"active_test": false},
	}, &stock)
	if err != nil {
		return nil, fmt.Errorf("get product stock: %w", err)
	}
	return stock, nil
}
```

- [ ] **Step 4: Verify**

Run: `gofmt -l . && go vet ./... && go test -count=1 .`
Expected: no gofmt output; all PASS.

### Task 5: Order status fields, confirm and cancel

**Files:**
- Modify: `orders.go`, `orders_test.go` (field list assertions), `cmd/odoo-cli/orders_test.go` and
  `cmd/odoo-cli/example_files_test.go` plus `examples/sale-order-get.response.json` only if they
  break on the new fields
- Create: `order_transition_test.go`

**Interfaces:**
- Consumes: `ErrSaleOrderNotFound`, `ErrInvalidOrderState`.
- Produces:
  - `SaleOrder.InvoiceStatus`, `SaleOrder.DeliveryStatus` (`OptionalString`, JSON `invoice_status`,
    `delivery_status`)
  - `(c *Client) ConfirmSaleOrder(ctx context.Context, id int) error`
  - `(c *Client) CancelSaleOrder(ctx context.Context, id int) error`

- [ ] **Step 1: Write the failing test** — create `order_transition_test.go`:

```go
package odoo

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func transitionHandler(t *testing.T, state, method, wantBody string, paths *[]string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		*paths = append(*paths, r.URL.Path)
		switch r.URL.Path {
		case "/json/2/sale.order/search_read":
			assertJSON(t, requestParams(t, r)["fields"], `["state"]`)
			if state == "" {
				fmt.Fprint(w, `[]`)
				return
			}
			fmt.Fprintf(w, `[{"state":%q}]`, state)
		case "/json/2/sale.order/" + method:
			assertJSON(t, requestParams(t, r), wantBody)
			fmt.Fprint(w, "true")
		}
	}
}

func TestConfirmSaleOrder(t *testing.T) {
	for _, state := range []string{"draft", "sent"} {
		var paths []string

		client := orderTestClient(t, transitionHandler(t, state, "action_confirm", `{"ids":[5]}`, &paths))
		require.NoError(t, client.ConfirmSaleOrder(context.Background(), 5), state)
		assert.Equal(t, []string{"/json/2/sale.order/search_read", "/json/2/sale.order/action_confirm"}, paths)
	}
}

func TestCancelSaleOrder(t *testing.T) {
	var paths []string

	body := `{"ids":[5],"context":{"disable_cancel_warning":true}}`
	client := orderTestClient(t, transitionHandler(t, "sale", "action_cancel", body, &paths))
	require.NoError(t, client.CancelSaleOrder(context.Background(), 5))
	assert.Equal(t, []string{"/json/2/sale.order/search_read", "/json/2/sale.order/action_cancel"}, paths)
}

func TestOrderTransitionsRejectBadStateWithoutWriting(t *testing.T) {
	for name, tc := range map[string]struct {
		state string
		call  func(*Client) error
		want  error
	}{
		"confirm sale":    {"sale", func(c *Client) error { return c.ConfirmSaleOrder(context.Background(), 5) }, ErrInvalidOrderState},
		"confirm cancel":  {"cancel", func(c *Client) error { return c.ConfirmSaleOrder(context.Background(), 5) }, ErrInvalidOrderState},
		"cancel cancel":   {"cancel", func(c *Client) error { return c.CancelSaleOrder(context.Background(), 5) }, ErrInvalidOrderState},
		"confirm missing": {"", func(c *Client) error { return c.ConfirmSaleOrder(context.Background(), 5) }, ErrSaleOrderNotFound},
		"cancel bad id":   {"draft", func(c *Client) error { return c.CancelSaleOrder(context.Background(), 0) }, ErrInvalidArgument},
	} {
		t.Run(name, func(t *testing.T) {
			var paths []string

			client := orderTestClient(t, transitionHandler(t, tc.state, "unused", `{}`, &paths))
			assert.ErrorIs(t, tc.call(client), tc.want)
			for _, p := range paths {
				assert.Equal(t, "/json/2/sale.order/search_read", p) // never a write
			}
		})
	}
}

func TestOrderTransitionsAreNotRetried(t *testing.T) {
	var reads, writes int

	client := orderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/json/2/sale.order/search_read" {
			reads++
			fmt.Fprint(w, `[{"state":"draft"}]`)
			return
		}
		writes++
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	})
	client.Retry = RetryPolicy{MaxRetries: 3}

	var httpErr *HTTPError
	require.ErrorAs(t, client.ConfirmSaleOrder(context.Background(), 5), &httpErr)
	assert.Equal(t, 503, httpErr.StatusCode)
	assert.Equal(t, 1, reads)
	assert.Equal(t, 1, writes)
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test -count=1 -run 'SaleOrder|OrderTransitions' .`
Expected: FAIL — `ConfirmSaleOrder` undefined.

- [ ] **Step 3: Implement.** In `orders.go`, add to the `SaleOrder` struct after `ClientOrderRef`:

```go
	InvoiceStatus  OptionalString `json:"invoice_status"`
	DeliveryStatus OptionalString `json:"delivery_status"`
```

and extend the `fields` list in `SearchSaleOrders` with `"invoice_status", "delivery_status"` (the
latter needs the stock module, matching the Global Constraints). Append:

```go
// saleOrderState reads the order's state, or ErrSaleOrderNotFound.
func (c *Client) saleOrderState(ctx context.Context, id int) (string, error) {
	var (
		err  error
		rows []struct {
			State string `json:"state"`
		}
	)

	if id <= 0 {
		return "", invalidArgument("sales order ID must be positive")
	}
	err = c.CallRead(ctx, "sale.order", "search_read", map[string]any{
		"domain": []any{[]any{"id", "=", id}},
		"fields": []string{"state"},
		"limit":  1,
	}, &rows)
	if err != nil {
		return "", fmt.Errorf("read sales order %d state: %w", id, err)
	}
	if len(rows) == 0 {
		return "", fmt.Errorf("%w: %d", ErrSaleOrderNotFound, id)
	}
	return rows[0].State, nil
}

// ConfirmSaleOrder turns a draft or sent quotation into a confirmed order (action_confirm). It checks
// the state first and returns ErrInvalidOrderState without writing when the order is not a quotation;
// the gap between that check and the write is accepted and Odoo's own validation is authoritative.
// It does not retry. After a transport error the outcome is unknown: call GetSaleOrder and inspect
// State before trying again.
func (c *Client) ConfirmSaleOrder(ctx context.Context, id int) error {
	var (
		err   error
		state string
	)

	if state, err = c.saleOrderState(ctx, id); err != nil {
		return err
	}
	if state != "draft" && state != "sent" {
		return fmt.Errorf("%w: cannot confirm order %d in state %q", ErrInvalidOrderState, id, state)
	}
	if err = c.Call(ctx, "sale.order", "action_confirm", map[string]any{"ids": []int{id}}, nil); err != nil {
		return fmt.Errorf("confirm sales order %d: %w", id, err)
	}
	return nil
}

// CancelSaleOrder cancels an order that is not already cancelled (action_cancel, with Odoo's
// cancellation wizard suppressed). Odoo may still refuse, for example for delivered or invoiced orders;
// that error passes through as an *HTTPError. It does not retry; see ConfirmSaleOrder for the
// uncertain-outcome rule.
func (c *Client) CancelSaleOrder(ctx context.Context, id int) error {
	var (
		err   error
		state string
	)

	if state, err = c.saleOrderState(ctx, id); err != nil {
		return err
	}
	if state == "cancel" {
		return fmt.Errorf("%w: order %d is already cancelled", ErrInvalidOrderState, id)
	}
	err = c.Call(ctx, "sale.order", "action_cancel", map[string]any{
		"ids":     []int{id},
		"context": map[string]any{"disable_cancel_warning": true},
	}, nil)
	if err != nil {
		return fmt.Errorf("cancel sales order %d: %w", id, err)
	}
	return nil
}
```

- [ ] **Step 4: Verify and repair fallout**

Run: `gofmt -l . && go vet ./... && go test -count=1 ./...`
Expected: new tests PASS. Existing tests that assert the `fields` list or the full `SaleOrder`
JSON (`orders_test.go`, `cmd/odoo-cli/orders_test.go`, `example_files_test.go`,
`examples/sale-order-get.response.json`) may fail because the two new fields now appear. Update
those expectations to include `"invoice_status"`/`"delivery_status"` (add the keys to the fixture
rows and to `sale-order-get.response.json`, empty-string when the fixture returns `false`), then
re-run until all PASS.

### Task 6: CLI

**Files:**
- Create: `cmd/odoo-cli/input.go`, `cmd/odoo-cli/partners.go`, `cmd/odoo-cli/stock.go`,
  `cmd/odoo-cli/writes.go`, `cmd/odoo-cli/writes_test.go`
- Modify: `cmd/odoo-cli/main.go`, `cmd/odoo-cli/orders.go`, `cmd/odoo-cli/products.go`,
  `cmd/odoo-cli/categories.go`

**Interfaces:**
- Consumes: every SDK method from Tasks 2–5; existing `writeFile`, `configYAML` test helpers.
- Produces: `decodeFile(path string, v any) error`, `printJSON(out io.Writer, v any) error`,
  `runPartners`, `runStock`, `runOrderTransition`, `runProductWrite`, `runCategoryUpdate`.

- [ ] **Step 1: Write the failing test** — create `cmd/odoo-cli/writes_test.go`:

```go
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
		"sale.order/search_read":   `[{"state":"draft"}]`,
		"sale.order/action_confirm": `true`,
	})
	out, err := runCLI(t, "-config", config, "orders", "confirm", "-id", "5", "-yes")
	require.NoError(t, err)
	assert.JSONEq(t, `{"action":"confirm","id":5,"executed":true}`, out)
	assert.Len(t, *paths, 2)

	_, err = runCLI(t, "-config", config, "orders", "confirm", "-yes")
	assert.Error(t, err) // missing -id
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
		{"partners", "upsert", "-file", file},                                   // no -ref
		{"partners", "create", "-file", writeFile(t, "b.json", `{"nme":"x"}`)},  // unknown field
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
		"product.product/search_read": `[{"id":21}]`,
		"product.product/write":       `true`,
		"product.category/search_read": `[{"id":4}]`,
		"product.category/write":      `true`,
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
```

- [ ] **Step 2: Run to verify failure**

Run:

```sh
go test -count=1 -run 'OrdersConfirm|PartnersCommands|StockGet|ProductAndCategoryWrites' ./cmd/odoo-cli
```

Expected: FAIL — unknown command `stock` / `partners`, `confirm` etc.

- [ ] **Step 3: Implement.**

Create `cmd/odoo-cli/input.go`:

```go
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// decodeFile reads exactly one JSON object from path into v, rejecting unknown fields.
func decodeFile(path string, v any) error {
	var (
		err     error
		f       *os.File
		decoder *json.Decoder
		extra   any
	)

	if path == "" {
		return fmt.Errorf("-file is required")
	}
	if f, err = os.Open(path); err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	decoder = json.NewDecoder(f)
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(v); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	if err = decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("%s must contain exactly one JSON object", path)
	}
	return nil
}

// printJSON writes v as indented JSON.
func printJSON(out io.Writer, v any) error {
	var encoder = json.NewEncoder(out)

	encoder.SetIndent("", "  ")
	return encoder.Encode(v)
}
```

Create `cmd/odoo-cli/partners.go`:

```go
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/d2jvkpn/odoo"
)

const partnersUsage = `Usage:
  odoo partners list [-domain '[]'] [-limit 100] [-offset 0] [-order id] [-config configs/local.yaml]
  odoo partners get -id ID [-config configs/local.yaml]
  odoo partners create -file partner.json [-config configs/local.yaml]
  odoo partners update -id ID -file patch.json [-config configs/local.yaml]
  odoo partners upsert -ref REF -file partner.json [-config configs/local.yaml]

upsert matches an active partner by ref and updates it, or creates one. Concurrent upserts for the
same ref may create duplicates; serialize them per ref.
`

func runPartners(ctx context.Context, args []string, out io.Writer) error {
	var (
		err        error
		action     string
		flags      *flag.FlagSet
		configPath *string
		query      odoo.PartnerQuery
		domain     string
		file       string
		ref        string
		id         int
		client     *odoo.Client
		result     any
	)

	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		_, err = io.WriteString(out, partnersUsage)
		return err
	}
	action = args[0]
	flags = flag.NewFlagSet("odoo partners "+action, flag.ContinueOnError)
	flags.SetOutput(out)
	configPath = flags.String("config", "configs/local.yaml", "path to YAML configuration")
	switch action {
	case "list":
		flags.IntVar(&query.Limit, "limit", 100, "maximum number of partners")
		flags.IntVar(&query.Offset, "offset", 0, "number of partners to skip")
		flags.StringVar(&query.Order, "order", "id", "Odoo sort expression")
		flags.StringVar(&domain, "domain", "[]", "Odoo domain as a JSON array")
	case "get":
		flags.IntVar(&id, "id", 0, "partner ID")
	case "create":
		flags.StringVar(&file, "file", "", "JSON file containing the partner")
	case "update":
		flags.IntVar(&id, "id", 0, "partner ID")
		flags.StringVar(&file, "file", "", "JSON file containing the fields to change")
	case "upsert":
		flags.StringVar(&ref, "ref", "", "caller's identifier stored in the partner ref")
		flags.StringVar(&file, "file", "", "JSON file containing the partner")
	default:
		return fmt.Errorf("unknown command %q; use -h for help", action)
	}
	if err = flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %v", flags.Args())
	}

	var (
		create odoo.CreatePartnerRequest
		patch  odoo.UpdatePartnerRequest
	)
	switch action {
	case "list":
		if query.Limit <= 0 || query.Offset < 0 {
			return fmt.Errorf("limit must be positive and offset nonnegative")
		}
		if query.Domain, err = parseDomain(domain); err != nil {
			return err
		}
	case "get", "update":
		if id <= 0 {
			return fmt.Errorf("id must be positive")
		}
	case "upsert":
		if ref == "" {
			return fmt.Errorf("upsert requires -ref")
		}
	}
	switch action {
	case "create", "upsert":
		if err = decodeFile(file, &create); err != nil {
			return err
		}
	case "update":
		if err = decodeFile(file, &patch); err != nil {
			return err
		}
	}
	if client, err = odoo.LoadClient(*configPath); err != nil {
		return err
	}
	switch action {
	case "list":
		result, err = client.SearchPartners(ctx, query)
	case "get":
		result, err = client.GetPartner(ctx, id)
	case "create":
		var created int
		created, err = client.CreatePartner(ctx, create)
		result = struct {
			ID int `json:"id"`
		}{created}
	case "update":
		err = client.UpdatePartner(ctx, id, patch)
		result = struct {
			ID int `json:"id"`
		}{id}
	case "upsert":
		var (
			upserted int
			created  bool
		)
		upserted, created, err = client.UpsertPartnerByRef(ctx, ref, create)
		result = struct {
			ID      int  `json:"id"`
			Created bool `json:"created"`
		}{upserted, created}
	}
	if err != nil {
		return err
	}
	return printJSON(out, result)
}
```

Create `cmd/odoo-cli/stock.go`:

```go
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/d2jvkpn/odoo"
)

const stockUsage = `Usage:
  odoo stock get -ids 1,2,3 [-config configs/local.yaml]

get prints on-hand, forecast, and free quantities for up to 200 product.product IDs. Unknown IDs
are omitted.
`

func runStock(ctx context.Context, args []string, out io.Writer) error {
	var (
		err        error
		flags      *flag.FlagSet
		configPath *string
		idList     string
		ids        []int
		client     *odoo.Client
		stock      []odoo.ProductStock
	)

	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		_, err = io.WriteString(out, stockUsage)
		return err
	}
	if args[0] != "get" {
		return fmt.Errorf("unknown command %q; use -h for help", args[0])
	}
	flags = flag.NewFlagSet("odoo stock get", flag.ContinueOnError)
	flags.SetOutput(out)
	configPath = flags.String("config", "configs/local.yaml", "path to YAML configuration")
	flags.StringVar(&idList, "ids", "", "comma-separated product.product IDs")
	if err = flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %v", flags.Args())
	}
	for _, part := range strings.Split(idList, ",") {
		id, convErr := strconv.Atoi(strings.TrimSpace(part))
		if convErr != nil || id <= 0 {
			return fmt.Errorf("-ids must be comma-separated positive integers")
		}
		ids = append(ids, id)
	}
	if client, err = odoo.LoadClient(*configPath); err != nil {
		return err
	}
	if stock, err = client.GetProductStock(ctx, ids); err != nil {
		return err
	}
	return printJSON(out, stock)
}
```

Create `cmd/odoo-cli/writes.go` (the new write actions of existing groups):

```go
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/d2jvkpn/odoo"
)

// parseWriteFlags parses -config, -id and optionally -file for a write action.
func parseWriteFlags(name string, args []string, out io.Writer, withFile bool) (config string, id int, file string, err error) {
	var flags = flag.NewFlagSet(name, flag.ContinueOnError)

	flags.SetOutput(out)
	flags.StringVar(&config, "config", "configs/local.yaml", "path to YAML configuration")
	flags.IntVar(&id, "id", 0, "record ID")
	if withFile {
		flags.StringVar(&file, "file", "", "JSON file containing the fields to change")
	}
	if err = flags.Parse(args); err != nil {
		return
	}
	if flags.NArg() != 0 {
		return "", 0, "", fmt.Errorf("unexpected positional arguments: %v", flags.Args())
	}
	if id <= 0 {
		return "", 0, "", fmt.Errorf("id must be positive")
	}
	return
}

// runOrderTransition handles `orders confirm|cancel`. Without -yes it only reports the action and
// sends nothing, so a mistyped command cannot change an order.
func runOrderTransition(ctx context.Context, action string, args []string, out io.Writer) error {
	var (
		err        error
		flags      *flag.FlagSet
		configPath *string
		id         *int
		yes        *bool
		client     *odoo.Client
	)

	flags = flag.NewFlagSet("odoo orders "+action, flag.ContinueOnError)
	flags.SetOutput(out)
	configPath = flags.String("config", "configs/local.yaml", "path to YAML configuration")
	id = flags.Int("id", 0, "sales order ID")
	yes = flags.Bool("yes", false, "actually perform the action; without it nothing is sent")
	if err = flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 || *id <= 0 {
		return fmt.Errorf("%s requires -id ID and takes no positional arguments", action)
	}
	if *yes {
		if client, err = odoo.LoadClient(*configPath); err != nil {
			return err
		}
		if action == "confirm" {
			err = client.ConfirmSaleOrder(ctx, *id)
		} else {
			err = client.CancelSaleOrder(ctx, *id)
		}
		if err != nil {
			return err
		}
	}
	return printJSON(out, struct {
		Action   string `json:"action"`
		ID       int    `json:"id"`
		Executed bool   `json:"executed"`
	}{action, *id, *yes})
}

// runProductWrite handles `products update|archive|unarchive`.
func runProductWrite(ctx context.Context, action string, args []string, out io.Writer) error {
	var (
		err    error
		config string
		id     int
		file   string
		patch  odoo.UpdateProductRequest
		client *odoo.Client
	)

	if config, id, file, err = parseWriteFlags("odoo products "+action, args, out, action == "update"); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if action == "update" {
		if err = decodeFile(file, &patch); err != nil {
			return err
		}
	}
	if client, err = odoo.LoadClient(config); err != nil {
		return err
	}
	switch action {
	case "update":
		if err = client.UpdateProduct(ctx, id, patch); err != nil {
			return err
		}
		return printJSON(out, struct {
			ID int `json:"id"`
		}{id})
	default:
		active := action == "unarchive"
		if err = client.SetProductActive(ctx, id, active); err != nil {
			return err
		}
		return printJSON(out, struct {
			ID     int  `json:"id"`
			Active bool `json:"active"`
		}{id, active})
	}
}

// runCategoryUpdate handles `categories update`.
func runCategoryUpdate(ctx context.Context, args []string, out io.Writer) error {
	var (
		err    error
		flags  *flag.FlagSet
		config *string
		kind   *string
		id     *int
		file   *string
		patch  odoo.UpdateProductCategoryRequest
		client *odoo.Client
	)

	flags = flag.NewFlagSet("odoo categories update", flag.ContinueOnError)
	flags.SetOutput(out)
	config = flags.String("config", "configs/local.yaml", "path to YAML configuration")
	kind = flags.String("type", "internal", "category type: internal or website")
	id = flags.Int("id", 0, "category ID")
	file = flags.String("file", "", "JSON file containing the fields to change")
	if err = flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 || *id <= 0 {
		return fmt.Errorf("update requires -id ID and -file patch.json")
	}
	if *kind != "internal" && *kind != "website" {
		return fmt.Errorf("type must be internal or website")
	}
	if err = decodeFile(*file, &patch); err != nil {
		return err
	}
	if client, err = odoo.LoadClient(*config); err != nil {
		return err
	}
	if err = client.UpdateProductCategory(ctx, odoo.CategoryType(*kind), *id, patch); err != nil {
		return err
	}
	return printJSON(out, struct {
		ID int `json:"id"`
	}{*id})
}
```

Wire the dispatch. In `main.go`, add to the `switch args[0]` after `categories`:

```go
	case "partners":
		command = runPartners
	case "stock":
		command = runStock
```

and extend `usage` with two lines:

```
  odoo [-config configs/local.yaml] partners list|get|create|update|upsert [options]
  odoo [-config configs/local.yaml] stock get -ids 1,2,3
```

In `orders.go`, `products.go`, and `categories.go`, insert directly after the line
`action = args[0]`:

```go
	if action == "confirm" || action == "cancel" { // orders.go only
		return runOrderTransition(ctx, action, args[1:], out)
	}
```

```go
	if action == "update" || action == "archive" || action == "unarchive" { // products.go only
		return runProductWrite(ctx, action, args[1:], out)
	}
```

```go
	if action == "update" { // categories.go only
		return runCategoryUpdate(ctx, args[1:], out)
	}
```

Add one usage line per new action to each file's usage constant (for example
`odoo orders confirm|cancel -id ID [-yes]` with a sentence that without `-yes` nothing is sent,
`odoo products update -id ID -file patch.json`, `odoo products archive|unarchive -id ID`,
`odoo categories update -id ID -file patch.json [-type internal|website]`).

- [ ] **Step 4: Verify**

Run: `gofmt -l . && go vet ./... && go test -count=1 ./...`
Expected: no gofmt output (run `gofmt -w` on the test files if listed); all PASS. Then
`go run ./cmd/odoo-cli -h` should list `partners` and `stock`.

### Task 7: Examples and docs

**Files:**
- Create: `examples/partner.json`, `examples/partner.response.json`, `examples/stock.response.json`
- Modify: `example_files_test.go`, `cmd/odoo-cli/example_files_test.go`, `README.md`,
  `docs/README.md`, `docs/manual-testing.md`

- [ ] **Step 1: Write the failing test.** In `example_files_test.go` add to the request-file map:

```go
		"examples/partner.json": &CreatePartnerRequest{},
```

In `cmd/odoo-cli/example_files_test.go`, add fixtures to the server `switch`
(`res.partner/search_read` → `[]`, `res.partner/create` → `[15]`) and compare the outputs of
`partners upsert -ref web-1001 -file examples/partner.json` with `examples/partner.response.json`,
following the existing product and category cases in that file.

- [ ] **Step 2: Run to verify failure**

Run: `go test -count=1 -run 'Example' ./...`
Expected: FAIL — `examples/partner.json` does not exist.

- [ ] **Step 3: Implement.** Create the example files:

`examples/partner.json`:

```json
{
  "name": "Azure Interior",
  "email": "orders@azure.example",
  "phone": "+47 22 00 00 00",
  "city": "Oslo",
  "country_id": 161
}
```

`examples/partner.response.json`:

```json
{"id": 15, "created": true}
```

`examples/stock.response.json`:

```json
[{"id": 21, "qty_available": 5, "virtual_available": 3, "free_qty": 2}]
```

Update docs:
- `README.md`: add usage for `partners`, `stock`, `orders confirm|cancel` (note `-yes`),
  `products update|archive|unarchive`, `categories update`, plus the Go API signatures and the
  uncertain-outcome rule (call `GetSaleOrder` and inspect `State` before retrying a transition).
- `docs/README.md` Architecture: add the new typed helpers to the list and add
  `UpdateProduct`, `UpdateProductCategory`, `UpdatePartner`, `UpsertPartnerByRef`,
  `ConfirmSaleOrder`, `CancelSaleOrder` to the never-retried writes sentence; add that
  `GetProductStock` is a retryable read. Keep lines at 100 code points.
- `docs/manual-testing.md`: add read-only checks `partners list -limit 5` and
  `stock get -ids <id>` only; do not add write commands.

- [ ] **Step 4: Final verification**

Run:

```sh
gofmt -l .
go vet ./...
go test -race -cover -count=1 ./...
perl -CSD -ne 'print "$ARGV:$.: ", length, "\n" if length > 100 && !/^\|/ && !/^ / ' docs/README.md
```

Expected: no gofmt output; vet clean; all tests PASS; no over-long prose lines in
`docs/README.md`. Report the results to the user and stop; do not commit.

## Self-review

- **Spec coverage:** partners (Task 3), stock (Task 4), confirm/cancel and status fields
  (Task 5), product update/archive and category update (Task 2), CLI with `-yes` guard (Task 6),
  examples and docs (Task 7), new errors and write helpers (Task 1). Retry boundary, state
  pre-check, upsert branches, empty and over-limit stock, and empty-patch cases are each asserted.
- **Spec deviations already folded into the spec:** `action_cancel` with
  `disable_cancel_warning` instead of private `_action_cancel`; no category archive because
  neither category model has `active`.
- **Type consistency:** helper names (`updateRecord`, `writeRecord`, `patchValues`,
  `recordExists`), `recordingHandler` (defined in Task 2's test file, reused in Task 3), and
  `failTransport` (Task 1) are used consistently; `maxStockIDs` is defined in `stock.go` and used
  by its test.
