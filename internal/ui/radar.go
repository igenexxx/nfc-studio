package ui

import (
	"fmt"
	"strings"
	"time"

	"nfc-studio/internal/nfcfile"
	"nfc-studio/internal/nfcml"
)

// RadarSourceKind categorizes the visual center glyph rendered in the NFC radar.
type RadarSourceKind int

const (
	RadarSourceIdle RadarSourceKind = iota
	RadarSourceNTAG
	RadarSourceCard
	RadarSourceEncrypted
	RadarSourceMetal
	RadarSourceNFCB
	RadarSourceFeliCa
	RadarSourceNFCV
)

// ClassifyRadarSource determines the radar center icon from live field, dump profile, and tag metadata.
func ClassifyRadarSource(tag *nfcfile.NfcTag, profile nfcml.DumpProfile, field nfcml.FieldReading) RadarSourceKind {
	if field.State == nfcml.FieldMetalDetune {
		return RadarSourceMetal
	}
	if tag == nil {
		return RadarSourceIdle
	}
	switch strings.ToUpper(tag.Protocol) {
	case "NFC-B":
		return RadarSourceNFCB
	case "NFC-F":
		return RadarSourceFeliCa
	case "NFC-V":
		return RadarSourceNFCV
	}
	if profile.Class == nfcml.ClassEncrypted {
		return RadarSourceEncrypted
	}
	upper := strings.ToUpper(tag.DeviceType)
	if strings.Contains(upper, "14443-3B") {
		return RadarSourceNFCB
	}
	if strings.Contains(upper, "FELICA") {
		return RadarSourceFeliCa
	}
	if strings.Contains(upper, "15693") {
		return RadarSourceNFCV
	}
	if strings.Contains(upper, "MIFARE") || strings.Contains(upper, "DESFIRE") || tag.SAK == 0x08 || tag.SAK == 0x18 || tag.SAK == 0x20 {
		return RadarSourceCard
	}
	return RadarSourceNTAG
}

// drawScanRadar renders the adaptive 74x74 real-ADC field coupling radar in SCAN mode.
func (a *App) drawScanRadar(c *Canvas) {
	const (
		boxX, boxY, boxW, boxH = 6, 21, 74, 74
		cx, cy                 = 43, 49
	)
	field := a.LastField
	src := ClassifyRadarSource(a.CurrentTag, a.LastProfile, field)
	borderCol, ringCol := radarPalette(field.State, a.Scanning)

	c.FillRect(boxX, boxY, boxW, boxH, ColPanel)
	c.DrawRect(boxX, boxY, boxW, boxH, borderCol)

	drawScanRings(c, cx, cy, field, ringCol, a.Scanning, a.tick)
	drawSourceGlyph(c, cx, cy, src, ringCol)
	drawStrengthBar(c, boxX+6, boxY+55, boxW-12, 4, field.QualityPct, ringCol)

	lbl, lblCol := scanRadarFooter(field, a.Scanning, !a.SimMode && !a.LastSeen.IsZero() && time.Since(a.LastSeen) < tagDetectedWindow)
	c.DrawTextCentered(cx, boxY+63, lbl, lblCol, 1)
}

func radarPalette(state nfcml.FieldState, scanning bool) (border uint16, accent uint16) {
	if !scanning {
		return ColBorder, ColAmber
	}
	switch state {
	case nfcml.FieldSweetSpot:
		return ColEmerald, ColEmerald
	case nfcml.FieldWeak:
		return ColCyan, ColCyan
	case nfcml.FieldMetalDetune:
		return ColRed, ColAmber
	default:
		return ColBorder, ColCyan
	}
}

func drawScanRings(c *Canvas, cx, cy int, field nfcml.FieldReading, col uint16, scanning bool, tick uint64) {
	c.DrawCircle(cx, cy, 22, ColPanelAlt)
	switch field.State {
	case nfcml.FieldSweetSpot:
		c.DrawCircle(cx, cy, 13, ColEmerald)
		c.DrawCircle(cx, cy, 19, ColEmerald)
		drawLockCorners(c, cx, cy, 22, ColEmerald)
	case nfcml.FieldMetalDetune:
		c.DrawCircle(cx, cy, 16, ColRed)
		c.DrawCircle(cx, cy, 21, ColAmber)
	case nfcml.FieldWeak:
		r := 11 + (field.QualityPct * 11 / 100)
		c.DrawCircle(cx, cy, r, col)
		if scanning {
			pulse := 12 + int(tick%11)
			c.DrawCircle(cx, cy, pulse, ColPanelAlt)
		}
	default:
		c.DrawCircle(cx, cy, 11, ColBorder)
		if scanning {
			c.DrawCircle(cx, cy, 12+int(tick%11), col)
		}
	}
}

func drawLockCorners(c *Canvas, cx, cy, r int, col uint16) {
	const seg = 5
	c.HLine(cx-r, cy-r, seg, col)
	c.VLine(cx-r, cy-r, seg, col)
	c.HLine(cx+r-seg+1, cy-r, seg, col)
	c.VLine(cx+r, cy-r, seg, col)
	c.HLine(cx-r, cy+r, seg, col)
	c.VLine(cx-r, cy+r-seg+1, seg, col)
	c.HLine(cx+r-seg+1, cy+r, seg, col)
	c.VLine(cx+r, cy+r-seg+1, seg, col)
}

