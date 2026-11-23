package ui

import (
	"path/filepath"
	"testing"
	"time"

	"nfc-studio/internal/config"
	"nfc-studio/internal/st25r3916"
)

func TestNFCLinearHomeReadCloneAndEmulateFlow(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	mr := &mockReader{
		tag: &st25r3916.TagInfo{
			UID:  []byte{0x04, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66},
			ATQA: [2]byte{0x44, 0x00},
			SAK:  0x00,
		},
	}
	app := NewApp(mr, t.TempDir())
	defer app.Close()
	app.SetConfigPath(cfgPath)
	c, _ := NewCanvas("")

	// Boots into HOME in Simple Mode (4 items) with RF poller idle!
	if app.Mode != ModeHome {
		t.Fatalf("initial Mode = %v, want ModeHome", app.Mode)
	}
	if got := len(app.homeMenuItems()); got != 4 {
		t.Fatalf("Simple Mode homeMenuItems = %d, want 4", got)
	}
	if app.wantScan.Load() || app.wantEmu.Load() {
		t.Fatalf("RF poller MUST be idle on HOME")
	}
	app.Render(c)

	// Enter opens Read & Clone -> activates RF scan poller -> auto-freezes Step 2 on tag!
	app.StartPoller()
	defer app.StopPoller()
	app.HandleKey(KeyEnter)
	if app.Mode != ModeRead || app.ReadCaptured || !app.wantScan.Load() {
		t.Fatalf("expected ModeRead polling, got mode=%v cap=%v wantScan=%v", app.Mode, app.ReadCaptured, app.wantScan.Load())
	}
	app.Render(c)

	deadline := time.Now().Add(1500 * time.Millisecond)
	for !app.ReadCaptured && time.Now().Before(deadline) {
		app.Step()
		time.Sleep(25 * time.Millisecond)
	}
	if !app.ReadCaptured || app.CurrentTag == nil {
		t.Fatalf("expected auto-freeze Step 2 on hardware tag detection")
	}
	if app.wantScan.Load() {
		t.Fatalf("expected RF scan poller paused after Step 2 freeze")
	}
	app.Render(c)

	// 4 EMU starts emulation immediately; Esc stops emulation and returns to HOME.
	app.HandleKey(KeyNav4)
	if app.Mode != ModeEmu || !app.EmuActive {
		t.Fatalf("expected ModeEmu active after 4 EMU, got mode=%v emu=%v", app.Mode, app.EmuActive)
	}
	app.HandleKey(KeyBack)
	if app.Mode != ModeHome || app.EmuActive {
		t.Fatalf("expected ModeHome & EmuActive=false after KeyBack, got mode=%v emu=%v", app.Mode, app.EmuActive)
	}
}

func TestNFCHomeExitConfirmAndExpertPersistence(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	app := NewApp(nil, t.TempDir())
	defer app.Close()
	app.SetConfigPath(cfgPath)
	c, _ := NewCanvas("")

	// Toggle Expert Mode via HOME 5 EXP -> unlocks 7 items & persists.
	app.HandleKey(KeyNav5)
	if !app.Settings.ExpertMode || len(app.homeMenuItems()) != 7 {
		t.Fatalf("expected ExpertMode=true with 7 items, got exp=%v len=%d", app.Settings.ExpertMode, len(app.homeMenuItems()))
	}
	loaded, err := config.Load(cfgPath)
	if err != nil || !loaded.ExpertMode {
		t.Fatalf("expected ExpertMode=true persisted in config.json, got %+v err=%v", loaded, err)
	}

	// SIM Read & Clone -> 4 READ -> 5 SAVE opens My Cards.
	app.enterReadWizard()
	app.HandleKey(KeyNav4)
	if !app.ReadCaptured {
		t.Fatalf("expected SIM 4 READ to capture sample tag")
	}
	app.HandleKey(KeyNav5)
	if app.Mode != ModeSaved || len(app.SavedFiles) == 0 {
		t.Fatalf("expected ModeSaved with >=1 tag after 5 SAVE, got mode=%v len=%d", app.Mode, len(app.SavedFiles))
	}

	// Esc returns to HOME from every screen.
	for _, m := range []Mode{ModeSaved, ModeEmu, ModeScan, ModeDump, ModeProbe, ModeSettings} {
		app.SwitchMode(m)
		app.HandleKey(KeyBack)
		if app.Mode != ModeHome {
			t.Fatalf("KeyBack from %v went to %v, want ModeHome", m, app.Mode)
		}
	}

	// First Esc on HOME asks confirmation; Enter confirms exit.
	if exit := app.HandleKey(KeyBack); exit || !app.ConfirmExit {
		t.Fatalf("expected ConfirmExit=true without immediate exit")
	}
	app.Render(c)
	if exit := app.HandleKey(KeyEnter); !exit {
		t.Fatalf("expected exit=true after Esc -> Enter on HOME")
	}
}
