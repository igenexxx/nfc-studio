package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"nfc-studio/internal/config"
	"nfc-studio/internal/st25r3916"
)

type fakeBackend struct {
	mu           sync.Mutex
	info         st25r3916.ChipInfo
	pollTag      *st25r3916.TagInfo
	pollErr      error
	pollCalls    int
	emuErr       error
	emuCalls     int
	fieldOffCnt  int
	hitsToReturn int
}

func (f *fakeBackend) Info() st25r3916.ChipInfo {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.info
}

func (f *fakeBackend) PollISO14443A() (*st25r3916.TagInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pollCalls++
	if f.pollErr != nil {
		return nil, f.pollErr
	}
	return f.pollTag, nil
}

func (f *fakeBackend) StartEmulation(_ []byte, _ [2]byte, _ uint8) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.emuCalls++
	return f.emuErr
}

func (f *fakeBackend) PollTargetHit() (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.hitsToReturn > 0 {
		f.hitsToReturn--
		return true, nil
	}
	return false, nil
}

func (f *fakeBackend) FieldOff() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fieldOffCnt++
	return nil
}

func sendRPC(t *testing.T, srv *Server, ctx context.Context, lines ...string) []rpcResponse {
	t.Helper()
	input := strings.Join(lines, "\n") + "\n"
	var out bytes.Buffer
	if err := srv.ServeStdio(ctx, strings.NewReader(input), &out); err != nil {
		t.Fatalf("ServeStdio() error = %v", err)
	}
	var resps []rpcResponse
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var r rpcResponse
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatalf("unmarshal response %q: %v", line, err)
		}
		resps = append(resps, r)
	}
	return resps
}

