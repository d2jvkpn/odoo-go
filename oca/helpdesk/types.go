package helpdesk

import odoo "github.com/d2jvkpn/odoo-go"

// Ticket is a helpdesk.ticket record.
type Ticket struct {
	ID           int           `json:"id"`
	Name         string        `json:"name"`           // title; required in Odoo
	Number       string        `json:"number"`         // e.g. "HT00003"
	Description  string        `json:"description"`    // HTML body; required in Odoo
	Partner      odoo.Many2One `json:"partner_id"`     // contact (res.partner)
	SaleOrderIDs []int         `json:"sale_order_ids"` // linked sales orders (many2many)
	Stage        odoo.Many2One `json:"stage_id"`       // helpdesk.ticket.stage
	Team         odoo.Many2One `json:"team_id"`        // helpdesk.ticket.team
	User         odoo.Many2One `json:"user_id"`        // assigned res.users
	Priority     string        `json:"priority"`       // "0".."3"
	Channel      odoo.Many2One `json:"channel_id"`     // source channel
	Category     odoo.Many2One `json:"category_id"`    // helpdesk.ticket.category
	TagIDs       []int         `json:"tag_ids"`        // helpdesk.ticket.tag (many2many)
	Product      odoo.Many2One `json:"product_id"`     // product.product
	Closed       bool          `json:"closed"`         // derived from the ticket's stage
	MessageIDs   []int         `json:"message_ids"`    // mail.message thread (chatter)
	CreateDate   string        `json:"create_date"`    // UTC "YYYY-MM-DD HH:MM:SS"
	WriteDate    string        `json:"write_date"`     // UTC "YYYY-MM-DD HH:MM:SS"
}

// TicketQuery filters and paginates tickets. Limit defaults to 100 and Order to "id desc".
// Domain is an Odoo domain; nil matches every visible ticket.
type TicketQuery struct {
	Domain []any  // Odoo domain; nil matches all
	Limit  int    // maximum records per call; zero means 100
	Offset int    // records to skip; zero means none
	Order  string // Odoo order clause; empty means "id desc"
}

// TicketStage is a helpdesk.ticket.stage record.
type TicketStage struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Sequence int    `json:"sequence"`
	Closed   bool   `json:"closed"` // tickets in this stage are closed
	Fold     bool   `json:"fold"`   // folded in the kanban view
}

// TicketTeam is a helpdesk.ticket.team record.
type TicketTeam struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// TicketChannel is a helpdesk.ticket.channel record (source of a ticket).
type TicketChannel struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}
