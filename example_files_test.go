package odoo

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestExampleRequestFilesMatchTypes keeps the request files in examples/ valid for the SDK's request types.
func TestExampleRequestFilesMatchTypes(t *testing.T) {
	for file, target := range map[string]any{
		"examples/product.json":    &CreateProductRequest{},
		"examples/sale-order.json": &CreateSaleOrderRequest{},
	} {
		t.Run(file, func(t *testing.T) {
			data, err := os.ReadFile(file)
			require.NoError(t, err)

			decoder := json.NewDecoder(bytes.NewReader(data))
			decoder.DisallowUnknownFields()
			require.NoError(t, decoder.Decode(target))
		})
	}
}