func decodeToolPayload(t *testing.T, resp rpcResponse) (map[string]any, bool) {
	t.Helper()
	if resp.Error != nil {
		t.Fatalf("unexpected RPC error: %+v", resp.Error)
	}
	raw, err := json.Marshal(resp.Result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	var tr toolResult
	if err := json.Unmarshal(raw, &tr); err != nil {
		t.Fatalf("unmarshal toolResult: %v", err)
	}
	if len(tr.Content) == 0 {
		t.Fatalf("empty tool content")
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(tr.Content[0].Text), &payload); err != nil {
		t.Fatalf("unmarshal tool text %q: %v", tr.Content[0].Text, err)
	}
	return payload, tr.IsError
}

func TestProtocolLifecycleAndErrors(t *testing.T) {
	srv := New(nil, t.TempDir(), config.Settings{MCPEnabled: true, AutoScan: true})
	srv.extraDirs = nil

	resps := sendRPC(t, srv, context.Background(),
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		``,
		`{"jsonrpc":"2.0","id":2,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/list"}`,
		`{not-valid-json`,
		`{"jsonrpc":"2.0","id":4,"method":"unknown/method"}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call"}`,
		`{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"does_not_exist"}}`,
		`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"nfc_scan_tag","arguments":"bad"}}`,
	)

	// notifications/initialized and blank line produce no response -> 8 responses total.
	if len(resps) != 8 {
		t.Fatalf("got %d responses, want 8", len(resps))
	}

	// 1. initialize
	initRes, ok := resps[0].Result.(map[string]any)
	if !ok || initRes["protocolVersion"] != protocolVersion {
		t.Fatalf("unexpected initialize result: %+v", resps[0])
	}
	// 2. ping
	if resps[1].Error != nil {
		t.Fatalf("ping failed: %+v", resps[1].Error)
	}
	// 3. tools/list -> must expose all 31 tools
	listRes := resps[2].Result.(map[string]any)
	tools := listRes["tools"].([]any)
	if len(tools) != 31 {
		t.Fatalf("expected 31 tools, got %d", len(tools))
	}
	// 4. parse error -32700
	if resps[3].Error == nil || resps[3].Error.Code != errCodeParseError {
		t.Fatalf("expected -32700, got %+v", resps[3].Error)
	}
	// 5. method not found -32601
	if resps[4].Error == nil || resps[4].Error.Code != errCodeMethodNotFound {
		t.Fatalf("expected -32601, got %+v", resps[4].Error)
	}
	// 6. missing params -32602
	if resps[5].Error == nil || resps[5].Error.Code != errCodeInvalidParams {
		t.Fatalf("expected -32602, got %+v", resps[5].Error)
	}
	// 7. unknown tool -32602
	if resps[6].Error == nil || resps[6].Error.Code != errCodeInvalidParams {
		t.Fatalf("expected -32602 for unknown tool, got %+v", resps[6].Error)
	}
	// 8. invalid arguments type -32602
	if resps[7].Error == nil || resps[7].Error.Code != errCodeInvalidParams {
		t.Fatalf("expected -32602 for invalid arguments, got %+v", resps[7].Error)
	}
}

func TestGetStatusAndScanTools(t *testing.T) {
	t.Run("sim_mode", func(t *testing.T) {
		srv := New(nil, t.TempDir(), config.Settings{MCPEnabled: true, AutoScan: false})
		srv.extraDirs = nil

		resps := sendRPC(t, srv, context.Background(),
			`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"nfc_get_status"}}`,
			`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"nfc_scan_tag"}}`,
			`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"nfc_scan_tag","arguments":{"attempts":99}}}`,
		)

		status, isErr := decodeToolPayload(t, resps[0])
		if isErr || status["sim_mode"] != true {
			t.Fatalf("unexpected sim status: %+v isErr=%v", status, isErr)
		}
		scan, isErr := decodeToolPayload(t, resps[1])
		if isErr || scan["found"] != true || scan["device_type"] != "NTAG215" {
			t.Fatalf("unexpected sim scan: %+v isErr=%v", scan, isErr)
		}
		_, isErr = decodeToolPayload(t, resps[2])
		if !isErr {
			t.Fatal("expected error for attempts=99")
		}
	})

	t.Run("hardware_backend_found_notag_and_error", func(t *testing.T) {
		fb := &fakeBackend{
			info: st25r3916.ChipInfo{Identity: 0x2A, Revision: 2, SupplyMV: 3320},
			pollTag: &st25r3916.TagInfo{
				UID:  []byte{0x04, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66},
				ATQA: [2]byte{0x44, 0x00},
				SAK:  0x00,
				Pages: [][]byte{
					{0x04, 0x11, 0x22, 0x77},
					{0x33, 0x44, 0x55, 0x66},
					{0x00, 0x00, 0x00, 0x00},
					{0xE1, 0x10, 0x12, 0x00},
					{0x03, 0x0A, 0xD1, 0x01},
					{0x06, 0x55, 0x04, 'a'},
					{'.', 'b', '.', 'c'},
					{0xFE, 0x00, 0x00, 0x00},
				},
			},
		}
		srv := New(fb, t.TempDir(), config.Settings{MCPEnabled: true, AutoScan: true})
		srv.extraDirs = nil

		resps := sendRPC(t, srv, context.Background(),
			`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"nfc_get_status"}}`,
			`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"nfc_scan_tag","arguments":{"attempts":2}}}`,
		)
		status, isErr := decodeToolPayload(t, resps[0])
		if isErr || status["sim_mode"] != false || status["identity"] != "0x2A" || status["supply_mv"].(float64) != 3320 {
			t.Fatalf("unexpected hw status: %+v", status)
		}
		scan, isErr := decodeToolPayload(t, resps[1])
		if isErr || scan["found"] != true || scan["uid_hex"] != "04 11 22 33 44 55 66" {
			t.Fatalf("unexpected hw scan: %+v", scan)
		}
		ndef := scan["ndef"].(map[string]any)
		if ndef["type"] != "URI" || ndef["payload"] != "https://a.b.c" {
			t.Fatalf("unexpected NDEF: %+v", ndef)
		}

		// ErrNoTag across 2 attempts -> found=false.
		fb.pollErr = st25r3916.ErrNoTag
		fb.pollCalls = 0
		resps = sendRPC(t, srv, context.Background(),
			`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"nfc_scan_tag","arguments":{"attempts":2}}}`,
		)
		scan, isErr = decodeToolPayload(t, resps[0])
		if isErr || scan["found"] != false || fb.pollCalls != 2 {
			t.Fatalf("expected found=false after 2 attempts, got %+v calls=%d", scan, fb.pollCalls)
		}

		// Hardware error -> isError=true.
		fb.pollErr = errors.New("spi bus fault")
		resps = sendRPC(t, srv, context.Background(),
			`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"nfc_scan_tag"}}`,
		)
		_, isErr = decodeToolPayload(t, resps[0])
		if !isErr {
			t.Fatal("expected isError=true on SPI bus fault")
		}
	})
}

func TestParseNDEFTool(t *testing.T) {
	srv := New(nil, t.TempDir(), config.Default())
	srv.extraDirs = nil

	tests := []struct {
		name      string
		req       string
		wantErr   bool
		wantFound bool
		wantURI   string
	}{
		{
			name: "valid_uri",
			req: `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"nfc_parse_ndef","arguments":{"pages_hex":[` +
				`"04 A1 B2 77","C3 D4 E5 F6","11 48 00 00","E1 10 3E 00",` +
				`"03 0A D1 01","06 55 04 61","2E 62 2E 63","FE 00 00 00"]}}}`,
			wantFound: true,
			wantURI:   "https://a.b.c",
		},
		{
			name:      "no_ndef_in_short_pages",
			req:       `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"nfc_parse_ndef","arguments":{"pages_hex":["04 A1 B2 77"]}}}`,
			wantFound: false,
		},
		{
			name:    "empty_pages_error",
			req:     `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"nfc_parse_ndef","arguments":{"pages_hex":[]}}}`,
			wantErr: true,
		},
		{
			name:    "bad_hex_error",
			req:     `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"nfc_parse_ndef","arguments":{"pages_hex":["ZZ ZZ"]}}}`,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resps := sendRPC(t, srv, context.Background(), tc.req)
			payload, isErr := decodeToolPayload(t, resps[0])
			if isErr != tc.wantErr {
				t.Fatalf("isErr=%v want %v payload=%+v", isErr, tc.wantErr, payload)
			}
			if tc.wantErr {
				return
			}
			if payload["found"] != tc.wantFound {
				t.Fatalf("found=%v want %v", payload["found"], tc.wantFound)
			}
			if tc.wantFound {
				ndef := payload["ndef"].(map[string]any)
				if ndef["payload"] != tc.wantURI {
					t.Fatalf("payload=%v want %q", ndef["payload"], tc.wantURI)
				}
			}
		})
	}
}

