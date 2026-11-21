package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefault(t *testing.T) {
	s := Default()
	if s.MCPEnabled {
		t.Fatal("expected MCPEnabled=false by default")
	}
	if !s.AutoScan {
		t.Fatal("expected AutoScan=true by default")
	}
}

func TestDefaultPath(t *testing.T) {
	p := DefaultPath()
	if !strings.HasSuffix(p, filepath.Join("nfc-studio", "config.json")) {
		t.Fatalf("unexpected DefaultPath: %q", p)
	}
}

func TestLoadMissingReturnsDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist", "config.json")
	s, err := Load(path)
	if err != nil {
		t.Fatalf("Load(missing) returned unexpected error: %v", err)
	}
	want := Default()
	if s != want {
		t.Fatalf("Load(missing) = %+v, want %+v", s, want)
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		in   Settings
	}{
		{
			name: "mcp_enabled_autoscan_off",
			in:   Settings{MCPEnabled: true, AutoScan: false},
		},
		{
			name: "defaults",
			in:   Default(),
		},
		{
			name: "both_enabled",
			in:   Settings{MCPEnabled: true, AutoScan: true},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "nested", "cfg")
			path := filepath.Join(dir, "config.json")

			if err := Save(path, tc.in); err != nil {
				t.Fatalf("Save() error = %v", err)
			}

			// Verify file permissions are 0600 and directory is 0700.
			dirInfo, err := os.Stat(dir)
			if err != nil {
				t.Fatalf("Stat(dir) error = %v", err)
			}
			if perm := dirInfo.Mode().Perm(); perm != 0o700 {
				t.Fatalf("dir perm = %04o, want 0700", perm)
			}
			fileInfo, err := os.Stat(path)
			if err != nil {
				t.Fatalf("Stat(file) error = %v", err)
			}
			if perm := fileInfo.Mode().Perm(); perm != 0o600 {
				t.Fatalf("file perm = %04o, want 0600", perm)
			}

			got, err := Load(path)
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if got != tc.in {
				t.Fatalf("Load() = %+v, want %+v", got, tc.in)
			}
		})
	}
}

func TestLoadPartialJSONPreservesDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"mcp_enabled": true}`), 0o600); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !got.MCPEnabled || !got.AutoScan {
		t.Fatalf("got %+v, want MCPEnabled=true AutoScan=true", got)
	}
}

func TestLoadCorruptedJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"mcp_enabled": not-valid-json`), 0o600); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}
	got, err := Load(path)
	if err == nil {
		t.Fatal("expected error for corrupted JSON, got nil")
	}
	if got != Default() {
		t.Fatalf("expected Default() on error, got %+v", got)
	}
}

func TestLoadReadDirError(t *testing.T) {
	// Passing a directory path as config file triggers a non-ErrNotExist read error.
	dir := t.TempDir()
	_, err := Load(dir)
	if err == nil {
		t.Fatal("expected error reading directory as file, got nil")
	}
}

func TestSaveToInvalidParentFails(t *testing.T) {
	// Create a regular file, then try to use it as a parent directory.
	base := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(base, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}
	if err := Save(filepath.Join(base, "config.json"), Default()); err == nil {
		t.Fatal("expected error saving under non-directory parent, got nil")
	}
}

func TestEmptyPathUsesDefaultHome(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	if err := Save("", Settings{MCPEnabled: true, AutoScan: false}); err != nil {
		t.Fatalf("Save(\"\") error = %v", err)
	}
	got, err := Load("")
	if err != nil {
		t.Fatalf("Load(\"\") error = %v", err)
	}
	if !got.MCPEnabled || got.AutoScan {
		t.Fatalf("Load(\"\") = %+v, want MCPEnabled=true AutoScan=false", got)
	}
}
