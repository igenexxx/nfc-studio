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

type helpPageContent struct {
	Title string
	Lines []string
}

var helpPages = []helpPageContent{
	{
		Title: "1/5 HOME MENU & NAVIGATION",
		Lines: []string{
			"F / X : Move up / down in menus",
			"Z / C : Flip help pages / sub-tabs",
			"Enter : Open item or primary action",
			"Esc   : Step back (Home -> Exit ask)",
			"4..8  : Bottom bar labeled softkeys",
			"W     : Jump to Write / Clone Card",
			"5 EXP : Toggle Simple / Expert Mode",
			"Never hot-swap Cap CC1101 & NFC!",
		},
	},
	{
		Title: "2/5 READ & CLONE (2 STEPS)",
		Lines: []string{
			"Step 1: Tap card/tag on Cap NFC coil",
			"  Auto-reads & freezes on Step 2!",
			"Step 2: Shows card type, UID & NDEF.",
			"  Enter / 4 EMU : Emulate immediately",
			"  5 SAVE  : Save .nfc & open My Cards",
			"  6 WRITE : Write dump to blank card",
			"  Space   : Read another card",
		},
	},
	{
		Title: "3/5 WRITE / CLONE TO BLANK CARD",
		Lines: []string{
			"Default AUTO detects blank card type!",
			"Space  : Preview blank & compatibility",
			"2-step : 4 ARM (5s) -> 4 WRITE / Enter",
			"NTAG   : User P04+ (auto-trims -> 213)",
			"Gen1A  : 0x40/43 Blk0 UID + 1K/4K data",
			"Gen2/MF: Pure-Go Crypto1 + Std fallback",
			"6 SAFE : Skips trailers  7: Quick NDEF",
		},
	},
	{
		Title: "4/5 MY CARDS & EMULATE",
		Lines: []string{
			"My Cards: ~/nfc-studio/tags/*.nfc",
			"  4 EMU  5 WRITE  6 INFO/DUMP  7 DEL",
			"Emulate Card: ST25R3916 Passive Target",
			"  NFC-A (4B/7B UID) & FeliCa (8B IDm)",
			"  4 START/STOP  5 CARDS  7 RST hits",
			"  Esc stops emulation & turns RF OFF.",
		},
	},
	{
		Title: "5/5 EXPERT MODE & MCP SERVER",
		Lines: []string{
			"Enable Expert Mode (HOME -> 5 EXP / SET)",
			"Unlocks: NFC Radar Lab (InductiveNet,",
			"  DumpNet), Hex Dump, Auto-Emu Probe,",
			"  Random UID (RND/TYPE), DB Sources.",
			"MCP (opt-in SET): nfc-studio --mcp",
			"31 tools: Auto-Write, Crypto1, EMV, ML",
		},
	},
}

func (a *App) drawHelpModal(c *Canvas) {
	page := helpPages[a.HelpPage%len(helpPages)]
	c.FillRect(10, 18, 300, 128, ColPanel)
	c.DrawRect(10, 18, 300, 128, ColEmerald)
	c.FillRect(11, 19, 298, 13, ColNavActive)
	c.DrawTextCentered(ScreenW/2, 22, page.Title, ColText, 1)
	for i, l := range page.Lines {
		c.DrawText(16, 36+i*12, l, ColText, 1)
	}
	c.HLine(12, 133, 296, ColBorder)
	c.DrawTextCentered(ScreenW/2, 136, "[Z/C] Page   [Enter/H] Close", ColAmber, 1)
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
