package dbsync

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"nfc-studio/internal/nfcfile"
)

func validNfcPayload(uid []byte) string {
	tag := &nfcfile.NfcTag{
		FileType:   "Flipper NFC device",
		Version:    4,
		DeviceType: "NTAG215",
		UID:        uid,
		ATQA:       [2]byte{0x44, 0x00},
		SAK:        0x00,
	}
	return tag.Format()
}

func TestGitHubDeltaSyncIgnoredAndDeletePackDir(t *testing.T) {
	var downloads atomic.Int32
	etag := `"v1"`
	tree := map[string]any{
		"tree": []map[string]string{
			{"path": "NFC/Amiibo/link.nfc", "type": "blob", "sha": "sha-1"},
			{"path": "NFC/Amiibo/mario.nfc", "type": "blob", "sha": "sha-2"},
		},
	}
	bodies := map[string]string{
		"/o/r/main/NFC/Amiibo/link.nfc":  validNfcPayload([]byte{0x04, 0x01, 0x02, 0x03, 0x04, 0x05, 0x01}),
		"/o/r/main/NFC/Amiibo/mario.nfc": validNfcPayload([]byte{0x04, 0x01, 0x02, 0x03, 0x04, 0x05, 0x02}),
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/o/r/git/trees/main" {
			if r.Header.Get("If-None-Match") == etag {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Header().Set("ETag", etag)
			_ = json.NewEncoder(w).Encode(tree)
			return
		}
		if b, ok := bodies[r.URL.Path]; ok {
			downloads.Add(1)
			_, _ = w.Write([]byte(b))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	dbDir := t.TempDir()
	syncer := &Syncer{Client: srv.Client(), DatabasesDir: dbDir, GitHubAPIBase: srv.URL, GitHubRawBase: srv.URL}
	src := DatabaseSource{ID: "amiibo", Type: SourceGitHub, Owner: "o", Repo: "r", Branch: "main", Subpath: "NFC/Amiibo", Enabled: true}

	r1 := syncer.SyncSource(context.Background(), src)
	if r1.Err != "" || r1.Added != 2 || downloads.Load() != 2 {
		t.Fatalf("r1 unexpected: %+v dl=%d", r1, downloads.Load())
	}
	r2 := syncer.SyncSource(context.Background(), src)
	if !r2.NotModified || downloads.Load() != 2 {
		t.Fatalf("r2 expected 304, got %+v dl=%d", r2, downloads.Load())
	}

	// Mark mario.nfc ignored after deleting it; re-sync with new ETag must NOT re-download it.
	_ = os.Remove(filepath.Join(dbDir, "amiibo", "mario.nfc"))
	_ = MarkIgnored(filepath.Join(dbDir, "amiibo"), []string{"mario.nfc"})
	etag = `"v2"`
	r3 := syncer.SyncSource(context.Background(), src)
	if r3.Added != 0 || r3.Skipped != 2 {
		t.Fatalf("r3 expected skipped=2, got %+v", r3)
	}
	if fileExists(filepath.Join(dbDir, "amiibo", "mario.nfc")) {
		t.Fatal("ignored mario.nfc must not be re-downloaded")
	}

	if err := DeletePackDir(dbDir, "amiibo"); err != nil {
		t.Fatalf("DeletePackDir failed: %v", err)
	}
}
