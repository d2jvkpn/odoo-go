package main

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckCommand(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.json")
	require.NoError(t, writeCatalogFile(path, sampleFile()))

	var out bytes.Buffer
	require.NoError(t, run(context.Background(), []string{"check", "-in", path}, &out))
	assert.Contains(t, out.String(), "ok: 2 templates, 3 variants")
}

func TestCheckCommandReportsProblems(t *testing.T) {
	f := sampleFile()
	f.Templates[1].Variants[0].DefaultCode = "A-1"
	path := filepath.Join(t.TempDir(), "catalog.json")
	require.NoError(t, writeCatalogFile(path, f))

	var out bytes.Buffer
	err := run(context.Background(), []string{"check", "-in", path}, &out)
	require.Error(t, err)
	assert.Contains(t, out.String(), `default_code "A-1"`)
}

func TestRunRejectsUnknownCommandAndPrintsUsage(t *testing.T) {
	var out bytes.Buffer
	assert.Error(t, run(context.Background(), []string{"nope"}, &out))
	require.NoError(t, run(context.Background(), []string{"-h"}, &out))
	assert.Contains(t, out.String(), "Usage:")
}
