package library

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"nfc-studio/internal/nfcfile"
)

func sampleNfcText(uid []byte) string {
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

func TestStoreSeparationPromoteAndDelete(t *testing.T) {
	root := t.TempDir()
	userDir := filepath.Join(root, "tags")
	dbDir := filepath.Join(root, "databases")
	zeroDir := filepath.Join(root, "zerosub")

	writeFile := func(path, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	writeFile(filepath.Join(userDir, "my_badge.nfc"), sampleNfcText([]byte{0x04, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06}))
	writeFile(filepath.Join(dbDir, "amiibo", "link.nfc"), sampleNfcText([]byte{0x04, 0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0x01}))
	writeFile(filepath.Join(dbDir, "amiibo", "mario.nfc"), sampleNfcText([]byte{0x04, 0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0x02}))
	writeFile(filepath.Join(zeroDir, "legacy.nfc"), sampleNfcText([]byte{0x04, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66}))

	store := NewStore(userDir, dbDir, []string{zeroDir})
	packs := store.ListPacks()
	if len(packs) != 3 {
		t.Fatalf("expected 3 packs (amiibo, zerosub, user), got %d: %+v", len(packs), packs)
	}
	if packs[2].ID != PackIDUser || packs[2].ReadOnly {
		t.Fatalf("unexpected user pack: %+v", packs[2])
	}

	// User entries strictly contain only ~/nfc-studio/tags/.
	userEntries := store.ListUserEntries()
	if len(userEntries) != 1 || userEntries[0].Name != "my_badge.nfc" {
		t.Fatalf("unexpected user entries: %+v", userEntries)
	}

	// Promote external amiibo tag to User Library (with collision-safe second copy).
	amiibo, err := store.ListEntries("db:amiibo")
	if err != nil || len(amiibo) != 2 {
		t.Fatalf("ListEntries(db:amiibo) = (%d, %v)", len(amiibo), err)
	}
	p1, err := store.PromoteToUser(amiibo[0])
	if err != nil || p1.Name != "link.nfc" {
		t.Fatalf("PromoteToUser 1 failed: %+v %v", p1, err)
	}
	p2, err := store.PromoteToUser(amiibo[0])
	if err != nil || p2.Name != "link_2.nfc" {
		t.Fatalf("PromoteToUser 2 failed: %+v %v", p2, err)
	}

	// DeleteUserEntry rejects external pack; DeleteDatabaseEntry rejects ext:zerosub.
	if err := store.DeleteUserEntry(amiibo[0]); !errors.Is(err, ErrReadOnlySource) {
		t.Fatalf("expected ErrReadOnlySource, got %v", err)
	}
	zeroEntries, _ := store.ListEntries(PackIDZeroSub)
	if err := store.DeleteDatabaseEntry(zeroEntries[0]); !errors.Is(err, ErrReadOnlySource) {
		t.Fatalf("expected ErrReadOnlySource on ext:zerosub, got %v", err)
	}
	if err := store.DeleteDatabaseEntry(amiibo[1]); err != nil {
		t.Fatalf("DeleteDatabaseEntry(db:amiibo) failed: %v", err)
	}
}

func TestProbeSessionAutoEmuRewind(t *testing.T) {
	ps := NewProbeSession(2000)
	entries := []EntryRef{
		{Name: "a.nfc", PackID: "db:cards", Source: SourceExternal, ReadOnly: true},
		{Name: "b.nfc", PackID: "db:cards", Source: SourceExternal, ReadOnly: true},
		{Name: "c.nfc", PackID: "db:cards", Source: SourceExternal, ReadOnly: true},
	}
	ps.SetCatalog(PackInfo{ID: "db:cards", Name: "cards", Source: SourceExternal, ReadOnly: true}, entries)

	t0 := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	if !ps.Start(t0) || ps.Status != ProbeRunning {
		t.Fatalf("expected ProbeRunning")
	}
	ref, idx, ok := ps.PollDue(t0)
	if !ok || idx != 0 || ref.Name != "a.nfc" {
		t.Fatalf("PollDue(t0) = (%q, %d, %v)", ref.Name, idx, ok)
	}
	ps.OnEmulated(0, t0)
	if _, _, due := ps.PollDue(t0.Add(time.Second)); due {
		t.Fatal("should wait 2s before next entry")
	}
	_, idx, ok = ps.PollDue(t0.Add(2100 * time.Millisecond))
	if !ok || idx != 1 {
		t.Fatalf("expected idx=1 due, got %d %v", idx, ok)
	}
	ps.OnEmulated(1, t0.Add(2100*time.Millisecond))
	ps.Pause()
	if ps.Status != ProbePaused || ps.LastSentIdx != 1 || !ps.IsRecentSent(0) {
		t.Fatalf("unexpected paused state: %+v", ps)
	}
	ps.MoveCursor(-2)
	if ps.CursorIdx != 0 {
		t.Fatalf("expected CursorIdx=0 after rewind, got %d", ps.CursorIdx)
	}
}