func TestEmulateTagTool(t *testing.T) {
	fb := &fakeBackend{hitsToReturn: 3}
	srv := New(fb, t.TempDir(), config.Settings{MCPEnabled: true})
	srv.extraDirs = nil
	srv.emuPollInterval = 5 * time.Millisecond

	// 1. Must reject without confirm_rf: true.
	resps := sendRPC(t, srv, context.Background(),
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"nfc_emulate_tag","arguments":{"uid_hex":"DE AD BE EF","duration_ms":500}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"nfc_emulate_tag","arguments":{"uid_hex":"DE AD","confirm_rf":true,"duration_ms":500}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"nfc_emulate_tag","arguments":{"uid_hex":"DE AD BE EF","confirm_rf":true,"duration_ms":100}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"nfc_emulate_tag","arguments":{"uid_hex":"DE AD BE EF","atqa_hex":"04","confirm_rf":true,"duration_ms":500}}}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"nfc_emulate_tag","arguments":{"uid_hex":"DE AD BE EF","sak_hex":"08 09","confirm_rf":true,"duration_ms":500}}}`,
	)
	for i, r := range resps {
		if _, isErr := decodeToolPayload(t, r); !isErr {
			t.Fatalf("case %d: expected isError=true", i)
		}
	}
	if fb.emuCalls != 0 {
		t.Fatalf("expected 0 StartEmulation calls on rejected inputs, got %d", fb.emuCalls)
	}

	// 2. Valid emulation with hardware backend counts hits and calls FieldOff.
	resps = sendRPC(t, srv, context.Background(),
		`{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"nfc_emulate_tag","arguments":{"uid_hex":"DE AD BE EF","atqa_hex":"04 00","sak_hex":"08","duration_ms":500,"confirm_rf":true}}}`,
	)
	payload, isErr := decodeToolPayload(t, resps[0])
	if isErr {
		t.Fatalf("expected success, got error: %+v", payload)
	}
	if payload["reader_hits"].(float64) != 3 {
		t.Fatalf("reader_hits = %v, want 3", payload["reader_hits"])
	}
	if fb.fieldOffCnt != 1 {
		t.Fatalf("expected FieldOff() called once, got %d", fb.fieldOffCnt)
	}

	// 3. Sim mode emulation works cleanly.
	simSrv := New(nil, t.TempDir(), config.Settings{MCPEnabled: true})
	simSrv.extraDirs = nil
	resps = sendRPC(t, simSrv, context.Background(),
		`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"nfc_emulate_tag","arguments":{"uid_hex":"04 A1 B2 C3 D4 E5 F6","duration_ms":500,"confirm_rf":true}}}`,
	)
	payload, isErr = decodeToolPayload(t, resps[0])
	if isErr || payload["sim_mode"] != true {
		t.Fatalf("unexpected sim emulate result: %+v isErr=%v", payload, isErr)
	}
}

func TestSavedTagToolsAndPathTraversal(t *testing.T) {
	tagsDir := t.TempDir()
	extraDir := t.TempDir()
	srv := New(nil, tagsDir, config.Settings{MCPEnabled: true})
	srv.extraDirs = []string{extraDir}

	// Seed a file in extraDir (~/zerosub/nfc equivalent) to ensure list/read find it,
	// while delete MUST NOT delete outside tagsDir.
	extraFile := filepath.Join(extraDir, "readonly_extra.nfc")
	extraContent := "Filetype: Flipper NFC device\nVersion: 4\nDevice type: MIFARE Classic 1K\nUID: DE AD BE EF\nATQA: 04 00\nSAK: 08\n"
	if err := os.WriteFile(extraFile, []byte(extraContent), 0o644); err != nil {
		t.Fatalf("seed extra file: %v", err)
	}

	// 1. Save a tag.
	resps := sendRPC(t, srv, context.Background(),
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"nfc_save_tag","arguments":{"name":"badge_test","device_type":"NTAG215","uid_hex":"04 A1 B2 C3 D4 E5 F6","atqa_hex":"44 00","sak_hex":"00","pages_hex":["04 A1 B2 77","C3 D4 E5 F6","11 48 00 00","E1 10 3E 00","03 0A D1 01","06 55 04 61","2E 62 2E 63","FE 00 00 00"]}}}`,
	)
	saved, isErr := decodeToolPayload(t, resps[0])
	if isErr || saved["saved"] != true || saved["name"] != "badge_test.nfc" {
		t.Fatalf("unexpected save result: %+v isErr=%v", saved, isErr)
	}

	// 2. List saved -> should see both badge_test.nfc and readonly_extra.nfc.
	resps = sendRPC(t, srv, context.Background(),
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"nfc_list_saved"}}`,
	)
	listed, isErr := decodeToolPayload(t, resps[0])
	if isErr || listed["count"].(float64) != 2 {
		t.Fatalf("unexpected list result: %+v isErr=%v", listed, isErr)
	}

	// 3. Read saved tag and verify NDEF decode.
	resps = sendRPC(t, srv, context.Background(),
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"nfc_read_saved","arguments":{"name":"badge_test.nfc"}}}`,
	)
	readRes, isErr := decodeToolPayload(t, resps[0])
	if isErr || readRes["uid_hex"] != "04 A1 B2 C3 D4 E5 F6" {
		t.Fatalf("unexpected read result: %+v isErr=%v", readRes, isErr)
	}
	ndef := readRes["ndef"].(map[string]any)
	if ndef["payload"] != "https://a.b.c" {
		t.Fatalf("unexpected NDEF: %+v", ndef)
	}

	// 4. Adversarial checks: path traversal on save, read, delete; missing confirm on delete; delete extraDir file rejected.
	badReqs := []string{
		`{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"nfc_save_tag","arguments":{"name":"../evil.nfc","uid_hex":"DE AD BE EF"}}}`,
		`{"jsonrpc":"2.0","id":11,"method":"tools/call","params":{"name":"nfc_save_tag","arguments":{"name":"sub/evil.nfc","uid_hex":"DE AD BE EF"}}}`,
		`{"jsonrpc":"2.0","id":12,"method":"tools/call","params":{"name":"nfc_read_saved","arguments":{"name":"../etc/passwd"}}}`,
		`{"jsonrpc":"2.0","id":13,"method":"tools/call","params":{"name":"nfc_read_saved","arguments":{"name":"missing.nfc"}}}`,
		`{"jsonrpc":"2.0","id":14,"method":"tools/call","params":{"name":"nfc_delete_saved","arguments":{"name":"badge_test.nfc","confirm":false}}}`,
		`{"jsonrpc":"2.0","id":15,"method":"tools/call","params":{"name":"nfc_delete_saved","arguments":{"name":"../evil.nfc","confirm":true}}}`,
		`{"jsonrpc":"2.0","id":16,"method":"tools/call","params":{"name":"nfc_delete_saved","arguments":{"name":"readonly_extra.nfc","confirm":true}}}`,
	}
	resps = sendRPC(t, srv, context.Background(), badReqs...)
	for i, r := range resps {
		if _, isErr := decodeToolPayload(t, r); !isErr {
			t.Fatalf("adversarial case %d: expected isError=true", i)
		}
	}
	// Ensure extraFile was NOT deleted.
	if _, err := os.Stat(extraFile); err != nil {
		t.Fatalf("extraFile should still exist: %v", err)
	}

	// 5. Authorized delete inside tagsDir succeeds.
	resps = sendRPC(t, srv, context.Background(),
		`{"jsonrpc":"2.0","id":20,"method":"tools/call","params":{"name":"nfc_delete_saved","arguments":{"name":"badge_test.nfc","confirm":true}}}`,
	)
	delRes, isErr := decodeToolPayload(t, resps[0])
	if isErr || delRes["deleted"] != true {
		t.Fatalf("expected delete success, got %+v isErr=%v", delRes, isErr)
	}
	if _, err := os.Stat(filepath.Join(tagsDir, "badge_test.nfc")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected badge_test.nfc to be deleted, stat err=%v", err)
	}
}

