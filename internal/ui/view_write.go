package ui

import (
	"fmt"
	"time"

	"nfc-studio/internal/nfcfile"
)

var mifareWriteModeLabels = [6]string{
	"AUTO (Detect Blank)",
	"Gen1A Full (0x40/43)",
	"Gen1A UID Only",
	"Gen2 CUID Full (Cr1)",
	"Gen2 CUID UID (Cr1)",
	"Regular Sectors (Cr1)",
}

var ndefKindLabels = [3]string{"URL", "TEXT", "WIFI"}

func (a *App) handleWriteKey(act KeyAction) {
	if a.WriteBusy {
		if act == KeyNav4 || act == KeyBack || act == KeyNav8 {
			a.cancelWriteJob()
		}
		return
	}
	if act == KeyBack || act == KeyNav8 {
		a.WriteArmedUntil = time.Time{}
		target := a.WriteReturnMode
		if target == ModeWrite {
			target = ModeHome
		}
		a.SwitchMode(target)
		return
	}
	switch act {
	case KeyLeft, KeyRight:
		a.toggleWriteSubMode()
	case KeyUp:
		if a.WriteSub == WriteSubNDEF {
			a.cycleNDEFPreset(-1)
		}
	case KeyDown:
		if a.WriteSub == WriteSubNDEF {
			a.cycleNDEFPreset(1)
		}
	case KeyNav4, KeyEnter:
		a.handleWriteArmOrStart()
	case KeySpace:
		a.WriteArmedUntil = time.Time{}
		a.startBlankProbePreview()
	case KeyNav5:
		a.WriteArmedUntil = time.Time{}
		if a.WriteSub == WriteSubNDEF {
			a.WriteNDEFKind = (a.WriteNDEFKind + 1) % len(ndefKindLabels)
			a.WriteNDEFPreset = 0
		} else {
			a.WriteMifareMode = (a.WriteMifareMode + 1) % len(mifareWriteModeLabels)
		}
	case KeyNav6:
		a.WriteArmedUntil = time.Time{}
		if a.WriteSub == WriteSubNDEF {
			a.saveQuickNDEFTag()
		} else {
			a.WriteTrailers = !a.WriteTrailers
			if a.WriteTrailers {
				a.SetToast("WARN: Sector trailers ON")
			} else {
				a.SetToast("Safe: Trailers skipped")
			}
		}
	case KeyNav7:
		a.toggleWriteSubMode()
	}
}

func (a *App) toggleWriteSubMode() {
	a.WriteArmedUntil = time.Time{}
	a.WriteResultSet = false
	if a.WriteSub == WriteSubDump {
		a.WriteSub = WriteSubNDEF
	} else {
		a.WriteSub = WriteSubDump
	}
}

func (a *App) cycleNDEFPreset(delta int) {
	a.WriteArmedUntil = time.Time{}
	list := ndefPresetsByKind[a.WriteNDEFKind%len(ndefPresetsByKind)]
	a.WriteNDEFPreset = (a.WriteNDEFPreset + len(list) + delta) % len(list)
}

func (a *App) handleWriteArmOrStart() {
	plan, err := a.currentWritePlan()
	if err != nil || plan.Strategy == nfcfile.StrategyUnsupported {
		reason := "Unsupported card"
		if err != nil {
			reason = err.Error()
		} else if plan.Reason != "" {
			reason = plan.Reason
		}
		a.SetToast(truncate(reason, 28))
		return
	}
	now := time.Now()
	if now.Before(a.WriteArmedUntil) {
		a.startWriteJob()
		return
	}
	a.WriteResultSet = false
	a.WriteArmedUntil = now.Add(writeArmWindow)
	a.SetToast("Armed 5s: press 4 WRITE!")
}

func (a *App) writeNavLabels() [5]string {
	if a.WriteBusy {
		return [5]string{"STOP", "", "", "", "BACK"}
	}
	s4 := "ARM"
	if time.Now().Before(a.WriteArmedUntil) {
		s4 = "WRITE"
	}
	if a.WriteSub == WriteSubNDEF {
		return [5]string{s4, "KIND", "SAVE", "DUMP", "BACK"}
	}
	safeLbl := "SAFE"
	if a.WriteTrailers {
		safeLbl = "TRLR!"
	}
	return [5]string{s4, "MODE", safeLbl, "NDEF", "BACK"}
}

