package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// decodeFile reads exactly one JSON object from path into v, rejecting unknown fields.
func decodeFile(path string, v any) error {
	var (
		err     error
		f       *os.File
		decoder *json.Decoder
		extra   any
	)

	if path == "" {
		return fmt.Errorf("-file is required")
	}
	if f, err = os.Open(path); err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	decoder = json.NewDecoder(f)
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(v); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	if err = decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("%s must contain exactly one JSON object", path)
	}
	return nil
}

// printJSON writes v as indented JSON.
func printJSON(out io.Writer, v any) error {
	var encoder = json.NewEncoder(out)

	encoder.SetIndent("", "  ")
	return encoder.Encode(v)
}
