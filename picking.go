package odoo

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"fmt"
)

// backorderWizard is the model Odoo opens when a delivery is validated with less than the demanded quantity.
const backorderWizard = "stock.backorder.confirmation"

// ValidatePickingOptions controls how ValidatePicking handles a short delivery.
type ValidatePickingOptions struct {
	// Backorder creates a backorder for the quantity that could not be delivered when the reserved quantity
	// is below the demand. When false, such a delivery is left untouched and ValidatePicking returns
	// ErrBackorderRequired.
	Backorder bool
}

// ValidatePicking validates a delivery (stock.picking), the "Validate" button in Odoo: the reserved quantities
// leave the stock, the delivery becomes "done" and the order's delivery status advances. It returns the
// delivery as read afterwards. This cannot be undone except by a return in Odoo.
//
// The delivery must be ready ("assigned"), otherwise ErrInvalidPickingState; an unknown ID returns
// ErrPickingNotFound. The reserved quantity is validated as is: the SDK does not change quantities. When it is
// below the demand Odoo opens a backorder question; see ValidatePickingOptions.Backorder. Any other question
// Odoo opens (immediate transfer, lot or serial numbers) is not handled and returns ErrUnexpectedResponse
// without changing the delivery.
//
// The write is attempted once. After a transport error the outcome is unknown: read the delivery with
// OrderPickings before validating again.
func (c *Client) ValidatePicking(
	ctx context.Context,
	pickingID int,
	opts ValidatePickingOptions,
) (*StockPicking, error) {
	var (
		err  error
		rows []struct {
			State string `json:"state"`
		}
		result   jsontext.Value
		pickings []StockPicking
	)

	if pickingID <= 0 {
		return nil, invalidArgument("delivery ID must be positive")
	}
	err = c.CallRead(ctx, "stock.picking", "read", map[string]any{
		"ids": []int{pickingID}, "fields": []string{"state"},
	}, &rows)
	if err != nil {
		return nil, fmt.Errorf("read delivery %d: %w", pickingID, err)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("%w: %d", ErrPickingNotFound, pickingID)
	}
	if rows[0].State != "assigned" {
		return nil, fmt.Errorf("%w: delivery %d is %q, not ready", ErrInvalidPickingState, pickingID, rows[0].State)
	}
	err = c.Call(ctx, "stock.picking", "button_validate", map[string]any{"ids": []int{pickingID}}, &result)
	if err != nil {
		return nil, fmt.Errorf("validate delivery %d: %w", pickingID, err)
	}
	if err = c.finishValidation(ctx, pickingID, result, opts); err != nil {
		return nil, err
	}
	if pickings, err = c.readPickings(ctx, []int{pickingID}); err != nil || len(pickings) != 1 {
		if err == nil {
			err = fmt.Errorf("%w: delivery %d is missing", ErrUnexpectedResponse, pickingID)
		}
		return nil, fmt.Errorf("delivery %d was validated but reading it failed: %w", pickingID, err)
	}
	return &pickings[0], nil
}

// finishValidation interprets what button_validate returned: true when the delivery is done, or an action
// that opens a wizard. Only the backorder wizard is processed, and only when opts.Backorder is set.
func (c *Client) finishValidation(
	ctx context.Context,
	pickingID int,
	result jsontext.Value,
	opts ValidatePickingOptions,
) error {
	var (
		err    error
		action struct {
			Model   string         `json:"res_model"`
			Context map[string]any `json:"context"`
		}
		wizardIDs []int
	)

	if bytes.Equal(bytes.TrimSpace(result), []byte("true")) {
		return nil
	}
	if err = jsonv2.Unmarshal(result, &action); err != nil || action.Model == "" {
		return fmt.Errorf("%w: validate delivery %d returned %s", ErrUnexpectedResponse, pickingID, result)
	}
	if action.Model != backorderWizard {
		return fmt.Errorf(
			"%w: validating delivery %d opened %s, which the SDK does not handle; finish it in Odoo",
			ErrUnexpectedResponse, pickingID, action.Model,
		)
	}
	if !opts.Backorder {
		return fmt.Errorf("%w: delivery %d", ErrBackorderRequired, pickingID)
	}
	err = c.Call(ctx, backorderWizard, "create", map[string]any{
		"vals_list": []any{map[string]any{}}, "context": action.Context,
	}, &wizardIDs)
	if err != nil {
		return fmt.Errorf("create backorder for delivery %d: %w", pickingID, err)
	}
	if len(wizardIDs) != 1 || wizardIDs[0] <= 0 {
		return fmt.Errorf("%w: expected one wizard ID for delivery %d", ErrUnexpectedResponse, pickingID)
	}
	err = c.Call(ctx, backorderWizard, "process", map[string]any{
		"ids": wizardIDs, "context": action.Context,
	}, nil)
	if err != nil {
		return fmt.Errorf("process backorder for delivery %d: %w", pickingID, err)
	}
	return nil
}