func (a *App) drawWrite(c *Canvas) {
	a.drawWriteSubTabs(c)
	plan, _ := a.currentWritePlan()
	if a.WriteSub == WriteSubNDEF {
		a.drawWriteNDEFBody(c, plan)
	} else {
		a.drawWriteDumpBody(c, plan)
	}
	a.drawWriteStatusPanel(c, plan)
}

func (a *App) drawWriteSubTabs(c *Canvas) {
	drawTab := func(x, w int, label string, active bool) {
		bg, border, fg := ColPanel, ColBorder, ColMuted
		if active {
			bg, border, fg = ColNavActive, ColEmerald, ColEmerald
		}
		c.FillRect(x, 20, w, 13, bg)
		c.DrawRect(x, 20, w, 13, border)
		c.DrawTextCentered(x+w/2, 23, label, fg, 1)
	}
	drawTab(6, 151, "DUMP -> BLANK CARD", a.WriteSub == WriteSubDump)
	drawTab(163, 151, "QUICK NDEF WRITER", a.WriteSub == WriteSubNDEF)
}

func (a *App) drawWriteDumpBody(c *Canvas, plan nfcfile.WritePlan) {
	c.FillRect(6, 36, 308, 66, ColPanel)
	c.DrawRect(6, 36, 308, 66, ColBorder)
	if a.CurrentTag == nil {
		c.DrawTextCentered(ScreenW/2, 56, "No .nfc card loaded", ColAmber, 1)
		c.DrawTextCentered(ScreenW/2, 72, "Read a card, pick My Cards, or press 7 NDEF", ColMuted, 1)
		return
	}
	badge, badgeCol := writeStrategyBadge(plan.Strategy)
	if a.isWriteAutoMode() && plan.Strategy != nfcfile.StrategyUnsupported {
		badge, badgeCol = "AUTO DET", ColEmerald
	}
	c.DrawText(12, 41, truncate(a.CurrentTag.DeviceType, 30), ColCyan, 1)
	c.FillRect(244, 39, 64, 11, ColPanelAlt)
	c.DrawRect(244, 39, 64, 11, badgeCol)
	c.DrawTextCentered(276, 41, badge, badgeCol, 1)

	uidLine := "UID: " + nfcfile.FormatHex(a.CurrentTag.UID)
	if len(a.CurrentTag.UID) == 4 {
		bcc := a.CurrentTag.UID[0] ^ a.CurrentTag.UID[1] ^ a.CurrentTag.UID[2] ^ a.CurrentTag.UID[3]
		uidLine += fmt.Sprintf("  BCC:%02X", bcc)
	}
	c.DrawText(12, 54, truncate(uidLine, 48), ColText, 1)

	if plan.Strategy == nfcfile.StrategyUnsupported {
		c.DrawText(12, 68, "CANNOT WRITE TO BLANK CARD:", ColAmber, 1)
		c.DrawText(12, 82, truncate(plan.Reason, 48), ColMuted, 1)
		return
	}
	c.DrawText(12, 67, truncate(plan.Summary, 48), ColEmerald, 1)
	if a.WriteDetectedBlank != "" {
		bCol := ColCyan
		if a.WriteCompatLevel == nfcfile.CompatIncompatible {
			bCol = ColAmber
		} else if a.WriteCompatLevel == nfcfile.CompatExact || a.WriteCompatLevel == nfcfile.CompatAdapted {
			bCol = ColEmerald
		}
		c.DrawText(12, 79, truncate("Blank: "+a.WriteDetectedBlank, 48), bCol, 1)
		c.DrawText(12, 91, truncate(a.WriteCompatNote, 48), ColMuted, 1)
		return
	}
	if isMifareStrategy(plan.Strategy) {
		modeTxt := "5 MODE: " + mifareWriteModeLabels[a.WriteMifareMode%len(mifareWriteModeLabels)]
		c.DrawText(12, 79, truncate(modeTxt, 48), ColAmber, 1)
		c.DrawText(12, 91, truncate(plan.Notice, 48), trailerNoticeColor(a.WriteTrailers), 1)
		return
	}
	c.DrawText(12, 80, truncate(plan.Notice, 48), ColMuted, 1)
	c.DrawText(12, 91, "Space: Detect blank   5: Override mode", ColMuted, 1)
}

