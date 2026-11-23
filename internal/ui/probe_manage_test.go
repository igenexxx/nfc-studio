package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nfc-studio/internal/dbsync"
	"nfc-studio/internal/library"
	"nfc-studio/internal/nfcfile"
)

func TestProbeAutoEmuPromoteAndBatchDelete(t *testing.T) {
	root := t.TempDir()
	tagsDir := filepath.Join(root, "tags")
	dbDir := filepath.Join(root, "databases")
	sourcesPath := filepath.Join(root, "sources.json")

	writeTag := func(path string, lastByte byte) {
		t.Helper()
		tag := &nfcfile.NfcTag{
			FileType: "Flipper NFC device", Version: 4, DeviceType: "NTAG215",
			UID: []byte{0x04, 0xA1, 0xB2, 0xC3, 0xD4, 0xE5, lastByte}, ATQA: [2]byte{0x44, 0x00}, SAK: 0x00,
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte(tag.Format()), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	writeTag(filepath.Join(tagsDir, "u1.nfc"), 0x01)
	writeTag(filepath.Join(tagsDir, "u2.nfc"), 0x02)
	writeTag(filepath.Join(dbDir, "amiibo", "a1.nfc"), 0x11)
	writeTag(filepath.Join(dbDir, "amiibo", "a2.nfc"), 0x12)
	_, _ = dbsync.UpsertSource(sourcesPath, dbsync.DatabaseSource{ID: "amiibo", Type: dbsync.SourceGitHub, Owner: "o", Repo: "r", Enabled: true})

	app := NewApp(nil, tagsDir)
	defer app.Close()
	app.SetSourcesPath(sourcesPath)
	app.SetLibraryStore(library.NewStore(tagsDir, dbDir, nil))

	// SAVED strictly contains only User Library tags.
	if len(app.SavedFiles) != 2 {
		t.Fatalf("expected 2 user tags, got %d", len(app.SavedFiles))
	}

	// PROBE Auto-Emu -> Step -> Pause -> Rewind -> Promote to USER.
	app.Mode = ModeProbe
	app.refreshProbePacks()
	app.HandleKey(KeyNav4) // Start Auto-Emu
	app.Step()
	if app.Probe.LastSentIdx != 0 || !app.EmuActive {
		t.Fatalf("expected emulation started at idx 0, last=%d emu=%v", app.Probe.LastSentIdx, app.EmuActive)
	}
	app.HandleKey(KeySpace) // Stop -> Pause
	app.HandleKey(KeyUp)    // Rewind to 0
	app.HandleKey(KeyNav6)  // Promote a1.nfc to USER
	if !strings.Contains(app.ToastText, "Saved: a1.nfc") || len(app.SavedFiles) != 3 {
		t.Fatalf("promote failed: toast=%q saved=%d", app.ToastText, len(app.SavedFiles))
	}

	// PROBE delete marks ignored in .sync-state.json.
	app.HandleKey(KeyMark)
	app.HandleKey(KeyDelete)
	app.HandleKey(KeyDelete)
	if st := dbsync.LoadSyncState(filepath.Join(dbDir, "amiibo")); !st.Ignored["a1.nfc"] {
		t.Fatalf("expected a1.nfc ignored in .sync-state.json")
	}

	// SET -> Manage DB Sources -> batch delete source + dir (Expert Mode row 4).
	app.Settings.ExpertMode = true
	app.Mode = ModeSettings
	app.SettingsIdx = 4
	app.HandleKey(KeyEnter)
	if !app.ShowSources || len(app.SourceRows) != 1 {
		t.Fatalf("expected ShowSources with 1 row, got %v %d", app.ShowSources, len(app.SourceRows))
	}
	c, _ := NewCanvas("")
	app.Render(c)
	app.HandleKey(KeyNav6)
	app.HandleKey(KeyNav6)
	if len(app.SourceRows) != 0 {
		t.Fatalf("expected 0 sources after ALL delete, got %d", len(app.SourceRows))
	}
}
