package ui

import (
	"fmt"
	"math/rand"

	"nfc-studio/internal/nfcfile"
)

func (a *App) handleEmuKey(act KeyAction) {
	switch act {
	case KeyNav4, KeySpace, KeyEnter:
		if a.EmuActive {
			a.stopEmulation()
		} else {
			a.startEmulation()
		}
	case KeyNav5:
		a.randomizeUID()
	case KeyNav6:
		a.toggleUIDLength()
	case KeyNav7:
		a.ReaderHits = 0
		a.SetToast("Hits reset")
	}
}

func (a *App) startEmulation() {
	if a.CurrentTag == nil {
		return
	}
	if a.Chip != nil {
		if err := a.Chip.StartEmulation(a.CurrentTag.UID, a.CurrentTag.ATQA, a.CurrentTag.SAK); err != nil {
			a.SetToast("EMU err")
			return
		}
	}
	a.EmuActive = true
	a.syncPollFlags()
	a.SetToast("Emulating " + a.CurrentTag.DeviceType)
}

func (a *App) stopEmulation() {
	if a.Chip != nil {
		_ = a.Chip.FieldOff()
	}
	a.EmuActive = false
	a.syncPollFlags()
	a.SetToast("Emulation stopped")
}

func (a *App) randomizeUID() {
	if a.CurrentTag == nil {
		return
	}
	for i := range a.CurrentTag.UID {
		a.CurrentTag.UID[i] = uint8(rand.Intn(256))
	}
	if len(a.CurrentTag.UID) == 7 {
		a.CurrentTag.UID[0] = 0x04 // NXP manufacturer prefix
	}
	if a.EmuActive {
		a.startEmulation()
	} else {
		a.SetToast("New UID ready")
	}
}

func (a *App) toggleUIDLength() {
	if a.CurrentTag == nil {
		return
	}
	if len(a.CurrentTag.UID) == 7 {
		a.CurrentTag.UID = []byte{0xDE, 0xAD, 0xBE, 0xEF}
		a.CurrentTag.ATQA = [2]byte{0x04, 0x00}
		a.CurrentTag.SAK = 0x08
		a.CurrentTag.DeviceType = "MIFARE Classic 1K"
	} else {
		a.CurrentTag.UID = []byte{0x04, 0xA1, 0xB2, 0xC3, 0xD4, 0xE5, 0xF6}
		a.CurrentTag.ATQA = [2]byte{0x44, 0x00}
		a.CurrentTag.SAK = 0x00
		a.CurrentTag.DeviceType = "NTAG215"
	}
	if a.EmuActive {
		a.startEmulation()
	}
}

func (a *App) drawEmu(c *Canvas) {
	c.FillRect(6, 21, 308, 118, ColPanel)
	border := ColBorder
	if a.EmuActive {
		border = ColEmerald
	}
	c.DrawRect(6, 21, 308, 118, border)

	status := "IDLE"
	stCol := ColMuted
	if a.EmuActive {
		status = "EMULATING 13.56MHz"
		stCol = ColEmerald
	}
	c.DrawText(14, 27, status, stCol, 1)
	c.DrawText(224, 27, fmt.Sprintf("HITS: %d", a.ReaderHits), ColAmber, 1)

	if a.CurrentTag != nil {
		c.DrawText(14, 46, a.CurrentTag.DeviceType, ColCyan, 2)
		c.DrawText(14, 70, "UID : "+nfcfile.FormatHex(a.CurrentTag.UID), ColText, 1)
		c.DrawText(14, 84, fmt.Sprintf("ATQA: %02X %02X   SAK: %02X", a.CurrentTag.ATQA[0], a.CurrentTag.ATQA[1], a.CurrentTag.SAK), ColAmber, 1)
	}
	if a.EmuActive {
		r := 8 + int(a.tick%14)
		c.DrawCircle(280, 108, r, ColEmerald)
		c.DrawCircle(280, 108, 5, ColCyan)
	}
	c.DrawText(14, 116, "Hold phone/reader near Cap NFC antenna", ColMuted, 1)
}
