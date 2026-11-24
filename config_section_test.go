package odoo

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadClientSectionSelectsNamedConnection(t *testing.T) {
	var (
		path   string
		client *Client
		err    error
	)

	path = filepath.Join(t.TempDir(), "connections.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
odoo:
  base_url: https://default.example
  api_key: default-key
meiyuan_test:
  base_url: https://meiyuan.example
  api_key: meiyuan-key
  db: meiyuan-db
  max_retries: 2
jubao_test:
  base_url: https://jubao.example
  api_key: jubao-key
  db: jubao-db
`), 0o600))

	client, err = LoadClientSection(path, "meiyuan_test")
	require.NoError(t, err)
	assert.Equal(t, "https://meiyuan.example", client.BaseURL)
	assert.Equal(t, "meiyuan-key", client.APIKey)
	assert.Equal(t, "meiyuan-db", client.DB)
	assert.Equal(t, 2, client.Retry.MaxRetries)

	client, err = LoadClientSection(path, "jubao_test")
	require.NoError(t, err)
	assert.Equal(t, "https://jubao.example", client.BaseURL)
	assert.Equal(t, "jubao-key", client.APIKey)
	assert.Equal(t, "jubao-db", client.DB)

	client, err = LoadClient(path)
	require.NoError(t, err)
	assert.Equal(t, "https://default.example", client.BaseURL)
}

func TestLoadClientSectionRejectsMissingAndInvalidConnection(t *testing.T) {
	var (
		path string
		err  error
	)

	path = filepath.Join(t.TempDir(), "connections.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
invalid:
  base_url: https://example.com
  api_key: secret-should-not-appear
  max_retries: 11
`), 0o600))

	_, err = LoadClientSection(path, "missing")
	assert.ErrorIs(t, err, ErrInvalidConfig)
	assert.ErrorContains(t, err, "missing")

	_, err = LoadClientSection(path, "invalid")
	assert.ErrorIs(t, err, ErrInvalidConfig)
	assert.ErrorContains(t, err, "invalid.max_retries")
	assert.NotContains(t, err.Error(), "secret-should-not-appear")
}

func TestLoadClientKeepsMissingDefaultSectionError(t *testing.T) {
	var (
		path string
		err  error
	)

	path = filepath.Join(t.TempDir(), "connections.yaml")
	require.NoError(t, os.WriteFile(path, []byte("other:\n  api_key: k\n"), 0o600))
	_, err = LoadClient(path)
	assert.ErrorContains(t, err, "odoo.base_url must be an absolute HTTP or HTTPS URL")
}
