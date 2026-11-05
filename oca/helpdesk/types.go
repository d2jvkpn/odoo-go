package helpdesk

import (
	jsonv2 "encoding/json/v2"

	odoo "github.com/d2jvkpn/odoo-go"
)

// Ticket is a helpdesk.ticket record.
type Ticket struct {
	ID              int                 `json:"id"`
	Name            string              `json:"name"`              // title; required in Odoo
	Number          string              `json:"number"`            // e.g. "HT00003"
	Description     string              `json:"description"`       // HTML body; required in Odoo
	Partner         odoo.Many2One       `json:"partner_id"`        // contact (res.partner)
	PartnerName     odoo.OptionalString `json:"partner_name"`      // contact name when no partner is linked
	PartnerEmail    odoo.OptionalString `json:"partner_email"`     // contact email
	SaleOrderIDs    []int               `json:"sale_order_ids"`    // optional helpdesk_mgmt_sale field
	Stage           odoo.Many2One       `json:"stage_id"`          // helpdesk.ticket.stage
	Team            odoo.Many2One       `json:"team_id"`           // helpdesk.ticket.team
	User            odoo.Many2One       `json:"user_id"`           // assigned res.users
	Priority        string              `json:"priority"`          // "0".."3"; empty when unset
	Channel         odoo.Many2One       `json:"channel_id"`        // source channel
	Category        odoo.Many2One       `json:"category_id"`       // helpdesk.ticket.category
	TagIDs          []int               `json:"tag_ids"`           // helpdesk.ticket.tag (many2many)
	Product         odoo.Many2One       `json:"product_id"`        // optional helpdesk_product field
	Company         odoo.Many2One       `json:"company_id"`        // res.company
	Active          bool                `json:"active"`            // archived when false
	Closed          bool                `json:"closed"`            // derived from the ticket's stage
	Unattended      bool                `json:"unattended"`        // derived from the ticket's stage
	Color           int                 `json:"color"`             // kanban color index
	KanbanState     odoo.OptionalString `json:"kanban_state"`      // normal, done or blocked
	Sequence        int                 `json:"sequence"`          // ticket ordering
	LastStageUpdate odoo.OptionalString `json:"last_stage_update"` // UTC "YYYY-MM-DD HH:MM:SS"
	AssignedDate    odoo.OptionalString `json:"assigned_date"`     // UTC "YYYY-MM-DD HH:MM:SS"
	ClosedDate      odoo.OptionalString `json:"closed_date"`       // UTC "YYYY-MM-DD HH:MM:SS"
	MessageIDs      []int               `json:"message_ids"`       // mail.message thread (chatter)
	CreateDate      string              `json:"create_date"`       // UTC "YYYY-MM-DD HH:MM:SS"
	WriteDate       string              `json:"write_date"`        // UTC "YYYY-MM-DD HH:MM:SS"
}

// UnmarshalJSON decodes a ticket, mapping an unset priority to the empty string.
func (t *Ticket) UnmarshalJSON(data []byte) error {
	type plain Ticket
	var aux struct {
		plain
		Priority odoo.OptionalString `json:"priority"`
	}

	if err := jsonv2.Unmarshal(data, &aux); err != nil {
		return err
	}
	*t = Ticket(aux.plain)
	t.Priority = string(aux.Priority)
	return nil
}

// TicketQuery filters and paginates tickets. Limit defaults to 100 and Order to "id desc".
// Domain is an Odoo domain; nil matches every visible ticket.
type TicketQuery struct {
	Domain            []any  // Odoo domain; nil matches all
	Limit             int    // maximum records per call; zero means 100
	Offset            int    // records to skip; zero means none
	Order             string // Odoo order clause; empty means "id desc"
	IncludeArchived   bool   // disable Odoo's active filter
	IncludeSaleOrders bool   // request helpdesk_mgmt_sale's sale_order_ids field
	IncludeProduct    bool   // request helpdesk_product's product_id field
}

// TicketStage is a helpdesk.ticket.stage record.
type TicketStage struct {
	ID              int                 `json:"id"`
	Name            string              `json:"name"`
	Description     odoo.OptionalString `json:"description"`
	Sequence        int                 `json:"sequence"`
	Active          bool                `json:"active"`
	Unattended      bool                `json:"unattended"`
	Closed          bool                `json:"closed"`            // tickets in this stage are closed
	CloseFromPortal bool                `json:"close_from_portal"` // customers may select this stage
	MailTemplate    odoo.Many2One       `json:"mail_template_id"`
	Fold            bool                `json:"fold"` // folded in the kanban view
	Company         odoo.Many2One       `json:"company_id"`
	TeamIDs         []int               `json:"team_ids"`
}

// TicketTeam is a helpdesk.ticket.team record.
type TicketTeam struct {
	ID           int           `json:"id"`
	Name         string        `json:"name"`
	CompleteName string        `json:"complete_name"`
	Sequence     int           `json:"sequence"`
	Active       bool          `json:"active"`
	Leader       odoo.Many2One `json:"user_id"`
	UserIDs      []int         `json:"user_ids"`
	CategoryIDs  []int         `json:"category_ids"`
	Company      odoo.Many2One `json:"company_id"`
	Alias        odoo.Many2One `json:"alias_id"`
	Color        int           `json:"color"`
	ShowInPortal bool          `json:"show_in_portal"`
	Parent       odoo.Many2One `json:"parent_id"`
}

// TicketChannel is a helpdesk.ticket.channel record (source of a ticket).
type TicketChannel struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}
