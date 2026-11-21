package ui

import (
	"bytes"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"nfc-studio/internal/config"
	"nfc-studio/internal/st25r3916"
)

func TestNfcAppLifecycle(t *testing.T) {
	app := NewApp(nil, t.TempDir())
	if app.Mode != ModeScan || !app.SimMode {
		t.Fatalf("unexpected initial state: %+v", app)
	}

	// Save tag -> verify in SAVED tab.
	app.HandleKey(KeyNav6)

	// Switch to DUMP, toggle NDEF, scroll.
	app.HandleKey(KeyTab)
	if app.Mode != ModeDump {
		t.Fatalf("expected ModeDump, got %v", app.Mode)
	}
	app.HandleKey(KeyNav4)
	if !app.ShowNdefTab {
		t.Fatal("expected ShowNdefTab=true")
	}

	// Switch to EMU, start, randomize UID, toggle 4B/7B.
	app.HandleKey(KeyTab)
	if app.Mode != ModeEmu {
		t.Fatalf("expected ModeEmu, got %v", app.Mode)
	}
	app.HandleKey(KeyNav4)
	if !app.EmuActive {
		t.Fatal("expected EmuActive=true")
	}
	app.HandleKey(KeyNav6) // toggle 7B -> 4B
	if len(app.CurrentTag.UID) != 4 {
		t.Fatalf("expected 4-byte UID, got %d", len(app.CurrentTag.UID))
	}
	app.HandleKey(KeyNav5) // randomize UID

	// Switch to SAVED, verify saved .nfc file appears.
	app.HandleKey(KeyTab)
	if app.Mode != ModeSaved || len(app.SavedFiles) == 0 {
		t.Fatalf("expected ModeSaved with >=1 file, got mode=%v count=%d", app.Mode, len(app.SavedFiles))
	}

	// Help modal paging.
	app.HandleKey(KeyHelp)
	app.HandleKey(KeyRight)
	if !app.ShowHelp || app.HelpPage != 1 {
		t.Fatalf("expected ShowHelp=true HelpPage=1, got %v %d", app.ShowHelp, app.HelpPage)
	}
	c, _ := NewCanvas("")
	app.Render(c)
	app.HandleKey(KeyHelp)

	if !app.HandleKey(KeyQuit) {
		t.Fatal("expected KeyQuit to return true")
	}
}

type mockReader struct {
	mu       sync.Mutex
	tag      *st25r3916.TagInfo
	hit      bool
	emuCalls int
}

func (m *mockReader) PollISO14443A() (*st25r3916.TagInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.tag == nil {
		return nil, st25r3916.ErrNoTag
	}
	return m.tag, nil
}

func (m *mockReader) PollTargetHit() (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.hit {
		m.hit = false
		return true, nil
	}
	return false, nil
}

func (m *mockReader) StartEmulation(_ []byte, _ [2]byte, _ uint8) error {
	m.mu.Lock()
	m.emuCalls++
	m.mu.Unlock()
	return nil
}

func (m *mockReader) FieldOff() error { return nil }

