package ui

import (
	"fmt"
	"time"

	"nfc-studio/internal/nfcfile"
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

func (a *App) drawScanRadar(c *Canvas) {
	c.FillRect(6, 21, 74, 74, ColPanel)
	c.DrawRect(6, 21, 74, 74, ColBorder)
	rx, ry := 43, 55
	c.DrawCircle(rx, ry, 8, ColEmerald)
	if a.Scanning {
		c.DrawCircle(rx, ry, 14+int(a.tick%18), ColCyan)
	}
	statusLbl, statusCol := "13.56M", ColMuted
	if !a.SimMode && !a.LastSeen.IsZero() && time.Since(a.LastSeen) < tagDetectedWindow {
		statusLbl, statusCol = "DETECTED", ColEmerald
	} else if !a.Scanning {
		statusLbl, statusCol = "PAUSED", ColAmber
	}
	c.DrawTextCentered(rx, 84, statusLbl, statusCol, 1)
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
	c.DrawText(90, 40, "UID : "+nfcfile.FormatHex(t.UID), ColText, 1)
	c.DrawText(90, 53, fmt.Sprintf("ATQA: %02X %02X   SAK: %02X", t.ATQA[0], t.ATQA[1], t.SAK), ColAmber, 1)
	c.DrawText(90, 66, fmt.Sprintf("MEM : %d pages (%d bytes)", len(t.Pages), len(t.Pages)*4), ColMuted, 1)
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
