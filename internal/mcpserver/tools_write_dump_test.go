package mcpserver

import (
	"os"
	"path/filepath"
	"testing"

	"nfc-studio/internal/config"
	"nfc-studio/internal/nfcfile"
)

func TestMCPWriteSavedTagAndGen1ATools(t *testing.T) {
	tagsDir := t.TempDir()
	srv := New(nil, tagsDir, config.Default())

	// Seed an NTAG215, MIFARE Classic 1K, and unsupported FeliCa .nfc file.
	ntag := &nfcfile.NfcTag{
		FileType: "Flipper NFC device", Version: 4, DeviceType: "NTAG215",
		UID: []byte{0x04, 1, 2, 3, 4, 5, 6}, ATQA: [2]byte{0x44, 0x00}, SAK: 0x00,
		Pages: [][]byte{
			{0x04, 1, 2, 0x77}, {3, 4, 5, 6}, {0, 0, 0, 0}, {0xE1, 0x10, 0x3E, 0x00},
			{0x03, 0x05, 0xD1, 0x01}, {0x01, 0x55, 0x04, 'a'}, {0xFE, 0, 0, 0},
		},
	}
	mfPages := make([][]byte, 8)
	for i := range mfPages {
		mfPages[i] = make([]byte, 16)
		mfPages[i][0] = byte(i)
	}
	mf := &nfcfile.NfcTag{
		FileType: "Flipper NFC device", Version: 4, DeviceType: "Mifare Classic",
		UID: []byte{0xDE, 0xAD, 0xBE, 0xEF}, ATQA: [2]byte{0x04, 0x00}, SAK: 0x08,
		Pages: mfPages, MultiMeta: nfcfile.MultiMeta{Protocol: "NFC-A", MifareType: "1K"},
	}
	felica := &nfcfile.NfcTag{
		FileType: "Flipper NFC device", Version: 4, DeviceType: "FeliCa",
		UID: []byte{1, 2, 3, 4, 5, 6, 7, 8}, MultiMeta: nfcfile.MultiMeta{Protocol: "NFC-F"},
	}
	_ = os.WriteFile(filepath.Join(tagsDir, "ntag.nfc"), []byte(ntag.Format()), 0o644)
	_ = os.WriteFile(filepath.Join(tagsDir, "mf1k.nfc"), []byte(mf.Format()), 0o644)
	_ = os.WriteFile(filepath.Join(tagsDir, "felica.nfc"), []byte(felica.Format()), 0o644)

	// 1. All 31 tools registered (including nfc_probe_blank, nfc_write_saved_tag, nfc_write_mifare_gen1a).
	defs := allToolDefinitions()
	if len(defs) != 31 {
		t.Fatalf("allToolDefinitions() count = %d, want 31", len(defs))
	}

	// 1b. nfc_probe_blank with source_name checks compatibility cleanly in SIM mode.
	res, out := callUpgradeTool(t, srv, "nfc_probe_blank", map[string]any{"source_name": "ntag.nfc"})
	if res.IsError || out["compat_level"] != "exact" {
		t.Fatalf("nfc_probe_blank(ntag.nfc) = isErr=%v out=%+v", res.IsError, out)
	}
	res, out = callUpgradeTool(t, srv, "nfc_probe_blank", map[string]any{"source_name": "mf1k.nfc"})
	if res.IsError || out["compat_level"] != "incompatible" {
		t.Fatalf("nfc_probe_blank(mf1k.nfc on SIM NTAG215) = isErr=%v out=%+v", res.IsError, out)
	}

	// 2. Safety gates: missing confirm_write and path traversal rejected.
	res, _ = callUpgradeTool(t, srv, "nfc_write_saved_tag", map[string]any{"name": "ntag.nfc"})
	if !res.IsError {
		t.Fatal("expected error when confirm_write omitted")
	}
	res, _ = callUpgradeTool(t, srv, "nfc_write_saved_tag", map[string]any{"name": "../etc/passwd", "confirm_write": true})
	if !res.IsError {
		t.Fatal("expected path traversal rejection")
	}

	// 3. Unsupported FeliCa rejected cleanly.
	res, _ = callUpgradeTool(t, srv, "nfc_write_saved_tag", map[string]any{"name": "felica.nfc", "confirm_write": true})
	if !res.IsError {
		t.Fatal("expected unsupported FeliCa rejection")
	}

	// 4. NTAG saved tag write in SIM mode writes user pages P04..P06 (3 pages).
	res, out = callUpgradeTool(t, srv, "nfc_write_saved_tag", map[string]any{"name": "ntag.nfc", "confirm_write": true})
	if res.IsError || out["strategy"] != "ntag_pages" || out["pages_written"] != float64(3) {
		t.Fatalf("unexpected NTAG saved write result: isErr=%v out=%+v", res.IsError, out)
	}

	// 5. MIFARE Classic saved tag write across gen1a / gen2 / regular modes.
	for _, gen := range []string{"gen1a", "gen2", "regular"} {
		res, out = callUpgradeTool(t, srv, "nfc_write_saved_tag", map[string]any{
			"name": "mf1k.nfc", "magic_gen": gen, "confirm_write": true,
		})
		if res.IsError || out["verified"] != true {
			t.Fatalf("saved write(%s) failed: isErr=%v out=%+v", gen, res.IsError, out)
		}
	}

	// 6. nfc_write_mifare_gen1a rejects sector trailer block 3 unless include_trailers=true.
	res, _ = callUpgradeTool(t, srv, "nfc_write_mifare_gen1a", map[string]any{
		"uid_hex":       "DE AD BE EF",
		"blocks":        []map[string]any{{"block": 3, "hex": "FF FF FF FF FF FF FF 07 80 69 FF FF FF FF FF FF"}},
		"confirm_write": true,
	})
	if !res.IsError {
		t.Fatal("expected sector trailer rejection without include_trailers=true")
	}
	res, out = callUpgradeTool(t, srv, "nfc_write_mifare_gen1a", map[string]any{
		"uid_hex":          "DE AD BE EF",
		"blocks":           []map[string]any{{"block": 1, "hex": "00 11 22 33 44 55 66 77 88 99 AA BB CC DD EE FF"}},
		"include_trailers": false,
		"confirm_write":    true,
	})
	if res.IsError || out["blocks_written"] != float64(2) {
		t.Fatalf("unexpected nfc_write_mifare_gen1a result: isErr=%v out=%+v", res.IsError, out)
	}
}
