package ui

func scanLabel(on bool) string {
	if on {
		return "HOLD"
	}
	return "SCAN"
}

func dumpTabLabel(ndef bool) string {
	if ndef {
		return "HEX"
	}
	return "NDEF"
}

func emuLabel(on bool) string {
	if on {
		return "STOP"
	}
	return "START"
}

func (a *App) drawHeader(c *Canvas) {
	c.FillRect(0, 0, ScreenW, 17, ColPanel)
	c.HLine(0, 17, ScreenW, ColBorder)
	a.drawHeaderTitle(c)
	a.drawHeaderBadges(c)
}

func (a *App) drawHeaderTitle(c *Canvas) {
	if a.Mode == ModeHome && !a.ShowSources {
		c.FillRect(4, 3, 56, 11, ColEmerald)
		c.DrawText(7, 5, "NFC-LAB", ColBg, 1)
		modeLbl, modeCol := "SIMPLE", ColMuted
		if a.Settings.ExpertMode {
			modeLbl, modeCol = "EXPERT", ColMagenta
		}
		c.DrawText(66, 5, modeLbl, modeCol, 1)
		return
	}
	title := a.Mode.Title()
	if a.ShowSources {
		title = "DB SOURCES"
	}
	c.DrawText(6, 5, "< "+truncate(title, 22), ColEmerald, 1)
}

func (a *App) drawHeaderBadges(c *Canvas) {
	if a.Settings.ExpertMode && a.Mode != ModeHome {
		c.FillRect(228, 3, 28, 11, ColPanelAlt)
		c.DrawRect(228, 3, 28, 11, ColMagenta)
		c.DrawTextCentered(242, 5, "EXP", ColMagenta, 1)
	}
	if a.SimMode {
		c.FillRect(262, 3, 54, 11, ColPanelAlt)
		c.DrawRect(262, 3, 54, 11, ColAmber)
		c.DrawTextCentered(289, 5, "SIM", ColAmber, 1)
		return
	}
	c.FillRect(262, 3, 54, 11, ColPanelAlt)
	c.DrawRect(262, 3, 54, 11, ColEmerald)
	c.DrawTextCentered(289, 5, "ST25R39", ColEmerald, 1)
}

func drawConfirmBanner(c *Canvas, msg string) {
	c.FillRect(14, 121, 292, 18, ColPanel)
	c.DrawRect(14, 121, 292, 18, ColAmber)
	c.DrawTextCentered(ScreenW/2, 126, truncate(msg, 46), ColAmber, 1)
}

func (a *App) drawNavBar(c *Canvas, labels [5]string) {
	c.FillRect(0, 143, ScreenW, 27, ColNavBg)
	c.HLine(0, 143, ScreenW, ColBorder)
	keys := [5]string{"4", "5", "6", "7", "8"}
	for i, cx := range NavKeyCenters {
		bx := cx - 25
		c.FillRect(bx, 146, 50, 21, ColPanelAlt)
		c.DrawRect(bx, 146, 50, 21, ColBorder)
		c.FillRect(bx+2, 148, 9, 9, ColEmerald)
		c.DrawText(bx+4, 149, keys[i], ColBg, 1)
		c.DrawTextCentered(cx+4, 154, labels[i], ColText, 1)
	}
}

func bytesToASCII(b []byte) string {
	out := make([]byte, len(b))
	for i, v := range b {
		if v >= 32 && v <= 126 {
			out[i] = v
		} else {
			out[i] = '.'
		}
	}
	return string(out)
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-2] + ".."
}
