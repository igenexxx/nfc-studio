package ui

import (
	"fmt"
	"time"

	"nfc-studio/internal/nfcfile"
	"nfc-studio/internal/nfcml"
)

const tagDetectedWindow = 1500 * time.Millisecond

func (a *App) handleScanKey(act KeyAction) {
	switch act {
	case KeyNav4, KeySpace:
		a.Scanning = !a.Scanning
		if a.Scanning {
			a.SetToast("Scanning 13.56 MHz...")
		} else {
			a.SetToast("Scan paused")
		}
	case KeyNav5, KeyEnter:
		if a.CurrentTag != nil {
			if !isNFCATag(a.CurrentTag) {
				a.SetToast("EMU supports NFC-A only")
				return
			}
			a.Mode = ModeEmu
			a.startEmulation()
		}
	case KeyNav6:
		a.saveCurrentTag()
	case KeyNav7:
		a.Mode = ModeDump
		a.DumpScroll = 0
	}
}

func (a *App) drawScan(c *Canvas) {
	a.drawScanRadar(c)
	a.drawTagSummary(c)
	a.drawNDEFPreview(c)
}

func (a *App) drawTagSummary(c *Canvas) {
	c.FillRect(84, 21, 230, 74, ColPanel)
	c.DrawRect(84, 21, 230, 74, ColCyan)
	if a.CurrentTag == nil {
		c.DrawTextCentered(199, 54, "Present NFC tag to Cap...", ColMuted, 1)
		return
	}
	t := a.CurrentTag
	c.DrawText(90, 26, t.DeviceType, ColEmerald, 1)
	switch t.Protocol {
	case "NFC-B":
		c.DrawText(90, 40, "PUPI: "+nfcfile.FormatHex(t.UID), ColText, 1)
		c.DrawText(90, 53, "APP : "+nfcfile.FormatHex(t.AppData[:]), ColAmber, 1)
		c.DrawText(90, 66, "INFO: "+nfcfile.FormatHex(t.ProtocolInfo[:]), ColMuted, 1)
	case "NFC-F":
		c.DrawText(90, 40, "IDm : "+truncate(nfcfile.FormatHex(t.UID), 24), ColText, 1)
		c.DrawText(90, 53, "PMm : "+truncate(nfcfile.FormatHex(t.PMm[:]), 24), ColAmber, 1)
		c.DrawText(90, 66, fmt.Sprintf("SYS : %02X%02X", t.SystemCode[0], t.SystemCode[1]), ColMuted, 1)
	case "NFC-V":
		c.DrawText(90, 40, "UID : "+truncate(nfcfile.FormatHex(t.UID), 24), ColText, 1)
		c.DrawText(90, 53, fmt.Sprintf("DSFID: %02X   BLK: %d", t.DSFID, len(t.Pages)), ColAmber, 1)
		c.DrawText(90, 66, fmt.Sprintf("MEM : %d blk  ISO15693", len(t.Pages)), ColMuted, 1)
	default:
		c.DrawText(90, 40, "UID : "+nfcfile.FormatHex(t.UID), ColText, 1)
		c.DrawText(90, 53, fmt.Sprintf("ATQA: %02X %02X   SAK: %02X", t.ATQA[0], t.ATQA[1], t.SAK), ColAmber, 1)
		c.DrawText(90, 66, fmt.Sprintf("MEM : %d pg  %s H=%.1f", len(t.Pages), a.LastProfile.ClassLabel, a.LastProfile.EntropyBits), ColMuted, 1)
	}
	fldLabel, fldCol := fieldBadgeStyle(a.LastField)
	c.DrawText(90, 79, fldLabel, fldCol, 1)
}

func isNFCATag(t *nfcfile.NfcTag) bool {
	if t == nil {
		return false
	}
	return t.Protocol == "" || t.Protocol == "NFC-A"
}

func fieldBadgeStyle(r nfcml.FieldReading) (string, uint16) {
	switch r.State {
	case nfcml.FieldSweetSpot:
		return fmt.Sprintf("FLD : SWEET SPOT (%d%%)", r.QualityPct), ColEmerald
	case nfcml.FieldMetalDetune:
		return "FLD : METAL / DETUNED!", ColAmber
	case nfcml.FieldWeak:
		return fmt.Sprintf("FLD : WEAK COUPLING (%d%%)", r.QualityPct), ColCyan
	case nfcml.FieldIdle:
		return "FLD : IDLE (calibrated)", ColMuted
	default:
		return "FLD : CALIBRATING...", ColMuted
	}
}

func (a *App) drawNDEFPreview(c *Canvas) {
	c.FillRect(6, 99, 308, 40, ColPanel)
	c.DrawRect(6, 99, 308, 40, ColBorder)
	if a.CurrentNDEF != nil {
		c.DrawText(12, 104, "NDEF ["+a.CurrentNDEF.Type+"]:", ColCyan, 1)
		c.DrawText(12, 118, truncate(a.CurrentNDEF.Payload, 48), ColText, 1)
		return
	}
	c.DrawText(12, 113, "No NDEF payload (raw / proprietary blocks)", ColMuted, 1)
}
