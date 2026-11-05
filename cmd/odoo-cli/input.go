package main

import (
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
)

// errNotOneObject reports a JSON input that holds more than one value.
var errNotOneObject = errors.New("input must contain exactly one JSON object")

// decodeStrict reads exactly one JSON value from r into v, rejecting unknown members. Trailing data
// returns errNotOneObject.
func decodeStrict(r io.Reader, v any) error {
	var decoder = jsontext.NewDecoder(r)

	if err := jsonv2.UnmarshalDecode(decoder, v, jsonv2.RejectUnknownMembers(true)); err != nil {
		return err
	}
	if _, err := decoder.ReadToken(); err != io.EOF {
		return errNotOneObject
	}
	return nil
}

// decodeFile reads exactly one JSON object from path into v, rejecting unknown fields.
func decodeFile(path string, v any) error {
	var (
		err error
		f   *os.File
	)

	if path == "" {
		return fmt.Errorf("-file is required")
	}
	if f, err = os.Open(path); err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	if err = decodeStrict(f, v); errors.Is(err, errNotOneObject) {
		return fmt.Errorf("%s must contain exactly one JSON object", path)
	} else if err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

// printJSON writes v as indented JSON with sorted map keys and a trailing newline.
func printJSON(out io.Writer, v any) error {
	if err := jsonv2.MarshalWrite(out, v, jsontext.WithIndent("  "), jsonv2.Deterministic(true)); err != nil {
		return err
	}
	_, err := io.WriteString(out, "\n")
	return err
}
