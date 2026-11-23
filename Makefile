.DEFAULT_GOAL := help

.PHONY: help build

help:
	@printf 'Targets:\n  help   Show available targets\n  build  Build odoo-cli and odoo-sync into target/\n'

build:
	@mkdir -p target
	go build -o target/odoo-cli ./cmd/odoo-cli
	go build -o target/odoo-sync ./cmd/odoo-sync
