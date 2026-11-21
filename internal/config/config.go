package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Settings holds persisted user preferences for NFC Studio.
type Settings struct {
	MCPEnabled bool `json:"mcp_enabled"`
	AutoScan   bool `json:"auto_scan"`
}

// Default returns the safe default settings (MCP disabled by default, auto-scan enabled).
func Default() Settings {
	return Settings{
		MCPEnabled: false,
		AutoScan:   true,
	}
}

// DefaultPath returns $HOME/.config/nfc-studio/config.json, falling back to /tmp/nfc-studio/config.json.
func DefaultPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "/tmp/nfc-studio/config.json"
	}
	return filepath.Join(home, ".config", "nfc-studio", "config.json")
}

// Load reads Settings from path. If the file does not exist, Default() is returned with a nil error.
func Load(path string) (Settings, error) {
	if path == "" {
		path = DefaultPath()
	}
	s := Default()
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return s, nil
		}
		return s, fmt.Errorf("config: read %s: %w", path, err)
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return Default(), fmt.Errorf("config: decode %s: %w", path, err)
	}
	return s, nil
}

// Save writes Settings atomically to path with 0700 parent directory and 0600 file permissions.
func Save(path string, s Settings) error {
	if path == "" {
		path = DefaultPath()
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("config: create dir %s: %w", dir, err)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("config: encode settings: %w", err)
	}
	data = append(data, '\n')

	tmpName, err := writeAtomicTemp(dir, data)
	if err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("config: rename temp file: %w", err)
	}
	return nil
}

func writeAtomicTemp(dir string, data []byte) (string, error) {
	tmp, err := os.CreateTemp(dir, ".config-*.tmp")
	if err != nil {
		return "", fmt.Errorf("config: create temp file: %w", err)
	}
	tmpName := tmp.Name()
	fail := func(op string, err error) (string, error) {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return "", fmt.Errorf("config: %s temp file: %w", op, err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		return fail("chmod", err)
	}
	if _, err := tmp.Write(data); err != nil {
		return fail("write", err)
	}
	if err := tmp.Sync(); err != nil {
		return fail("sync", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return "", fmt.Errorf("config: close temp file: %w", err)
	}
	return tmpName, nil
}