func drawSourceGlyph(c *Canvas, cx, cy int, src RadarSourceKind, accent uint16) {
	switch src {
	case RadarSourceMetal:
		// Bold warning diamond + exclamation mark for conductive metal detuning.
		c.FillRect(cx-6, cy-6, 13, 13, ColPanel)
		c.DrawRect(cx-6, cy-6, 13, 13, ColRed)
		c.VLine(cx, cy-4, 5, ColAmber)
		c.FillRect(cx, cy+3, 1, 2, ColAmber)
	case RadarSourceCard:
		// Smartcard / MIFARE access badge with gold contact chip.
		c.FillRect(cx-8, cy-5, 17, 11, ColPanelAlt)
		c.DrawRect(cx-8, cy-5, 17, 11, accent)
		c.FillRect(cx-5, cy-2, 4, 4, ColAmber)
		c.HLine(cx+1, cy-2, 5, ColText)
		c.HLine(cx+1, cy+1, 4, ColMuted)
	case RadarSourceEncrypted:
		// Padlock glyph for high-entropy / encrypted user memory.
		c.DrawRect(cx-3, cy-7, 7, 5, ColAmber)
		c.FillRect(cx-5, cy-2, 11, 8, ColPanelAlt)
		c.DrawRect(cx-5, cy-2, 11, 8, ColAmber)
		c.VLine(cx, cy+1, 3, ColText)
	case RadarSourceNTAG:
		// Compact NTAG microchip with pins.
		c.FillRect(cx-4, cy-4, 9, 9, ColPanelAlt)
		c.DrawRect(cx-4, cy-4, 9, 9, accent)
		c.FillRect(cx-2, cy-2, 5, 5, accent)
		c.HLine(cx-7, cy-2, 3, ColMuted)
		c.HLine(cx-7, cy+2, 3, ColMuted)
		c.HLine(cx+5, cy-2, 3, ColMuted)
		c.HLine(cx+5, cy+2, 3, ColMuted)
	case RadarSourceNFCB:
		// Compact passport booklet with e-chip symbol (NFC-B / ISO 14443-B).
		c.FillRect(cx-6, cy-7, 13, 15, ColPanelAlt)
		c.DrawRect(cx-6, cy-7, 13, 15, ColCyan)
		c.DrawRect(cx-3, cy-2, 7, 5, ColEmerald)
		c.HLine(cx-3, cy, 7, ColEmerald)
	case RadarSourceFeliCa:
		// Twin transit RF chevrons (FeliCa / NFC-F).
		c.DrawCircle(cx-2, cy, 5, ColCyan)
		c.DrawCircle(cx+2, cy, 5, ColAmber)
		c.FillRect(cx-1, cy-1, 3, 3, ColText)
	case RadarSourceNFCV:
		// Vicinity antenna bars (ISO 15693 / NFC-V).
		c.VLine(cx, cy-6, 13, ColEmerald)
		c.VLine(cx-4, cy-4, 9, ColCyan)
		c.VLine(cx+4, cy-4, 9, ColCyan)
		c.HLine(cx-6, cy, 13, ColEmerald)
	default:
		c.DrawCircle(cx, cy, 4, accent)
	}
}

func drawStrengthBar(c *Canvas, x, y, w, h, pct int, col uint16) {
	c.FillRect(x, y, w, h, ColBg)
	c.DrawRect(x, y, w, h, ColBorder)
	clamped := min(100, max(0, pct))
	fillW := (w - 2) * clamped / 100
	if fillW > 0 {
		c.FillRect(x+1, y+1, fillW, h-2, col)
	}
}

func scanRadarFooter(field nfcml.FieldReading, scanning, detected bool) (string, uint16) {
	if !scanning {
		return "PAUSED", ColAmber
	}
	switch field.State {
	case nfcml.FieldMetalDetune:
		return "METAL!", ColRed
	case nfcml.FieldSweetSpot:
		return fmt.Sprintf("LOCK %d%%", field.QualityPct), ColEmerald
	case nfcml.FieldWeak:
		return fmt.Sprintf("SIG %d%%", field.QualityPct), ColCyan
	}
	if detected {
		return "DETECTED", ColEmerald
	}
	return "13.56M", ColMuted
}

// drawEmuRadar renders the live outward TX waves and external reader RF strength bar in EMU mode.
func (a *App) drawEmuRadar(c *Canvas) {
	const (
		boxX, boxY, boxW, boxH = 248, 44, 58, 72
		cx, cy                 = 277, 72
	)
	flash := a.EmuActive && !a.HitFlashUntil.IsZero() && time.Now().Before(a.HitFlashUntil)
	border := ColBorder
	accent := ColMuted
	if flash {
		border, accent = ColEmerald, ColEmerald
	} else if a.EmuActive {
		border, accent = ColCyan, ColEmerald
	}

	c.FillRect(boxX, boxY, boxW, boxH, ColBg)
	c.DrawRect(boxX, boxY, boxW, boxH, border)

	if a.EmuActive {
		r1 := 7 + int(a.tick%14)
		r2 := 7 + int((a.tick+7)%14)
		c.DrawCircle(cx, cy, r1, accent)
		c.DrawCircle(cx, cy, r2, ColCyan)
		if flash {
			drawLockCorners(c, cx, cy, 20, ColEmerald)
		}
	}
	src := ClassifyRadarSource(a.CurrentTag, a.LastProfile, nfcml.FieldReading{})
	drawSourceGlyph(c, cx, cy, src, accent)

	barCol := ColCyan
	if a.ExternalFieldPct >= 70 || flash {
		barCol = ColEmerald
	}
	drawStrengthBar(c, boxX+5, boxY+52, boxW-10, 4, a.ExternalFieldPct, barCol)
	lbl := fmt.Sprintf("RF %d%%", a.ExternalFieldPct)
	lblCol := ColMuted
	if flash {
		lbl, lblCol = "HIT!", ColEmerald
	} else if a.EmuActive {
		lblCol = barCol
	}
	c.DrawTextCentered(cx, boxY+60, lbl, lblCol, 1)
}
