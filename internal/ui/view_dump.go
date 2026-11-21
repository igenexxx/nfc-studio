package ui

import (
	"fmt"

	"nfc-studio/internal/nfcfile"
)

const (
	dumpVisibleRows = 7
	dumpRowHeight   = 14
)

func (a *App) handleDumpKey(act KeyAction) {
	switch act {
	case KeyUp:
		if a.DumpScroll > 0 {
			a.DumpScroll--
		}
	case KeyDown:
		if a.CurrentTag != nil && a.DumpScroll < max(0, len(a.CurrentTag.Pages)-5) {
			a.DumpScroll++
		}
	case KeyNav4, KeySpace:
		a.ShowNdefTab = !a.ShowNdefTab
	case KeyNav5:
		a.Mode = ModeEmu
		a.startEmulation()
	case KeyNav6:
		a.saveCurrentTag()
	case KeyNav7:
		a.DumpScroll = 0
	}
}

func (a *App) drawDump(c *Canvas) {
	if a.CurrentTag == nil {
		c.DrawTextCentered(ScreenW/2, 75, "No tag loaded", ColMuted, 1)
		return
	}
	if a.ShowNdefTab {
		a.drawNdefOverview(c)
		return
	}
	c.DrawText(6, 21, fmt.Sprintf("Hex (%d pg) %s H=%.1f", len(a.CurrentTag.Pages), a.LastProfile.ClassLabel, a.LastProfile.EntropyBits), ColCyan, 1)
	c.FillRect(6, 32, 308, 108, ColPanel)
	c.DrawRect(6, 32, 308, 108, ColBorder)

	for row := range dumpVisibleRows {
		pgIdx := a.DumpScroll + row
		if pgIdx >= len(a.CurrentTag.Pages) {
			break
		}
		pg := a.CurrentTag.Pages[pgIdx]
		y := 37 + row*dumpRowHeight
		line := fmt.Sprintf("P%02d: %-12s |%s|", pgIdx, nfcfile.FormatHex(pg), bytesToASCII(pg))
		c.DrawText(12, y, line, pageColor(pgIdx), 1)
	}
}

func (a *App) drawNdefOverview(c *Canvas) {
	c.DrawText(6, 21, "NDEF & Tag Structure", ColCyan, 1)
	c.FillRect(6, 32, 308, 108, ColPanel)
	c.DrawRect(6, 32, 308, 108, ColBorder)
	c.DrawText(12, 40, "Type: "+a.CurrentTag.DeviceType, ColEmerald, 1)
	c.DrawText(12, 54, "UID : "+nfcfile.FormatHex(a.CurrentTag.UID), ColText, 1)
	if a.CurrentNDEF != nil {
		c.DrawText(12, 74, "NDEF Record ("+a.CurrentNDEF.Type+"):", ColAmber, 1)
		c.DrawText(12, 88, truncate(a.CurrentNDEF.Payload, 46), ColText, 1)
		return
	}
	c.DrawText(12, 78, "No NDEF TLV (0x03) found", ColMuted, 1)
}

func pageColor(pgIdx int) uint16 {
	switch {
	case pgIdx <= 2:
		return ColCyan // UID / BCC / Lock
	case pgIdx == 3:
		return ColAmber // Capability Container
	default:
		return ColEmerald // User / NDEF data
	}
}
