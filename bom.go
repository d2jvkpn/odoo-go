package odoo

import (
	"context"
	"fmt"
	"math"
)

// BOMLine is one component of a bill of materials (mrp.bom.line).
type BOMLine struct {
	ID          int      `json:"id"`                                       // mrp.bom.line ID
	BOM         Many2One `json:"bom_id"`                                   // the owning bill of materials
	Product     Many2One `json:"product_id"`                               // component product.product
	Quantity    float64  `json:"product_qty"`                              // quantity in UoM
	UoM         Many2One `json:"product_uom_id"`                           // unit of measure
	ValueIDs    []int    `json:"bom_product_template_attribute_value_ids"` // non-empty: the line applies only to these variants
	OperationID Many2One `json:"operation_id"`                             // routing operation consuming the line; ID 0 when none
	Sequence    int      `json:"sequence"`                                 // display order
}

// BOM is a bill of materials (mrp.bom). The mrp module must be installed; reads fail with HTTP 500
// on a database without it. A template-level BOM has Variant.ID == 0.
type BOM struct {
	ID           int            `json:"id"`              // mrp.bom ID
	Code         OptionalString `json:"code"`            // reference
	Type         string         `json:"type"`            // "normal" (manufacture) or "phantom" (kit)
	Quantity     float64        `json:"product_qty"`     // quantity the BOM produces, in UoM
	UoM          Many2One       `json:"product_uom_id"`  // unit of measure of Quantity
	Template     Many2One       `json:"product_tmpl_id"` // produced product.template
	Variant      Many2One       `json:"product_id"`      // set when the BOM is for one variant only
	Active       bool           `json:"active"`          // false when archived
	OperationIDs []int          `json:"operation_ids"`   // routing operations; non-empty is unsupported by odoo-sync
	ByproductIDs []int          `json:"byproduct_ids"`   // byproducts; non-empty is unsupported by odoo-sync
	LineIDs      []int          `json:"bom_line_ids"`    // IDs of Lines
	Lines        []BOMLine      `json:"lines,omitzero"`  // filled by ListBOMs and GetBOM
}

var bomFields = []string{
	"id", "code", "type", "product_qty", "product_uom_id", "product_tmpl_id", "product_id", "active",
	"operation_ids", "byproduct_ids", "bom_line_ids",
}

var bomLineFields = []string{
	"id", "bom_id", "product_id", "product_qty", "product_uom_id",
	"bom_product_template_attribute_value_ids", "operation_id", "sequence",
}

// attachBOMLines reads the lines matching domain and fills BOM.Lines in line order.
func (c *Client) attachBOMLines(ctx context.Context, boms []BOM, domain []any) error {
	var (
		lines []BOMLine
		index = make(map[int]int, len(boms))
		err   error
	)

	if len(boms) == 0 {
		return nil
	}
	if lines, err = readAll[BOMLine](ctx, c, "mrp.bom.line", bomLineFields, domain, false); err != nil {
		return err
	}
	for i := range boms {
		index[boms[i].ID] = i
	}
	for _, l := range lines {
		if i, ok := index[l.BOM.ID]; ok {
			boms[i].Lines = append(boms[i].Lines, l)
		}
	}
	return nil
}

// ListBOMs returns every bill of materials ordered by ID, with Lines filled. With includeArchived
// it also returns archived BOMs.
func (c *Client) ListBOMs(ctx context.Context, includeArchived bool) ([]BOM, error) {
	var (
		boms []BOM
		err  error
	)

	if boms, err = readAll[BOM](ctx, c, "mrp.bom", bomFields, nil, includeArchived); err != nil {
		return nil, err
	}
	if err = c.attachBOMLines(ctx, boms, nil); err != nil {
		return nil, err
	}
	return boms, nil
}

