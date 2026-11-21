package ui

import (
	"testing"
	"time"

	"nfc-studio/internal/nfcfile"
	"nfc-studio/internal/nfcml"
)

func TestClassifyRadarSource(t *testing.T) {
	tests := []struct {
		name    string
		tag     *nfcfile.NfcTag
		profile nfcml.DumpProfile
		field   nfcml.FieldReading
		want    RadarSourceKind
	}{
		{
			name:  "metal detune overrides everything",
			tag:   &nfcfile.NfcTag{DeviceType: "NTAG215", SAK: 0x00},
			field: nfcml.FieldReading{State: nfcml.FieldMetalDetune},
			want:  RadarSourceMetal,
		},
		{
			name: "nil tag is idle",
			want: RadarSourceIdle,
		},
		{
			name:    "encrypted dump overrides card glyph",
			tag:     &nfcfile.NfcTag{DeviceType: "MIFARE Classic 1K", SAK: 0x08},
			profile: nfcml.DumpProfile{Class: nfcml.ClassEncrypted},
			want:    RadarSourceEncrypted,
		},
		{
			name: "mifare classic uses card glyph",
			tag:  &nfcfile.NfcTag{DeviceType: "MIFARE Classic 1K", SAK: 0x08},
			want: RadarSourceCard,
		},
		{
			name: "desfire SAK 0x20 uses card glyph",
			tag:  &nfcfile.NfcTag{DeviceType: "ISO14443-4", SAK: 0x20},
			want: RadarSourceCard,
		},
		{
			name: "ntag215 uses chip glyph",
			tag:  &nfcfile.NfcTag{DeviceType: "NTAG215", SAK: 0x00},
			want: RadarSourceNTAG,
		},
		{
			name: "NFC-B uses passport glyph",
			tag:  &nfcfile.NfcTag{DeviceType: "ISO14443-3B", MultiMeta: nfcfile.MultiMeta{Protocol: "NFC-B"}},
			want: RadarSourceNFCB,
		},
		{
			name: "NFC-F uses FeliCa glyph",
			tag:  &nfcfile.NfcTag{DeviceType: "FeliCa", MultiMeta: nfcfile.MultiMeta{Protocol: "NFC-F"}},
			want: RadarSourceFeliCa,
		},
		{
			name: "NFC-V uses vicinity glyph",
			tag:  &nfcfile.NfcTag{DeviceType: "ISO15693-3", MultiMeta: nfcfile.MultiMeta{Protocol: "NFC-V"}},
			want: RadarSourceNFCV,
		},
		{
			name:  "metal detune takes precedence over NFC-B",
			tag:   &nfcfile.NfcTag{DeviceType: "ISO14443-3B", MultiMeta: nfcfile.MultiMeta{Protocol: "NFC-B"}},
			field: nfcml.FieldReading{State: nfcml.FieldMetalDetune},
			want:  RadarSourceMetal,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifyRadarSource(tc.tag, tc.profile, tc.field)
			if got != tc.want {
				t.Fatalf("ClassifyRadarSource() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDrawScanAndEmuRadarStates(t *testing.T) {
	cases := []struct {
		name      string
		field     nfcml.FieldReading
		tag       *nfcfile.NfcTag
		profile   nfcml.DumpProfile
		wantColor uint16
	}{
		{
			name:      "sweet spot renders emerald border",
			field:     nfcml.FieldReading{State: nfcml.FieldSweetSpot, QualityPct: 92},
			tag:       &nfcfile.NfcTag{DeviceType: "NTAG215", SAK: 0x00},
			wantColor: ColEmerald,
		},
		{
			name:      "metal detune renders red border",
			field:     nfcml.FieldReading{State: nfcml.FieldMetalDetune, QualityPct: 15},
			tag:       &nfcfile.NfcTag{DeviceType: "NTAG215", SAK: 0x00},
			wantColor: ColRed,
		},
		{
			name:      "weak coupling with mifare card renders cyan border",
			field:     nfcml.FieldReading{State: nfcml.FieldWeak, QualityPct: 55},
			tag:       &nfcfile.NfcTag{DeviceType: "MIFARE Classic 1K", SAK: 0x08},
			wantColor: ColCyan,
		},
		{
			name:      "encrypted dump renders cleanly",
			field:     nfcml.FieldReading{State: nfcml.FieldIdle, QualityPct: 0},
			tag:       &nfcfile.NfcTag{DeviceType: "NTAG215"},
			profile:   nfcml.DumpProfile{Class: nfcml.ClassEncrypted},
			wantColor: ColBorder,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := NewApp(nil, t.TempDir())
			defer app.Close()
			app.Scanning = true
			app.CurrentTag = tc.tag
			app.LastProfile = tc.profile
			app.LastField = tc.field

			c, _ := NewCanvas("")
			app.drawScanRadar(c)
			if got := c.PixelAt(6, 21); got != tc.wantColor {
				t.Fatalf("radar border pixel = 0x%04X, want 0x%04X", got, tc.wantColor)
			}
		})
	}

	t.Run("emu radar reflects external field and hit flash", func(t *testing.T) {
		app := NewApp(nil, t.TempDir())
		defer app.Close()
		app.Mode = ModeEmu
		app.EmuActive = true
		app.ExternalFieldPct = 82
		app.HitFlashUntil = time.Now().Add(time.Second)

		c, _ := NewCanvas("")
		app.drawEmuRadar(c)
		if got := c.PixelAt(248, 44); got != ColEmerald {
			t.Fatalf("emu radar flash border = 0x%04X, want emerald 0x%04X", got, ColEmerald)
		}
	})

	t.Run("EMU guard blocks non-NFC-A tags with toast", func(t *testing.T) {
		app := NewApp(nil, t.TempDir())
		defer app.Close()
		app.Mode = ModeScan
		app.CurrentTag = &nfcfile.NfcTag{
			DeviceType: "ISO14443-3B",
			UID:        []byte{0x12, 0x34, 0x56, 0x78},
			MultiMeta:  nfcfile.MultiMeta{Protocol: "NFC-B"},
		}
		app.HandleKey(KeyNav5)
		if app.Mode != ModeScan || app.EmuActive {
			t.Fatalf("expected EMU guard to keep ModeScan, got mode=%v emu=%v", app.Mode, app.EmuActive)
		}
		if app.ToastText != "EMU supports NFC-A & FeliCa only" {
			t.Fatalf("toast = %q, want 'EMU supports NFC-A & FeliCa only'", app.ToastText)
		}
	})

	t.Run("felica tag enters EMU and cycles presets cleanly", func(t *testing.T) {
		app := NewApp(nil, t.TempDir())
		defer app.Close()
		app.CurrentTag = &nfcfile.NfcTag{
			DeviceType: "FeliCa",
			Protocol:   "NFC-F",
			UID:        []byte{0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF},
			SystemCode: [2]byte{0x88, 0xB4},
		}
		app.HandleKey(KeyNav5)
		if app.Mode != ModeEmu || !app.EmuActive {
			t.Fatalf("expected ModeEmu active for FeliCa, got mode=%v active=%v", app.Mode, app.EmuActive)
		}
		c, _ := NewCanvas("")
		app.drawEmu(c)
	})
}
