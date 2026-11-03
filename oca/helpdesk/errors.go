package helpdesk

import "errors"

// ErrTicketNotFound means no ticket visible to this API user matched the ID.
var ErrTicketNotFound = errors.New("ticket not found")
