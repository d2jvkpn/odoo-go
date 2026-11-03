package helpdesk

import (
	"fmt"

	odoo "github.com/d2jvkpn/odoo-go"
)

// Client supplements *odoo.Client with OCA helpdesk ticket operations.
type Client struct {
	*odoo.Client
}

// NewClient wraps an existing odoo client.
func NewClient(base *odoo.Client) *Client {
	return &Client{Client: base}
}

// invalidArgument wraps odoo.ErrInvalidArgument with a detail message.
func invalidArgument(format string, args ...any) error {
	return fmt.Errorf("%w: %s", odoo.ErrInvalidArgument, fmt.Sprintf(format, args...))
}
