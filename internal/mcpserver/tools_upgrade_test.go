package mcpserver

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"nfc-studio/internal/config"
)

func callUpgradeTool(t *testing.T, srv *Server, name string, args map[string]any) (toolResult, map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(args)
	res, rpcErr := srv.invokeTool(context.Background(), name, raw)
	if rpcErr != nil {
		t.Fatalf("%s rpcErr = %+v", name, rpcErr)
	}
	var payload map[string]any
	if len(res.Content) > 0 {
		_ = json.Unmarshal([]byte(res.Content[0].Text), &payload)
	}
	return res, payload
}

func TestUpgradeToolsRegistered(t *testing.T) {
	defs := allToolDefinitions()
	want := []string{
		"nfc_write_ntag",
		"nfc_clone_magic_uid",
		"nfc_compose_ndef",
		"nfc_dict_scan",
		"nfc_inspect_emv",
		"nfc_scan_multiprotocol",
	}
	have := make(map[string]bool, len(defs))
	for _, d := range defs {
		have[d.Name] = true
	}
	for _, w := range want {
		if !have[w] {
			t.Fatalf("missing tool %q in allToolDefinitions()", w)
		}
	}
}

func TestMCPWriteSafetyGatesAndSimParity(t *testing.T) {
	srv := New(nil, t.TempDir(), config.Default())

	// 1. nfc_write_ntag without confirm_write must fail.
	res, _ := callUpgradeTool(t, srv, "nfc_write_ntag", map[string]any{
		"start_page": 4,
		"pages_hex":  []string{"01 02 03 04"},
	})
	if !res.IsError {
		t.Fatal("expected error when confirm_write is omitted")
	}

	// 2. Protected page without allow_system_pages must fail.
	res, _ = callUpgradeTool(t, srv, "nfc_write_ntag", map[string]any{
		"start_page":    2,
		"pages_hex":     []string{"00 00 00 00"},
		"confirm_write": true,
	})
	if !res.IsError {
		t.Fatal("expected error on protected page 2")
	}

	// 3. Valid NTAG write in SIM mode succeeds.
	res, out := callUpgradeTool(t, srv, "nfc_write_ntag", map[string]any{
		"start_page":    4,
		"pages_hex":     []string{"03 05 D1 01"},
		"confirm_write": true,
	})
	if res.IsError || out["sim_mode"] != true || out["verified"] != true {
		t.Fatalf("unexpected nfc_write_ntag result: isErr=%v out=%+v", res.IsError, out)
	}

	// 4. nfc_clone_magic_uid requires confirm_write and 4B UID.
	res, _ = callUpgradeTool(t, srv, "nfc_clone_magic_uid", map[string]any{
		"gen":     "gen1a",
		"uid_hex": "DE AD BE EF",
	})
	if !res.IsError {
		t.Fatal("expected error when confirm_write is omitted on magic clone")
	}
	res, out = callUpgradeTool(t, srv, "nfc_clone_magic_uid", map[string]any{
		"gen":           "gen1a",
		"uid_hex":       "DE AD BE EF",
		"confirm_write": true,
	})
	if res.IsError || out["verified"] != true {
		t.Fatalf("unexpected nfc_clone_magic_uid result: isErr=%v out=%+v", res.IsError, out)
	}

	// 5. nfc_compose_ndef: pure encode works without confirm_write; write_to_tag requires confirm_write.
	res, out = callUpgradeTool(t, srv, "nfc_compose_ndef", map[string]any{
		"kind":      "uri",
		"url":       "https://cardputer.cc",
		"save_name": "composed_test",
	})
	if res.IsError || out["saved_name"] != "composed_test.nfc" {
		t.Fatalf("unexpected nfc_compose_ndef result: isErr=%v out=%+v", res.IsError, out)
	}
	res, _ = callUpgradeTool(t, srv, "nfc_compose_ndef", map[string]any{
		"kind":         "text",
		"text":         "hello",
		"write_to_tag": true,
	})
	if !res.IsError {
		t.Fatal("expected error when write_to_tag=true without confirm_write=true")
	}
	res, _ = callUpgradeTool(t, srv, "nfc_compose_ndef", map[string]any{
		"kind":      "text",
		"text":      "hello",
		"save_name": "../escape.nfc",
	})
	if !res.IsError {
		t.Fatal("expected path traversal rejection on save_name")
	}
}

func TestMCPInspectAndMultiProtocolSim(t *testing.T) {
	srv := New(nil, t.TempDir(), config.Default())

	res, out := callUpgradeTool(t, srv, "nfc_dict_scan", map[string]any{
		"max_keys":  16,
		"save_name": "mf1k_audit",
	})
	if res.IsError || out["card_size"] != "1K" || out["saved_path"] == nil {
		t.Fatalf("unexpected nfc_dict_scan result: isErr=%v out=%+v", res.IsError, out)
	}

	res, out = callUpgradeTool(t, srv, "nfc_inspect_emv", map[string]any{})
	if res.IsError || out["read_only"] != true {
		t.Fatalf("unexpected nfc_inspect_emv result: isErr=%v out=%+v", res.IsError, out)
	}
	if !strings.Contains(res.Content[0].Text, "******") {
		t.Fatalf("expected masked PAN in EMV response: %s", res.Content[0].Text)
	}

	for _, proto := range []string{"A", "B", "F", "V"} {
		res, out = callUpgradeTool(t, srv, "nfc_scan_multiprotocol", map[string]any{
			"protocols": []string{proto},
			"attempts":  1,
		})
		if res.IsError || out["found"] != true {
			t.Fatalf("nfc_scan_multiprotocol(%s) failed: isErr=%v out=%+v", proto, res.IsError, out)
		}
	}

	res, _ = callUpgradeTool(t, srv, "nfc_scan_multiprotocol", map[string]any{
		"protocols": []string{"Z"},
	})
	if !res.IsError {
		t.Fatal("expected error for invalid protocol Z")
	}
}
