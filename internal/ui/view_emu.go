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
	if !isEmulatableTag(a.CurrentTag) {
		a.SetToast("EMU supports NFC-A & FeliCa only")
		return
	}
	if err := a.startChipEmulation(a.CurrentTag); err != nil {
		a.SetToast("EMU err")
		return
	}
	a.EmuActive = true
	a.syncPollFlags()
	a.SetToast("Emulating " + a.CurrentTag.DeviceType)
}

func (a *App) startChipEmulation(tag *nfcfile.NfcTag) error {
	if a.Chip == nil || tag == nil {
		return nil
	}
	if isFeliCaTag(tag) {
		if f, ok := a.Chip.(interface {
			StartEmulationF(idm, pmm []byte, sysCode [2]byte) error
		}); ok {
			return f.StartEmulationF(tag.UID, tag.PMm[:], tag.SystemCode)
		}
		return fmt.Errorf("hardware does not support FeliCa emulation")
	}
	return a.Chip.StartEmulation(tag.UID, tag.ATQA, tag.SAK)
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
	switch len(a.CurrentTag.UID) {
	case 7:
		a.CurrentTag.UID[0] = 0x04 // NXP manufacturer prefix
	case 8:
		a.CurrentTag.UID[0] = 0x01 // FeliCa IDm prefix
	}
	if a.EmuActive {
		a.startEmulation()
	} else {
		a.SetToast("New ID ready")
	}
}

func (a *App) toggleUIDLength() {
	if a.CurrentTag == nil {
		return
	}
	switch {
	case isFeliCaTag(a.CurrentTag):
		a.CurrentTag.Protocol = "NFC-A"
		a.CurrentTag.UID = []byte{0x04, 0xA1, 0xB2, 0xC3, 0xD4, 0xE5, 0xF6}
		a.CurrentTag.ATQA = [2]byte{0x44, 0x00}
		a.CurrentTag.SAK = 0x00
		a.CurrentTag.DeviceType = "NTAG215"
	case len(a.CurrentTag.UID) == 7:
		a.CurrentTag.Protocol = "NFC-A"
		a.CurrentTag.UID = []byte{0xDE, 0xAD, 0xBE, 0xEF}
		a.CurrentTag.ATQA = [2]byte{0x04, 0x00}
		a.CurrentTag.SAK = 0x08
		a.CurrentTag.DeviceType = "MIFARE Classic 1K"
	default:
		a.CurrentTag.Protocol = "NFC-F"
		a.CurrentTag.UID = []byte{0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF}
		a.CurrentTag.PMm = [8]byte{0x01, 0x20, 0x22, 0x04, 0x27, 0x67, 0x4E, 0xFF}
		a.CurrentTag.SystemCode = [2]byte{0x88, 0xB4}
		a.CurrentTag.DeviceType = "FeliCa"
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
	if a.LastActivity.Label != "" {
		c.DrawText(134, 27, a.LastActivity.Label, ColCyan, 1)
	}
	c.DrawText(224, 27, fmt.Sprintf("HITS: %d", a.ReaderHits), ColAmber, 1)

	if a.CurrentTag != nil {
		c.DrawText(14, 46, a.CurrentTag.DeviceType, ColCyan, 2)
		if isFeliCaTag(a.CurrentTag) {
			c.DrawText(14, 70, "IDm : "+truncate(nfcfile.FormatHex(a.CurrentTag.UID), 24), ColText, 1)
			c.DrawText(14, 84, fmt.Sprintf("SYS : %02X %02X   212/424k", a.CurrentTag.SystemCode[0], a.CurrentTag.SystemCode[1]), ColAmber, 1)
		} else {
			c.DrawText(14, 70, "UID : "+nfcfile.FormatHex(a.CurrentTag.UID), ColText, 1)
			c.DrawText(14, 84, fmt.Sprintf("ATQA: %02X %02X   SAK: %02X", a.CurrentTag.ATQA[0], a.CurrentTag.ATQA[1], a.CurrentTag.SAK), ColAmber, 1)
		}
	}
	a.drawEmuRadar(c)
	c.DrawText(14, 118, "Hold phone/reader near Cap NFC antenna", ColMuted, 1)
}
