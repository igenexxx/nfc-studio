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

	c.FillRect(4, 3, 48, 11, ColEmerald)
	c.DrawText(7, 5, "NFC-LAB", ColBg, 1)

	c.FillRect(56, 3, 44, 11, ColPanelAlt)
	c.DrawRect(56, 3, 44, 11, ColCyan)
	c.DrawTextCentered(78, 5, a.Mode.String(), ColCyan, 1)

	c.DrawText(106, 5, "13.56 MHz ISO14443", ColText, 1)

	if a.SimMode {
		c.FillRect(262, 3, 54, 11, ColPanelAlt)
		c.DrawRect(262, 3, 54, 11, ColAmber)
		c.DrawTextCentered(289, 5, "SIM", ColAmber, 1)
	} else {
		c.FillRect(262, 3, 54, 11, ColPanelAlt)
		c.DrawRect(262, 3, 54, 11, ColEmerald)
		c.DrawTextCentered(289, 5, "ST25R39", ColEmerald, 1)
	}
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
		Title: "1/5 CONTROLS & HARDWARE",
		Lines: []string{
			"4..8   : Bottom bar quick actions",
			"Tab/8  : Switch mode (6 tabs +SET)",
			"F / X  : Scroll pages / saved list",
			"Z / C  : Dwell interval / Help page",
			"Enter  : Emulate / Toggle setting",
			"H / Q  : Toggle Help / Quit safely",
			"ST25R39: Cap NFC (/dev/spidev0.2)",
			"Never hot-swap the Cap module!",
		},
	},
	{
		Title: "2/5 SCAN, RADAR & TAG FIELDS",
		Lines: []string{
			"Radar: Live ST25R3916 ADC coupling %,",
			" icons: NTAG/Card/Lock/NFC-B/F/V/MTL",
			"UID : Unique Identifier (4/7/8B).",
			"ATQA: Answer To Request A (2B).",
			"SAK : 00=NTAG 08=MIFARE 20=ISO-DEP",
			"NDEF: Parsed URL/Text/WiFi user mem.",
			"EMU : NFC-A only + external RF %.",
			"5(EMU) clones, 6(SAVE) exports .nfc",
		},
	},
	{
		Title: "3/5 MEMORY HEX & NDEF",
		Lines: []string{
			"Color-coded NTAG page layout:",
			" Cyan (P00-02): UID, BCC, Lock bytes",
			" Amber (P03)  : Capability Container",
			" Green (P04+) : User memory / NDEF",
			"Press 4(NDEF) to view decoded URI/Txt",
			" or switch back to raw HEX/ASCII.",
			"Use F/X to scroll through all pages.",
			"6(SAVE) writes Flipper .nfc file.",
		},
	},
	{
		Title: "4/5 AUTO-EMU & DATABASES",
		Lines: []string{
			"PROBE steps through external DB packs",
			" (~/nfc-studio/databases/<pack>) or",
			" USER tags at dwell (0.5s..8.0s).",
			"4(AUTO/STOP) Space/Enter: instant halt",
			"F/X: step up past reaction (> / *)",
			"5(TRY)/Enter: single emulate tag",
			"6(SAVE): copy winner to USER library",
			"1/D + Del: remove DB tag (ignored)",
		},
	},
	{
		Title: "5/5 SAVED & SETTINGS",
		Lines: []string{
			"SAVED lists USER tags only:",
			" ~/nfc-studio/tags (Space/1=select,",
			" 6=DEL 1 or batch, 4=EMU, 5=DUMP).",
			"SET: Opt-in MCP, Sync DB Sources &",
			" Manage DB Sources (del dir/source).",
			"CLI: nfc-studio --sync",
			"MCP: ssh <dev> nfc-studio --mcp",
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