func TestHardwarePollerDetectsTagAndEmuHits(t *testing.T) {
	mr := &mockReader{
		tag: &st25r3916.TagInfo{
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

	app := NewApp(mr, t.TempDir())
	if app.SimMode {
		t.Fatal("expected SimMode=false when hardware reader is provided")
	}
	if app.CurrentTag != nil {
		t.Fatal("expected CurrentTag=nil before real tag is detected")
	}

	app.StartPoller()
	defer app.StopPoller()

	deadline := time.Now().Add(1500 * time.Millisecond)
	for time.Now().Before(deadline) {
		app.Step()
		if app.CurrentTag != nil {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if app.CurrentTag == nil {
		t.Fatal("expected hardware poller to detect tag")
	}
	if !bytes.Equal(app.CurrentTag.UID, mr.tag.UID) {
		t.Fatalf("got UID=%X want %X", app.CurrentTag.UID, mr.tag.UID)
	}
	if app.CurrentNDEF == nil || app.CurrentNDEF.Payload != "https://a.b.c" {
		t.Fatalf("unexpected NDEF: %+v", app.CurrentNDEF)
	}

	// Switch to emulation mode and verify real target hit is counted.
	app.HandleKey(KeyNav5)
	if app.Mode != ModeEmu || !app.EmuActive {
		t.Fatalf("expected active emulation, got mode=%v active=%v", app.Mode, app.EmuActive)
	}
	mr.mu.Lock()
	mr.hit = true
	mr.mu.Unlock()

	deadline = time.Now().Add(1500 * time.Millisecond)
	for time.Now().Before(deadline) {
		app.Step()
		if app.ReaderHits > 0 {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if app.ReaderHits != 1 {
		t.Fatalf("expected ReaderHits=1, got %d", app.ReaderHits)
	}
}

func TestSettingsTabLifecycleAndPersistence(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	app := NewApp(nil, t.TempDir())
	app.SetConfigPath(cfgPath)
	app.ApplySettings(config.Default())

	// Tab 4 times from SCAN -> DUMP -> EMU -> SAVED -> SET.
	for range 4 {
		app.HandleKey(KeyTab)
	}
	if app.Mode != ModeSettings || app.Mode.String() != "SET" {
		t.Fatalf("expected ModeSettings (SET), got %v (%s)", app.Mode, app.Mode.String())
	}

	// Render default settings (MCP DISABLED, AutoScan ENABLED).
	c, _ := NewCanvas("")
	app.Render(c)

	// Toggle row 0 (MCP Server) ON -> auto-saves.
	app.HandleKey(KeyEnter)
	if !app.Settings.MCPEnabled {
		t.Fatal("expected MCPEnabled=true after toggle")
	}
	loaded, err := config.Load(cfgPath)
	if err != nil || !loaded.MCPEnabled {
		t.Fatalf("expected persisted MCPEnabled=true, got %+v err=%v", loaded, err)
	}

	// Move down to row 1 (Auto Scan) and toggle OFF via KeyNav4.
	app.HandleKey(KeyDown)
	if app.SettingsIdx != 1 {
		t.Fatalf("expected SettingsIdx=1, got %d", app.SettingsIdx)
	}
	app.HandleKey(KeyNav4)
	if app.Settings.AutoScan || app.Scanning {
		t.Fatalf("expected AutoScan=false and Scanning=false, got %+v scan=%v", app.Settings, app.Scanning)
	}

	// Explicit save (KeyNav5) and render enabled/disabled badges.
	app.HandleKey(KeyNav5)
	app.Render(c)

	// Reset (KeyNav6) restores defaults (MCPEnabled=false, AutoScan=true).
	app.HandleKey(KeyNav6)
	if app.Settings.MCPEnabled || !app.Settings.AutoScan {
		t.Fatalf("expected defaults after reset, got %+v", app.Settings)
	}
	loaded, err = config.Load(cfgPath)
	if err != nil || loaded.MCPEnabled || !loaded.AutoScan {
		t.Fatalf("expected persisted defaults after reset, got %+v err=%v", loaded, err)
	}

	// Move up to row 0, open Help via KeyNav7, close Help, and wrap SET -> SCAN via Tab.
	app.HandleKey(KeyUp)
	if app.SettingsIdx != 0 {
		t.Fatalf("expected SettingsIdx=0, got %d", app.SettingsIdx)
	}
	app.HandleKey(KeyNav7)
	if !app.ShowHelp {
		t.Fatal("expected ShowHelp=true on KeyNav7 in SET mode")
	}
	app.HandleKey(KeyEnter) // close help
	app.HandleKey(KeyTab)   // SET -> SCAN
	if app.Mode != ModeScan {
		t.Fatalf("expected wrap to ModeScan, got %v", app.Mode)
	}
}
