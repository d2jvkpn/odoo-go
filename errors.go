package odoo

import (
	"errors"
	"fmt"
	"time"
)

var (
	// ErrInvalidArgument means the caller passed an invalid parameter; no request was sent.
	ErrInvalidArgument = errors.New("invalid argument")
	// ErrInvalidConfig means the YAML configuration is missing or malformed.
	ErrInvalidConfig = errors.New("invalid config")
	// ErrUnexpectedResponse means Odoo answered successfully but the body was not in the expected shape.
	ErrUnexpectedResponse = errors.New("unexpected response")

	// ErrProductNotFound means no product visible to this user matched.
	ErrProductNotFound = errors.New("product not found")
	// ErrProductCategoryNotFound means no category visible to this user matched.
	ErrProductCategoryNotFound = errors.New("product category not found")
	// ErrSaleOrderNotFound means no order visible to this API user matched the ID.
	ErrSaleOrderNotFound = errors.New("sales order not found")
)

// HTTPError is returned by Client.Call for a non-2xx response. Body holds at most 4096 bytes. Use errors.As to
// inspect it; for example, StatusCode 401 means the API key was rejected.
type HTTPError struct {
	StatusCode int
	Body       string
	// RetryAfter is the delay requested by the server's Retry-After header; zero when absent or invalid.
	RetryAfter time.Duration
}

// Error implements error.
func (e *HTTPError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.StatusCode, e.Body)
}

// invalidArgument wraps ErrInvalidArgument with a detail message.
func invalidArgument(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidArgument, fmt.Sprintf(format, args...))
}
