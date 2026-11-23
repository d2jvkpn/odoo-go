package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckCommand(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.json")
	require.NoError(t, writeCatalogFile(path, sampleFile()))

	var out bytes.Buffer
	require.NoError(t, run(context.Background(), []string{"products", "check", "-in", path}, &out))
	assert.Contains(t, out.String(), "ok: 2 templates, 3 variants")
}

func TestCheckCommandReportsProblems(t *testing.T) {
	f := sampleFile()
	f.Templates[1].Variants[0].DefaultCode = "A-1"
	path := filepath.Join(t.TempDir(), "catalog.json")
	require.NoError(t, writeCatalogFile(path, f))

	var out bytes.Buffer
	err := run(context.Background(), []string{"products", "check", "-in", path}, &out)
	require.Error(t, err)
	assert.Contains(t, out.String(), `default_code "A-1"`)
}

func TestRunRejectsUnknownCommandAndPrintsUsage(t *testing.T) {
	var out bytes.Buffer
	assert.Error(t, run(context.Background(), []string{"nope"}, &out))
	for _, old := range []string{"export", "import", "check"} {
		assert.ErrorContains(t, run(context.Background(), []string{old}, &out), "unknown command")
	}
	require.NoError(t, run(context.Background(), []string{"-h"}, &out))
	assert.Contains(t, out.String(), "Usage:")
}

func TestCatalogGroupNeedsACommand(t *testing.T) {
	var out bytes.Buffer
	assert.ErrorContains(t, run(context.Background(), []string{"products"}, &out), "missing command")
	assert.ErrorContains(t, run(context.Background(), []string{"boms", "nope"}, &out), "unknown command")
}

func TestCheckCommandRejectsTheWrongSection(t *testing.T) {
	dir := t.TempDir()
	products := filepath.Join(dir, "products.json")
	require.NoError(t, writeCatalogFile(products, sampleFile()))
	boms := filepath.Join(dir, "boms.json")
	f := sampleFile()
	f.Templates, f.BOMs, f.Section = nil, []FileBOM{sampleBOM()}, "boms"
	require.NoError(t, writeCatalogFile(boms, f))

	err := run(context.Background(), []string{"boms", "check", "-in", products}, &bytes.Buffer{})
	assert.ErrorContains(t, err, "the file has no boms section")
	err = run(context.Background(), []string{"products", "check", "-in", boms}, &bytes.Buffer{})
	assert.ErrorContains(t, err, "the file has no products section")
}

func TestCatalogCommandsRejectFormatOneBeforeCheckingSection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"format":1,"include":["products"],"templates":[]}`), 0o600))
	for _, command := range []string{"check", "import"} {
		var out bytes.Buffer
		args := []string{"products", command, "-in", path}
		if command == "import" {
			args = append(args, "-target", "unused.yaml")
		}
		err := run(context.Background(), args, &out)
		require.ErrorContains(t, err, "problems found")
		assert.Contains(t, out.String(), "unsupported format 1: this tool reads format 2")
	}
}

func TestGroupHelpPrintsUsage(t *testing.T) {
	for _, group := range []string{"products", "boms", "partners"} {
		for _, h := range []string{"-h", "--help"} {
			var out bytes.Buffer
			require.NoError(t, run(context.Background(), []string{group, h}, &out), group+" "+h)
			assert.Equal(t, usage, out.String())
		}
	}
}
