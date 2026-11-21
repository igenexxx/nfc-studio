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
		Title: "1/4 CONTROLS & HARDWARE",
		Lines: []string{
			"4..8   : Bottom bar quick actions",
			"Tab/8  : Switch mode (5 tabs +SET)",
			"F / X  : Scroll pages / saved list",
			"Enter  : Emulate / Toggle setting",
			"H / Q  : Toggle Help / Quit safely",
			"ST25R39: Cap NFC (/dev/spidev0.2)",
			"SET    : Opt-in MCP server (SSH)",
			"Never hot-swap the Cap module!",
		},
	},
	{
		Title: "2/4 SCAN & TAG FIELDS",
		Lines: []string{
			"UID : Unique Identifier (4 or 7B).",
			"ATQA: Answer To Request A (2B).",
			"SAK : Select Acknowledge (card type).",
			"  SAK 00 = NTAG / Ultralight",
			"  SAK 08 = MIFARE Classic 1K",
			"  SAK 20 = ISO14443-4 / DESFire",
			"NDEF: Parsed URL/Text from user mem.",
			"5(EMU) clones, 6(SAVE) exports .nfc",
		},
	},
	{
		Title: "3/4 MEMORY HEX & NDEF",
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
		Title: "4/4 EMULATION & SAVED",
		Lines: []string{
			"EMU uses ST25R3916 Passive Target A",
			" memory (0xA0) for REQA/SELECT.",
			"5(RND) generates a fresh random UID.",
			"6(4B/7B) switches NTAG <-> MFC1K UID.",
			"SAVED reads Flipper .nfc files from:",
			" ~/nfc-studio/tags & ~/zerosub/nfc",
			"4(EMU)=Emulate  5(DUMP)=Inspect",
			"6(DEL)=Delete   7(SYNC)=Rescan",
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
