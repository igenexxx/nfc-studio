package ui

import (
	"fmt"

	"nfc-studio/internal/nfcfile"
)

func (a *App) enterReadWizard() {
	a.ReadCaptured = false
	a.Scanning = true
	if !a.SimMode {
		a.CurrentTag = nil
		a.CurrentNDEF = nil
		a.HasMatch = false
	}
	a.SwitchMode(ModeRead)
}

func (a *App) handleReadKey(act KeyAction) {
	if !a.ReadCaptured {
		a.handleReadWaitingKey(act)
		return
	}
	a.handleReadCapturedKey(act)
}

func (a *App) handleReadWaitingKey(act KeyAction) {
	switch act {
	case KeyNav4, KeySpace, KeyEnter:
		if a.SimMode {
			a.CurrentTag = defaultSimTag()
			a.updateNDEF()
			a.ReadCaptured = true
			a.SetToast("SIM tag read!")
			return
		}
		a.Scanning = !a.Scanning
		if a.Scanning {
			a.SetToast("Listening 13.56 MHz...")
		} else {
			a.SetToast("Scan paused")
		}
	case KeyNav7:
		if a.Settings.ExpertMode {
			a.SwitchMode(ModeScan)
		}
	case KeyBack, KeyNav8:
		a.goHome()
	}
}

func (a *App) handleReadCapturedKey(act KeyAction) {
	switch act {
	case KeyEnter, KeyNav4:
		if a.CurrentTag == nil {
			return
		}
		if !isEmulatableTag(a.CurrentTag) {
			a.SetToast("EMU supports NFC-A & FeliCa only")
			return
		}
		a.SwitchMode(ModeEmu)
		a.startEmulation()
	case KeyNav5:
		a.saveCurrentTag()
		a.refreshSavedFiles()
		if len(a.SavedFiles) > 0 {
			a.SavedIdx = len(a.SavedFiles) - 1
		}
		a.SwitchMode(ModeSaved)
	case KeyNav6, KeySpace:
		a.enterReadWizard()
		a.SetToast("Ready - tap next card")
	case KeyNav7:
		a.DumpScroll = 0
		a.ShowNdefTab = a.CurrentNDEF != nil
		a.SwitchMode(ModeDump)
	case KeyBack, KeyNav8:
		a.goHome()
	}
}

func (a *App) readNavLabels() [5]string {
	if !a.ReadCaptured {
		s4 := scanLabel(a.Scanning)
		if a.SimMode {
			s4 = "READ"
		}
		s7 := ""
		if a.Settings.ExpertMode {
			s7 = "LAB"
		}
		return [5]string{s4, "", "", s7, "MENU"}
	}
	return [5]string{"EMU", "SAVE", "AGAIN", "INFO", "MENU"}
}

func (a *App) drawRead(c *Canvas) {
	if !a.ReadCaptured || a.CurrentTag == nil {
		a.drawReadWaiting(c)
		return
	}
	a.drawReadCaptured(c)
}

func (a *App) drawReadWaiting(c *Canvas) {
	c.FillRect(6, 22, 308, 114, ColPanel)
	c.DrawRect(6, 22, 308, 114, ColEmerald)

	c.DrawTextCentered(ScreenW/2, 36, "STEP 1: TAP CARD OR TAG", ColEmerald, 1)
	c.DrawTextCentered(ScreenW/2, 54, "Hold NFC card or keyfob near the", ColText, 1)
	c.DrawTextCentered(ScreenW/2, 68, "Cap NFC antenna (13.56 MHz).", ColMuted, 1)

	statusTxt, statusCol := "Listening on ST25R3916...", ColEmerald
	if a.SimMode {
		statusTxt, statusCol = "SIM mode: press 4 READ / Enter", ColAmber
	} else if !a.Scanning {
		statusTxt, statusCol = "Paused (press 4 SCAN to resume)", ColAmber
	}
	c.DrawTextCentered(ScreenW/2, 92, statusTxt, statusCol, 1)

	fldLabel, fldCol := fieldBadgeStyle(a.LastField)
	c.DrawTextCentered(ScreenW/2, 112, fldLabel, fldCol, 1)
}

func (a *App) drawReadCaptured(c *Canvas) {
	t := a.CurrentTag
	c.FillRect(6, 22, 308, 114, ColPanel)
	c.DrawRect(6, 22, 308, 114, ColEmerald)

	c.DrawText(14, 30, "STEP 2: TAG READ!", ColEmerald, 1)
	if a.HasMatch {
		match := fmt.Sprintf("Match: %s %.0f%%", truncate(a.LastMatch.Name, 12), a.LastMatch.Similarity*100)
		c.DrawText(160, 30, match, ColMagenta, 1)
	}

	c.DrawText(14, 48, truncate(t.DeviceType, 32), ColCyan, 1)
	c.DrawText(14, 62, "UID: "+truncate(nfcfile.FormatHex(t.UID), 36), ColText, 1)

	if a.CurrentNDEF != nil {
		ndef := fmt.Sprintf("NDEF [%s]: %s", a.CurrentNDEF.Type, a.CurrentNDEF.Payload)
		c.DrawText(14, 78, truncate(ndef, 48), ColAmber, 1)
	} else {
		meta := fmt.Sprintf("ATQA:%02X%02X  SAK:%02X  Pages:%d", t.ATQA[0], t.ATQA[1], t.SAK, len(t.Pages))
		c.DrawText(14, 78, meta, ColMuted, 1)
	}

	c.HLine(12, 98, 296, ColBorder)
	c.DrawTextCentered(ScreenW/2, 108, "[Enter/4] EMULATE   [5] SAVE .NFC", ColEmerald, 1)
}