// GetBOM returns one bill of materials with Lines filled, archived or not. It returns
// ErrBOMNotFound for an unknown ID.
func (c *Client) GetBOM(ctx context.Context, id int) (*BOM, error) {
	var (
		boms []BOM
		err  error
	)

	if id <= 0 {
		return nil, invalidArgument("BOM ID must be positive")
	}
	if boms, err = readAll[BOM](ctx, c, "mrp.bom", bomFields, []any{[]any{"id", "=", id}}, true); err != nil {
		return nil, err
	}
	if len(boms) == 0 {
		return nil, fmt.Errorf("%w: %d", ErrBOMNotFound, id)
	}
	if err = c.attachBOMLines(ctx, boms, []any{[]any{"bom_id", "=", id}}); err != nil {
		return nil, err
	}
	return &boms[0], nil
}

// CreateBOMLine is one component of a new BOM.
type CreateBOMLine struct {
	ProductID int     // component product.product ID; > 0
	Quantity  float64 // finite and > 0
	UoMID     int     // uom.uom ID; > 0
}

// CreateBOMRequest describes a new bill of materials.
type CreateBOMRequest struct {
	TemplateID int             // produced product.template ID; > 0
	VariantID  int             // 0 for a template-level BOM, else a product.product ID of that template
	Code       string          // optional reference
	Type       string          // "normal" or "phantom"; empty means "normal"
	Quantity   float64         // quantity produced; finite and > 0
	UoMID      int             // uom.uom ID; > 0
	Lines      []CreateBOMLine // at least one
}

func positiveFinite(n float64) bool { return !math.IsNaN(n) && !math.IsInf(n, 0) && n > 0 }

// CreateBOM creates a bill of materials with its lines in one request and returns its ID. It does
// not retry; after a transport error the outcome may be unknown, so list the BOMs before
// resubmitting.
func (c *Client) CreateBOM(ctx context.Context, in CreateBOMRequest) (int, error) {
	var (
		typ   = in.Type
		lines = make([]any, 0, len(in.Lines))
		vals  map[string]any
		ids   []int
		err   error
	)

	if in.TemplateID <= 0 || in.VariantID < 0 {
		return 0, invalidArgument("product_tmpl_id must be positive and product_id nonnegative")
	}
	if typ == "" {
		typ = "normal"
	}
	if typ != "normal" && typ != "phantom" {
		return 0, invalidArgument("type must be \"normal\" or \"phantom\"")
	}
	if !positiveFinite(in.Quantity) || in.UoMID <= 0 {
		return 0, invalidArgument("product_qty must be finite and positive and product_uom_id positive")
	}
	if len(in.Lines) == 0 {
		return 0, invalidArgument("a BOM needs at least one line")
	}
	for _, l := range in.Lines {
		if l.ProductID <= 0 || l.UoMID <= 0 || !positiveFinite(l.Quantity) {
			return 0, invalidArgument("each line needs a positive product, unit and finite positive quantity")
		}
		lines = append(lines, []any{0, 0, map[string]any{
			"product_id": l.ProductID, "product_qty": l.Quantity, "product_uom_id": l.UoMID,
		}})
	}
	vals = map[string]any{
		"product_tmpl_id": in.TemplateID, "type": typ, "product_qty": in.Quantity,
		"product_uom_id": in.UoMID, "bom_line_ids": lines,
	}
	if in.Code != "" {
		vals["code"] = in.Code
	}
	if in.VariantID > 0 {
		vals["product_id"] = in.VariantID
	}
	if ids, err = c.createRecords(ctx, "mrp.bom", []any{vals}); err != nil {
		return 0, err
	}
	return ids[0], nil
}

// SetBOMActive archives (false) or restores (true) a bill of materials. It returns ErrBOMNotFound
// for an unknown ID and does not retry.
func (c *Client) SetBOMActive(ctx context.Context, id int, active bool) error {
	return c.updateRecord(ctx, "mrp.bom", id, struct {
		Active bool `json:"active"`
	}{active}, ErrBOMNotFound)
}
