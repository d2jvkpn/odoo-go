package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/d2jvkpn/odoo-go/oca/helpdesk"
)

type helpdeskInput struct {
	create        helpdesk.CreateTicketRequest
	patch         helpdesk.UpdateTicketRequest
	message       helpdesk.PostTicketMessageRequest
	activity      helpdesk.ActivityRequest
	activityPatch helpdesk.UpdateActivityRequest
	category      helpdesk.CreateTicketCategoryRequest
	categoryPatch helpdesk.UpdateTicketCategoryRequest
	tag           helpdesk.CreateTicketTagRequest
	tagPatch      helpdesk.UpdateTicketTagRequest
	data          []byte
}

func readHelpdeskInput(opts helpdeskOptions) (helpdeskInput, error) {
	var (
		input  helpdeskInput
		target any
		err    error
	)
	switch opts.action {
	case "create":
		target = &input.create
	case "update":
		target = &input.patch
	case "copy":
		if opts.file != "" {
			target = &input.patch
		}
	case "post":
		target = &input.message
	case "schedule":
		target = &input.activity
	case "update-activity":
		target = &input.activityPatch
	case "create-category":
		target = &input.category
	case "update-category":
		target = &input.categoryPatch
	case "create-tag":
		target = &input.tag
	case "update-tag":
		target = &input.tagPatch
	case "upload":
		if opts.input == "" {
			return input, fmt.Errorf("--input is required")
		}
		input.data, err = os.ReadFile(opts.input)
		if err != nil {
			return input, fmt.Errorf("read attachment: %w", err)
		}
	case "download":
		if opts.output == "" {
			return input, fmt.Errorf("--output is required")
		}
		if _, err = os.Lstat(opts.output); err == nil {
			return input, fmt.Errorf("output path already exists: %s", opts.output)
		} else if !os.IsNotExist(err) {
			return input, fmt.Errorf("check output path: %w", err)
		}
	}
	if target != nil {
		err = decodeHelpdeskFile(opts.file, target)
	}
	return input, err
}

func decodeHelpdeskFile(path string, target any) error {
	var (
		data    []byte
		decoder *json.Decoder
		extra   any
		err     error
	)
	if path == "" {
		return fmt.Errorf("--file is required")
	}
	if data, err = os.ReadFile(path); err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	data = bytes.TrimSpace(data)
	if len(data) == 0 || data[0] != '{' {
		return fmt.Errorf("%s must contain exactly one JSON object", path)
	}
	decoder = json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(target); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	if err = decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("%s must contain exactly one JSON object", path)
	}
	return nil
}
