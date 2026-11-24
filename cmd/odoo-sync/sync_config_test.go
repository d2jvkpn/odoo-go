package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/d2jvkpn/odoo-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSyncCommandsSelectConnectionsFromOneYAMLFile(t *testing.T) {
	var (
		sourceWrites []string
		targetWrites []string
		config       string
		file         string
		out          bytes.Buffer
		data         []byte
		source       string
		target       string
		err          error
	)

	source = fakeOdoo(t, partnerSourceRoutes(), &sourceWrites)
	target = fakeOdoo(t, partnerTargetRoutes(`[]`), &targetWrites)
	config = filepath.Join(t.TempDir(), "connections.yaml")
	require.NoError(t, os.WriteFile(config, []byte(
		"source:\n  base_url: "+source+"\n  api_key: source-key\n"+
			"target:\n  base_url: "+target+"\n  api_key: target-key\n",
	), 0o600))
	file = filepath.Join(t.TempDir(), "partners.json")

	require.NoError(t, run(context.Background(), []string{
		"partners", "export", "-source", config + ":source", "-out", file,
	}, &out))
	data, err = os.ReadFile(file)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"filename": "`+config+`:source"`)

	out.Reset()
	require.NoError(t, run(context.Background(), []string{
		"partners", "check", "-in", file, "-target", config + ":target",
	}, &out))
	assert.Contains(t, out.String(), "target preflight passed")
	assert.Empty(t, sourceWrites)
	assert.Empty(t, targetWrites)
}

func TestLoadSyncClientRejectsIncompleteSelector(t *testing.T) {
	var err error

	_, err = loadSyncClient(":source")
	assert.ErrorIs(t, err, odoo.ErrInvalidConfig)
	_, err = loadSyncClient("connections.yaml:")
	assert.ErrorIs(t, err, odoo.ErrInvalidConfig)
}
