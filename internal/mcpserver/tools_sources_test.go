package mcpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nfc-studio/internal/config"
	"nfc-studio/internal/dbsync"
	"nfc-studio/internal/nfcfile"
)

func TestMCPDatabasesSourcesPromoteAndDelete(t *testing.T) {
	tag := &nfcfile.NfcTag{
		FileType: "Flipper NFC device", Version: 4, DeviceType: "NTAG215",
		UID: []byte{0x04, 0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0x01}, ATQA: [2]byte{0x44, 0x00}, SAK: 0x00,
	}
	nfcText := tag.Format()

	var srvURL string
	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/manifest.json":
			w.Header().Set("ETag", `"m-1"`)
			if r.Header.Get("If-None-Match") == `"m-1"` {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"files": []map[string]string{{"name": "link.nfc", "url": srvURL + "/link.nfc"}},
			})
		case "/link.nfc":
			_, _ = w.Write([]byte(nfcText))
		}
	}))
	defer httpSrv.Close()
	srvURL = httpSrv.URL

	root := t.TempDir()
	tagsDir := filepath.Join(root, "tags")
	dbDir := filepath.Join(root, "databases")
	sourcesPath := filepath.Join(root, "sources.json")

	srv := New(nil, tagsDir, config.Default())
	srv.SetDatabaseConfig(dbDir, sourcesPath, &dbsync.Syncer{Client: httpSrv.Client(), DatabasesDir: dbDir})

	res, rpcErr := srv.invokeTool(context.Background(), "nfc_add_source", json.RawMessage(`{"id":"amiibo","type":"manifest","url":"`+srvURL+`/manifest.json"}`))
	if rpcErr != nil || res.IsError || !strings.Contains(res.Content[0].Text, `"added":true`) {
		t.Fatalf("nfc_add_source failed: %+v %+v", res, rpcErr)
	}
	res, rpcErr = srv.invokeTool(context.Background(), "nfc_sync_sources", json.RawMessage(`{"id":"amiibo"}`))
	if rpcErr != nil || res.IsError || !strings.Contains(res.Content[0].Text, `"added":1`) {
		t.Fatalf("nfc_sync_sources failed: %+v %+v", res, rpcErr)
	}
	res, rpcErr = srv.invokeTool(context.Background(), "nfc_promote_to_user", json.RawMessage(`{"pack":"db:amiibo","name":"link.nfc"}`))
	if rpcErr != nil || res.IsError || !strings.Contains(res.Content[0].Text, `"promoted":true`) {
		t.Fatalf("nfc_promote_to_user failed: %+v %+v", res, rpcErr)
	}
	if _, err := os.Stat(filepath.Join(tagsDir, "link.nfc")); err != nil {
		t.Fatalf("expected promoted link.nfc in tagsDir: %v", err)
	}
	res, rpcErr = srv.invokeTool(context.Background(), "nfc_delete_pack_files", json.RawMessage(`{"pack":"db:amiibo","names":["link.nfc"],"confirm":true}`))
	if rpcErr != nil || res.IsError || !strings.Contains(res.Content[0].Text, `"deleted":1`) {
		t.Fatalf("nfc_delete_pack_files failed: %+v %+v", res, rpcErr)
	}
	res, rpcErr = srv.invokeTool(context.Background(), "nfc_remove_source", json.RawMessage(`{"ids":["amiibo"],"confirm":true,"delete_files":true}`))
	if rpcErr != nil || res.IsError || !strings.Contains(res.Content[0].Text, `"deleted_files":true`) {
		t.Fatalf("nfc_remove_source failed: %+v %+v", res, rpcErr)
	}
}