func TestNewChipBackendNil(t *testing.T) {
	if got := NewChipBackend(nil); got != nil {
		t.Fatalf("NewChipBackend(nil) = %v, want nil", got)
	}
}

type stubSPITransport struct {
	mu sync.Mutex
}

func (s *stubSPITransport) Transfer(tx []byte) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rx := make([]byte, len(tx))
	if len(tx) >= 2 && tx[0] == (0x40|0x3F) {
		rx[1] = 0x2A // ST25R3916 rev 2
	}
	if len(tx) >= 2 && tx[0] == (0x40|0x25) {
		rx[1] = 140 // ~3276mV
	}
	if len(tx) >= 5 && tx[0] == (0x40|0x1A) {
		rx[2] = 0x40 // NRE timer expired (ErrNoTag)
	}
	if len(tx) >= 2 && tx[0] == (0x40|0x1D) {
		rx[1] = 0x80 // irqTargetWUA
	}
	return rx, nil
}

func (s *stubSPITransport) Close() error { return nil }

func TestChipBackendAdapter(t *testing.T) {
	chip, err := st25r3916.Open(&stubSPITransport{})
	if err != nil {
		t.Fatalf("st25r3916.Open() error = %v", err)
	}
	defer chip.Close()

	b := NewChipBackend(chip)
	info := b.Info()
	if info.Identity != 0x2A || info.Revision != 2 {
		t.Fatalf("unexpected Info: %+v", info)
	}
	if _, err := b.PollISO14443A(); !errors.Is(err, st25r3916.ErrNoTag) {
		t.Fatalf("PollISO14443A() err = %v, want ErrNoTag", err)
	}
	if err := b.StartEmulation([]byte{0xDE, 0xAD, 0xBE, 0xEF}, [2]byte{0x04, 0x00}, 0x08); err != nil {
		t.Fatalf("StartEmulation() error = %v", err)
	}
	if hit, err := b.PollTargetHit(); err != nil || !hit {
		t.Fatalf("PollTargetHit() = %v, %v; want true, nil", hit, err)
	}
	if err := b.FieldOff(); err != nil {
		t.Fatalf("FieldOff() error = %v", err)
	}
}

