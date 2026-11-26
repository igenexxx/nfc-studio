package ui

import (
	"testing"
	"time"

	"nfc-studio/internal/library"
)

func TestContextAwareHelpAllScreensAndBounds(t *testing.T) {
	app := NewApp(nil, t.TempDir())
	defer app.Close()
	c, _ := NewCanvas("")

	setupCases := []struct {
		name      string
		setup     func()
		wantTitle string
	}{
		{"home simple", func() { app.ShowSources = false; app.Settings.ExpertMode = false; app.goHome() }, "HOME MENU"},
		{"home expert", func() { app.ShowSources = false; app.Settings.ExpertMode = true; app.goHome() }, "HOME MENU"},
		{"read listen simple", func() { app.Settings.ExpertMode = false; app.enterReadWizard() }, "READ & CLONE (LISTEN)"},
		{"read listen expert", func() { app.Settings.ExpertMode = true; app.enterReadWizard() }, "READ & CLONE (LISTEN)"},
		{"read captured", func() {
			app.enterReadWizard()
			app.ReadCaptured = true
		}, "READ & CLONE (CAPTURED)"},
		{"saved simple", func() {
			app.Settings.ExpertMode = false
			app.SwitchMode(ModeSaved)
		}, "MY CARDS"},
		{"saved expert", func() {
			app.Settings.ExpertMode = true
			app.SwitchMode(ModeSaved)
		}, "MY CARDS (EXPERT)"},
		{"emu simple", func() {
			app.Settings.ExpertMode = false
			app.SwitchMode(ModeEmu)
		}, "EMULATE CARD"},
		{"emu expert", func() {
			app.Settings.ExpertMode = true
			app.SwitchMode(ModeEmu)
		}, "EMULATE CARD"},
		{"write idle", func() {
			app.SwitchMode(ModeWrite)
			app.WriteBusy = false
			app.WriteArmedUntil = time.Time{}
		}, "WRITE TO CARD"},
		{"write armed", func() {
			app.SwitchMode(ModeWrite)
			app.WriteBusy = false
			app.WriteArmedUntil = time.Now().Add(5 * time.Second)
		}, "WRITE TO CARD (ARMED)"},
		{"write busy", func() {
			app.SwitchMode(ModeWrite)
			app.WriteBusy = true
			app.WriteArmedUntil = time.Time{}
		}, "WRITE TO CARD (WRITING)"},
		{"scan", func() {
			app.WriteBusy = false
			app.Settings.ExpertMode = true
			app.SwitchMode(ModeScan)
		}, "NFC RADAR LAB"},
		{"dump hex simple", func() {
			app.Settings.ExpertMode = false
			app.ShowNdefTab = false
			app.SwitchMode(ModeDump)
		}, "CARD MEMORY (HEX)"},
		{"dump hex expert", func() {
			app.Settings.ExpertMode = true
			app.ShowNdefTab = false
			app.SwitchMode(ModeDump)
		}, "CARD MEMORY (HEX)"},
		{"dump ndef simple", func() {
			app.Settings.ExpertMode = false
			app.ShowNdefTab = true
			app.SwitchMode(ModeDump)
		}, "CARD MEMORY (NDEF)"},
		{"dump ndef expert", func() {
			app.Settings.ExpertMode = true
			app.ShowNdefTab = true
			app.SwitchMode(ModeDump)
		}, "CARD MEMORY (NDEF)"},
		{"probe idle", func() {
			app.SwitchMode(ModeProbe)
			app.Probe.Status = library.ProbeIdle
		}, "AUTO-EMU PROBE"},
		{"probe running", func() {
			app.SwitchMode(ModeProbe)
			app.Probe.Status = library.ProbeRunning
		}, "AUTO-EMU PROBE (RUNNING)"},
		{"settings simple", func() {
			app.Settings.ExpertMode = false
			app.SwitchMode(ModeSettings)
		}, "SETTINGS (SIMPLE)"},
		{"settings expert", func() {
			app.Settings.ExpertMode = true
			app.SwitchMode(ModeSettings)
		}, "SETTINGS (EXPERT)"},
		{"sources", func() {
			app.openSourcesManage()
		}, "MANAGE DB SOURCES"},
	}

	for _, tc := range setupCases {
		t.Run(tc.name, func(t *testing.T) {
			tc.setup()
			h := app.ActiveScreenHelp()
			if h.ScreenTitle != tc.wantTitle {
				t.Fatalf("ScreenTitle = %q, want %q", h.ScreenTitle, tc.wantTitle)
			}
			if h.Summary[0] == "" {
				t.Fatalf("Summary[0] must not be empty")
			}
			if len(h.Summary[0]) > helpSummaryMaxCh || len(h.Summary[1]) > helpSummaryMaxCh {
				t.Fatalf("summary line exceeds %d chars: %q (%d) / %q (%d)",
					helpSummaryMaxCh, h.Summary[0], len(h.Summary[0]), h.Summary[1], len(h.Summary[1]))
			}
			if len(h.Keys) == 0 {
				t.Fatalf("Keys must not be empty")
			}
			for _, k := range h.Keys {
				if k.Key == "" || len(k.Key) > 7 {
					t.Fatalf("keycap label %q must be 1..7 chars", k.Key)
				}
				if k.Desc == "" || len(k.Desc) > helpDescMaxCh {
					t.Fatalf("key desc %q (%d) must be 1..%d chars", k.Desc, len(k.Desc), helpDescMaxCh)
				}
			}

			// Open Help, render every page without panic, verify paging wrap, and close cleanly.
			app.HandleKey(KeyHelp)
			if !app.ShowHelp || app.HelpPage != 0 {
				t.Fatalf("expected ShowHelp=true HelpPage=0")
			}
			pages := h.PageCount()
			for p := range pages {
				if app.HelpPage != p {
					t.Fatalf("HelpPage = %d, want %d", app.HelpPage, p)
				}
				app.Render(c)
				if pages > 1 {
					app.HandleKey(KeyRight)
				}
			}
			if pages > 1 && app.HelpPage != 0 {
				t.Fatalf("expected HelpPage wrap back to 0, got %d", app.HelpPage)
			}
			if pages > 1 {
				app.HandleKey(KeyLeft)
				if app.HelpPage != pages-1 {
					t.Fatalf("expected KeyLeft wrap to %d, got %d", pages-1, app.HelpPage)
				}
			}
			app.HandleKey(KeyBack)
			if app.ShowHelp {
				t.Fatalf("expected KeyBack to close Help modal")
			}

			// Re-open and verify KeyHelp closes cleanly too.
			app.HandleKey(KeyHelp)
			if !app.ShowHelp {
				t.Fatalf("expected KeyHelp to open Help modal")
			}
			app.HandleKey(KeyHelp)
			if app.ShowHelp {
				t.Fatalf("expected KeyHelp to close Help modal")
			}
		})
	}
}
