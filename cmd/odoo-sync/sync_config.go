package main

import (
	"fmt"
	"strings"

	"github.com/d2jvkpn/odoo-go"
)

// loadSyncClient accepts a YAML path or a YAML path followed by a top-level section selector.
func loadSyncClient(connection string) (*odoo.Client, error) {
	var separator int

	separator = strings.LastIndexByte(connection, ':')
	if separator < 0 {
		return odoo.LoadClient(connection)
	}
	if separator == 0 || separator == len(connection)-1 {
		return nil, fmt.Errorf("%w: connection must be path.yaml:section", odoo.ErrInvalidConfig)
	}
	return odoo.LoadClientSection(connection[:separator], connection[separator+1:])
}