func TestEmulateErrorsAndContextCancel(t *testing.T) {
	fb := &fakeBackend{emuErr: errors.New("pt memory fault")}
	srv := New(fb, t.TempDir(), config.Settings{MCPEnabled: true})
	srv.extraDirs = nil
	srv.emuPollInterval = 5 * time.Millisecond

	resps := sendRPC(t, srv, context.Background(),
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"nfc_emulate_tag","arguments":{"uid_hex":"DE AD BE EF","duration_ms":500,"confirm_rf":true}}}`,
	)
	if _, isErr := decodeToolPayload(t, resps[0]); !isErr {
		t.Fatal("expected error when StartEmulation fails")
	}

	// Context cancellation during emulation returns cleanly with cancelled=true and calls FieldOff.
	fb.emuErr = nil
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	var out bytes.Buffer
	req := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"nfc_emulate_tag","arguments":{"uid_hex":"DE AD BE EF","duration_ms":5000,"confirm_rf":true}}}` + "\n"
	_ = srv.ServeStdio(ctx, strings.NewReader(req), &out)
	if !strings.Contains(out.String(), `\"cancelled\":true`) {
		t.Fatalf("expected cancelled emulation response, got: %s", out.String())
	}
	if fb.fieldOffCnt == 0 {
		t.Fatal("expected FieldOff on cancelled emulation")
	}
}

