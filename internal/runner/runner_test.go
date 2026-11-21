package runner

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nfc-studio/internal/config"
)

func TestDefaultTagsDir(t *testing.T) {
	dir := DefaultTagsDir()
	if dir == "" || !strings.HasSuffix(dir, "tags") {
		t.Fatalf("unexpected DefaultTagsDir: %q", dir)
	}
}

func TestRunSimModeContextCancel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()

	var out, errBuf bytes.Buffer
	err := Run(ctx, Config{
		SimMode:    true,
		FBPath:     "", // in-memory canvas for headless test
		TagsDir:    t.TempDir(),
		ConfigPath: filepath.Join(t.TempDir(), "config.json"),
	}, &out, &errBuf)
	if err != nil {
		t.Fatalf("Run(SimMode) returned unexpected error: %v", err)
	}
}

func TestRunProbeInvalidSPI(t *testing.T) {
	var out, errBuf bytes.Buffer
	err := Run(context.Background(), Config{
		ProbeOnly: true,
		SPIPath:   "/dev/nonexistent_spidev_test",
	}, &out, &errBuf)
	if err == nil {
		t.Fatal("expected error probing nonexistent SPI device")
	}
}

func TestRunMCPDeniedWhenDisabled(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	if err := config.Save(cfgPath, config.Settings{MCPEnabled: false, AutoScan: true}); err != nil {
		t.Fatalf("Save config: %v", err)
	}

	var out, errBuf bytes.Buffer
	err := Run(context.Background(), Config{
		MCPMode:    true,
		SimMode:    true,
		ConfigPath: cfgPath,
		TagsDir:    t.TempDir(),
		Stdin:      strings.NewReader(""),
	}, &out, &errBuf)
	if err == nil {
		t.Fatal("expected error when MCP is disabled")
	}
	want := "MCP server is disabled. Enable it in NFC Studio -> SET (Settings) first."
	if err.Error() != want {
		t.Fatalf("got error %q, want %q", err.Error(), want)
	}
}

func TestRunMCPDeniedByDefaultWhenConfigMissing(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "missing.json")
	var out, errBuf bytes.Buffer
	err := Run(context.Background(), Config{
		MCPMode:    true,
		SimMode:    true,
		ConfigPath: cfgPath,
		TagsDir:    t.TempDir(),
		Stdin:      strings.NewReader(""),
	}, &out, &errBuf)
	if err == nil {
		t.Fatal("expected error when config is missing (MCP disabled by default)")
	}
}

func TestRunMCPAllowedWhenEnabled(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	if err := config.Save(cfgPath, config.Settings{MCPEnabled: true, AutoScan: true}); err != nil {
		t.Fatalf("Save config: %v", err)
	}

	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"nfc_get_status"}}`,
	}, "\n") + "\n"

	var out, errBuf bytes.Buffer
	err := Run(context.Background(), Config{
		MCPMode:    true,
		SimMode:    true,
		ConfigPath: cfgPath,
		TagsDir:    t.TempDir(),
		Stdin:      strings.NewReader(input),
	}, &out, &errBuf)
	if err != nil {
		t.Fatalf("Run(MCPMode enabled) unexpected error: %v", err)
	}
	resp := out.String()
	if !strings.Contains(resp, `"protocolVersion":"2024-11-05"`) {
		t.Fatalf("missing initialize response in stdout: %s", resp)
	}
	if !strings.Contains(resp, `\"mcp_enabled\":true`) {
		t.Fatalf("missing nfc_get_status response in stdout: %s", resp)
	}
}

func TestRunMCPCorruptedConfigReturnsError(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "corrupt.json")
	if err := os.WriteFile(cfgPath, []byte("{bad-json"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	var out, errBuf bytes.Buffer
	err := Run(context.Background(), Config{
		MCPMode:    true,
		SimMode:    true,
		ConfigPath: cfgPath,
		Stdin:      strings.NewReader(""),
	}, &out, &errBuf)
	if err == nil {
		t.Fatal("expected error for corrupted config")
	}
}
