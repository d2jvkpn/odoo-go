package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHelpdeskHelp(t *testing.T) {
	for _, action := range []string{
		"", "list", "get", "count", "create", "update", "archive", "unarchive", "delete", "copy",
		"stages", "teams", "channels", "categories", "tags", "members", "team-stages",
		"messages", "post", "attachments", "upload", "download", "delete-attachment",
		"followers", "subscribe", "unsubscribe", "activities", "activity-types", "schedule",
		"update-activity", "complete-activity", "delete-activity", "create-category", "update-category",
		"delete-category", "create-tag", "update-tag", "delete-tag",
	} {
		t.Run(action, func(t *testing.T) {
			var (
				out  bytes.Buffer
				args = []string{"helpdesk"}
			)
			if action != "" {
				args = append(args, action)
			}
			args = append(args, "-h")
			require.NoError(t, run(context.Background(), args, &out))
			assert.Contains(t, out.String(), "helpdesk")
		})
	}
}

func TestHelpdeskRejectsArgumentsBeforeLoadingConfig(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"get"}, "id must be positive"},
		{[]string{"list", "--limit", "0"}, "limit must be positive"},
		{[]string{"messages", "--id", "1", "--offset", "-1"}, "offset"},
		{[]string{"list", "--domain", "null"}, "JSON array"},
		{[]string{"list", "extra"}, "unexpected positional"},
		{[]string{"create"}, "--file is required"},
		{[]string{"members"}, "team-id must be positive"},
		{[]string{"subscribe", "--id", "1", "--partner-ids", "0"}, "positive"},
		{[]string{"download", "--id", "1", "--attachment-id", "2"}, "--output is required"},
		{[]string{"delete-attachment", "--id", "1"}, "attachment-id must be positive"},
		{[]string{"complete-activity", "--id", "1"}, "activity-id must be positive"},
		{[]string{"nonsense"}, "unknown"},
	} {
		var err error
		err = run(context.Background(), append([]string{"--config", "/missing.yaml", "helpdesk"}, tc.args...), io.Discard)
		require.Error(t, err, "%v", tc.args)
		assert.Contains(t, err.Error(), tc.want, "%v", tc.args)
	}
}

func TestHelpdeskConfigAndQuery(t *testing.T) {
	for _, mode := range []string{"global", "action", "override"} {
		t.Run(mode, func(t *testing.T) {
			var (
				server *httptest.Server
				config string
				args   []string
				out    bytes.Buffer
				calls  int
			)
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var params map[string]any
				calls++
				assert.Equal(t, "/json/2/helpdesk.ticket/search_read", r.URL.Path)
				assert.Equal(t, "bearer test-key", r.Header.Get("Authorization"))
				assert.NoError(t, json.NewDecoder(r.Body).Decode(&params))
				assert.Equal(t, float64(2), params["limit"])
				assert.Equal(t, float64(3), params["offset"])
				assert.Equal(t, "id asc", params["order"])
				assert.Equal(t, map[string]any{"active_test": false}, params["context"])
				fmt.Fprint(w, `[{"id":7,"name":"Archived","active":false,"priority":false}]`)
			}))
			t.Cleanup(server.Close)
			config = writeFile(t, "config.yaml", configYAML(server.URL, ""))
			args = []string{"helpdesk", "list", "--limit", "2", "--offset", "3", "--order", "id asc", "--include-archived"}
			if mode == "global" {
				args = append([]string{"--config", config}, args...)
			} else {
				args = append(args, "--config", config)
				if mode == "override" {
					args = append([]string{"--config", "/missing.yaml"}, args...)
				}
			}
			require.NoError(t, run(context.Background(), args, &out))
			assert.Equal(t, 1, calls)
			assert.True(t, json.Valid(out.Bytes()))
			assert.Contains(t, out.String(), `"Archived"`)
		})
	}
}

func TestDetectMimeTypeStripsParameters(t *testing.T) {
	assert.Equal(t, "application/json", detectMimeType("a.json", []byte("{}")))
	assert.Equal(t, "text/plain", detectMimeType("notes", []byte("hello")))
	assert.Equal(t, "application/octet-stream", detectMimeType("blob", []byte{0, 1, 2, 3}))
}