func TestSaveReadDeleteEdgeCases(t *testing.T) {
	tagsDir := t.TempDir()
	// Write a corrupted .nfc file to verify read error and list skip.
	if err := os.WriteFile(filepath.Join(tagsDir, "broken.nfc"), []byte("not a valid nfc file"), 0o644); err != nil {
		t.Fatalf("write broken.nfc: %v", err)
	}
	srv := New(nil, tagsDir, config.Default())
	srv.extraDirs = nil

	resps := sendRPC(t, srv, context.Background(),
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"nfc_read_saved","arguments":{"name":"broken.nfc"}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"nfc_save_tag","arguments":{"name":"ok","uid_hex":""}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"nfc_save_tag","arguments":{"name":"ok","uid_hex":"DE AD BE EF","atqa_hex":"04"}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"nfc_save_tag","arguments":{"name":"ok","uid_hex":"DE AD BE EF","sak_hex":"08 09"}}}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"nfc_save_tag","arguments":{"name":"ok","uid_hex":"DE AD BE EF","pages_hex":["ZZ"]}}}`,
		`{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"nfc_save_tag","arguments":{"name":".","uid_hex":"DE AD BE EF"}}}`,
		`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"nfc_parse_ndef","arguments":"not-obj"}}`,
		`{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"nfc_emulate_tag","arguments":"not-obj"}}`,
		`{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"nfc_read_saved","arguments":"not-obj"}}`,
		`{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"nfc_save_tag","arguments":"not-obj"}}`,
		`{"jsonrpc":"2.0","id":11,"method":"tools/call","params":{"name":"nfc_delete_saved","arguments":"not-obj"}}`,
	)
	for i := 0; i < 6; i++ {
		if _, isErr := decodeToolPayload(t, resps[i]); !isErr {
			t.Fatalf("case %d: expected isError=true", i)
		}
	}
	for i := 6; i < len(resps); i++ {
		if resps[i].Error == nil || resps[i].Error.Code != errCodeInvalidParams {
			t.Fatalf("case %d: expected -32602, got %+v", i, resps[i].Error)
		}
	}
}

func TestNFCModelInfoMCPTool(t *testing.T) {
	srv := New(nil, t.TempDir(), config.Default())
	resps := sendRPC(t, srv, context.Background(),
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"nfc_model_info","arguments":{}}}`,
	)
	payload, isErr := decodeToolPayload(t, resps[0])
	if isErr {
		t.Fatalf("nfc_model_info failed: %+v", payload)
	}
	ind, ok := payload["nfc_inductive"].(map[string]any)
	if !ok || ind["source"] != "embedded" {
		t.Fatalf("unexpected nfc_inductive info: %+v", payload)
	}
	dump, ok := payload["nfc_dump"].(map[string]any)
	if !ok || dump["source"] != "embedded" {
		t.Fatalf("unexpected nfc_dump info: %+v", payload)
	}
}