func (a *App) drawWriteNDEFBody(c *Canvas, plan nfcfile.WritePlan) {
	c.FillRect(6, 36, 308, 66, ColPanel)
	c.DrawRect(6, 36, 308, 66, ColBorder)
	p := a.currentNDEFPreset()
	kind := ndefKindLabels[a.WriteNDEFKind%len(ndefKindLabels)]
	c.DrawText(12, 41, fmt.Sprintf("Kind [5]: %s   Preset [F/X]:", kind), ColCyan, 1)
	c.DrawText(12, 56, truncate(p.Label, 48), ColEmerald, 1)
	c.DrawText(12, 71, truncate(plan.Summary, 48), ColText, 1)
	c.DrawText(12, 86, "Target: Blank NTAG213/215/216 (6 SAVE .nfc)", ColMuted, 1)
}

func (a *App) drawWriteStatusPanel(c *Canvas, plan nfcfile.WritePlan) {
	const y, h = 105, 34
	now := time.Now()
	switch {
	case a.WriteBusy:
		c.FillRect(6, y, 308, h, ColPanelAlt)
		c.DrawRect(6, y, 308, h, ColCyan)
		tot := max(1, a.WriteTotalCount)
		pct := min(100, a.WriteDoneCount*100/tot)
		c.DrawText(12, y+5, truncate(a.WriteStage, 34), ColCyan, 1)
		c.DrawText(254, y+5, fmt.Sprintf("%3d%%", pct), ColEmerald, 1)
		c.FillRect(12, y+19, 296, 9, ColBg)
		c.DrawRect(12, y+19, 296, 9, ColBorder)
		fillW := (294 * pct) / 100
		if fillW > 0 {
			c.FillRect(13, y+20, fillW, 7, ColEmerald)
		}
	case a.WriteResultSet:
		border, fg := ColAmber, ColAmber
		if a.WriteResultOK {
			border, fg = ColEmerald, ColEmerald
		}
		c.FillRect(6, y, 308, h, ColPanelAlt)
		c.DrawRect(6, y, 308, h, border)
		c.DrawTextCentered(ScreenW/2, y+7, truncate(a.WriteResultMsg, 48), fg, 1)
		c.DrawTextCentered(ScreenW/2, y+20, "Press 4 ARM to write again or 8 BACK", ColMuted, 1)
	case now.Before(a.WriteArmedUntil):
		rem := int(a.WriteArmedUntil.Sub(now).Seconds()) + 1
		c.FillRect(6, y, 308, h, ColPanelAlt)
		c.DrawRect(6, y, 308, h, ColAmber)
		c.DrawTextCentered(ScreenW/2, y+6, fmt.Sprintf("ARMED (%ds)! Hold blank card on Cap coil", rem), ColAmber, 1)
		c.DrawTextCentered(ScreenW/2, y+20, "Press [4 WRITE] or [Enter] to WRITE NOW", ColText, 1)
	default:
		c.FillRect(6, y, 308, h, ColPanel)
		c.DrawRect(6, y, 308, h, ColBorder)
		if plan.Strategy == nfcfile.StrategyUnsupported {
			c.DrawTextCentered(ScreenW/2, y+12, "Switch to 7 NDEF or load an NTAG/MIFARE card", ColMuted, 1)
			return
		}
		c.DrawTextCentered(ScreenW/2, y+6, "Hold blank card flat on Cap NFC coil", ColText, 1)
		c.DrawTextCentered(ScreenW/2, y+20, "Press [4 ARM] (2-step safe confirm)", ColEmerald, 1)
	}
}

func writeStrategyBadge(s nfcfile.WriteStrategy) (string, uint16) {
	switch s {
	case nfcfile.StrategyNTAGPages:
		return "NTAG 0xA2", ColEmerald
	case nfcfile.StrategyMagicGen1A:
		return "GEN1A MF", ColCyan
	case nfcfile.StrategyMagicGen2:
		return "GEN2 CR1", ColMagenta
	case nfcfile.StrategyMifareSectors:
		return "MF SECT", ColAmber
	case nfcfile.StrategyNFCVBlocks:
		return "NFC-V 21", ColCyan
	default:
		return "UNSUP", ColMuted
	}
}

func isMifareStrategy(s nfcfile.WriteStrategy) bool {
	return s == nfcfile.StrategyMagicGen1A || s == nfcfile.StrategyMagicGen2 || s == nfcfile.StrategyMifareSectors
}

func trailerNoticeColor(writeTrailers bool) uint16 {
	if writeTrailers {
		return ColAmber
	}
	return ColMuted
}
