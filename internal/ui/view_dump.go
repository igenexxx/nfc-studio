package ui

import (
	"fmt"

	"nfc-studio/internal/nfcfile"
)

const (
	dumpVisibleRows = 7
	dumpRowHeight   = 14
	dumpViewCount   = 3
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
	case KeyLeft:
		a.DumpView = (a.DumpView + dumpViewCount - 1) % dumpViewCount
	case KeyRight:
		a.DumpView = (a.DumpView + 1) % dumpViewCount
	case KeyNav4, KeySpace:
		a.ShowNdefTab = !a.ShowNdefTab
	case KeyNav5:
		a.Mode = ModeEmu
		a.startEmulation()
	case KeyNav6:
		a.saveCurrentTag()
	case KeyNav7:
		a.DumpScroll = 0
	case KeyWrite:
		if a.CurrentTag != nil {
			a.openWriteView(ModeDump)
		}
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
	mifare := isMifareDumpTag(a.CurrentTag)
	c.DrawText(6, 21, dumpHeaderText(a.CurrentTag, a.LastProfile.ClassLabel, a.LastProfile.EntropyBits, a.DumpView, mifare), ColCyan, 1)
	c.FillRect(6, 32, 308, 108, ColPanel)
	c.DrawRect(6, 32, 308, 108, ColBorder)
	if len(a.CurrentTag.Pages) == 0 {
		c.DrawTextCentered(ScreenW/2, 74, "No memory pages/blocks read", ColAmber, 1)
		c.DrawTextCentered(ScreenW/2, 88, "(UID-only or encrypted sectors)", ColMuted, 1)
		return
	}

	for row := range dumpVisibleRows {
		pgIdx := a.DumpScroll + row
		if pgIdx >= len(a.CurrentTag.Pages) {
			break
		}
		y := 37 + row*dumpRowHeight
		line := formatDumpRow(a.CurrentTag, pgIdx, a.DumpView, mifare)
		col := dumpRowColor(a.CurrentTag, pgIdx, mifare)
		c.DrawText(10, y, truncate(line, 50), col, 1)
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

func isMifareDumpTag(t *nfcfile.NfcTag) bool {
	if t == nil {
		return false
	}
	if t.MifareType != "" || t.SAK == 0x08 || t.SAK == 0x18 {
		return true
	}
	return len(t.Pages) > 0 && len(t.Pages[0]) == 16
}

func dumpHeaderText(t *nfcfile.NfcTag, cls string, entropy float32, view int, mifare bool) string {
	if !mifare {
		return fmt.Sprintf("Hex (%d pg) %s H=%.1f", len(t.Pages), cls, entropy)
	}
	viewLbl := "[0..7]"
	switch view % dumpViewCount {
	case 1:
		viewLbl = "[8..15]"
	case 2:
		viewLbl = "[ASCII]"
	}
	return fmt.Sprintf("MF %d blk %s Z/C  UID/DATA/TRLR", len(t.Pages), viewLbl)
}

func formatDumpRow(t *nfcfile.NfcTag, idx, view int, mifare bool) string {
	pg := t.Pages[idx]
	if !mifare {
		return fmt.Sprintf("P%02d: %-12s |%s|", idx, nfcfile.FormatHex(pg), bytesToASCII(pg))
	}
	unreadable := idx < len(t.Unreadable) && t.Unreadable[idx]
	if unreadable {
		if view%dumpViewCount == 2 {
			return fmt.Sprintf("B%02d: |????????????????| S%02d LOCK", idx, nfcfile.MifareBlockSector(idx))
		}
		return fmt.Sprintf("B%02d: ?? ?? ?? ?? ?? ?? ?? ?? |????????|", idx)
	}
	if len(pg) < 16 {
		return fmt.Sprintf("B%02d: %s", idx, nfcfile.FormatHex(pg))
	}
	switch view % dumpViewCount {
	case 1:
		half := pg[8:16]
		return fmt.Sprintf("B%02d: %s |%s|", idx, nfcfile.FormatHex(half), bytesToASCII(half))
	case 2:
		return fmt.Sprintf("B%02d: |%s| S%02d %s", idx, bytesToASCII(pg), nfcfile.MifareBlockSector(idx), mifareBlockRole(idx))
	default:
		half := pg[0:8]
		return fmt.Sprintf("B%02d: %s |%s|", idx, nfcfile.FormatHex(half), bytesToASCII(half))
	}
}

func mifareBlockRole(idx int) string {
	if idx == 0 {
		return "UID"
	}
	if nfcfile.IsMifareTrailerBlock(idx) {
		return "TRLR"
	}
	return "DATA"
}

func dumpRowColor(t *nfcfile.NfcTag, idx int, mifare bool) uint16 {
	if !mifare {
		return pageColor(idx)
	}
	if idx < len(t.Unreadable) && t.Unreadable[idx] {
		return ColMuted
	}
	if idx == 0 {
		return ColCyan // Manufacturer Block 0 (UID / BCC / SAK / ATQA)
	}
	if nfcfile.IsMifareTrailerBlock(idx) {
		return ColAmber // Sector Trailer (Key A / Access Bits / Key B)
	}
	return ColEmerald // User data block
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
