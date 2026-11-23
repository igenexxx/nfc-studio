package ui

import "fmt"

const homeVisibleRows = 5

type homeMenuItem struct {
	Tag      string
	TagCol   uint16
	Title    string
	Subtitle string
	Action   func(*App)
}

func (a *App) homeMenuItems() []homeMenuItem {
	cardSub := fmt.Sprintf("%d saved .nfc cards", len(a.SavedFiles))
	if len(a.SavedFiles) == 0 {
		cardSub = "Emulate & manage your cards"
	}
	items := []homeMenuItem{
		{Tag: "RD", TagCol: ColEmerald, Title: "Read & Clone", Subtitle: "Tap card, save or emulate", Action: (*App).enterReadWizard},
		{Tag: "MY", TagCol: ColCyan, Title: "My Cards", Subtitle: cardSub, Action: func(app *App) {
			app.refreshSavedFiles()
			app.SwitchMode(ModeSaved)
		}},
		{Tag: "EM", TagCol: ColAmber, Title: "Emulate Card", Subtitle: "13.56 MHz passive target", Action: func(app *App) {
			app.openEmulateQuick()
		}},
		{Tag: "ST", TagCol: ColMuted, Title: "Settings", Subtitle: "Expert, Auto-Scan & MCP", Action: func(app *App) {
			app.SettingsIdx = 0
			app.SwitchMode(ModeSettings)
		}},
	}
	if a.Settings.ExpertMode {
		items = append(items,
			homeMenuItem{Tag: "SC", TagCol: ColMagenta, Title: "NFC Radar Lab", Subtitle: "ADC radar, Inductive & DumpNet", Action: func(app *App) {
				app.Scanning = true
				app.SwitchMode(ModeScan)
			}},
			homeMenuItem{Tag: "DM", TagCol: ColMagenta, Title: "Hex / NDEF Dump", Subtitle: "Color-coded page inspector", Action: func(app *App) {
				app.DumpScroll = 0
				app.SwitchMode(ModeDump)
			}},
			homeMenuItem{Tag: "PR", TagCol: ColAmber, Title: "Auto-Emu Probe", Subtitle: "Cycle DB packs & reader IAT", Action: func(app *App) {
				app.refreshProbePacks()
				app.SwitchMode(ModeProbe)
			}},
		)
	}
	return items
}

func (a *App) openEmulateQuick() {
	if a.CurrentTag == nil && len(a.SavedFiles) > 0 {
		a.CurrentTag = a.SavedFiles[0].Tag
		a.updateNDEF()
	}
	if a.CurrentTag == nil {
		a.SetToast("No card loaded - read one!")
		a.enterReadWizard()
		return
	}
	a.SwitchMode(ModeEmu)
}

func (a *App) clampHomeCursor(n int) {
	if n <= 0 {
		a.HomeCursor = 0
		a.HomeScroll = 0
		return
	}
	a.HomeCursor = min(max(a.HomeCursor, 0), n-1)
	if a.HomeCursor < a.HomeScroll {
		a.HomeScroll = a.HomeCursor
	}
	if a.HomeCursor >= a.HomeScroll+homeVisibleRows {
		a.HomeScroll = a.HomeCursor - homeVisibleRows + 1
	}
}

func (a *App) handleHomeKey(act KeyAction) bool {
	if a.ConfirmExit {
		if act == KeyEnter || act == KeyBack || act == KeyNav8 {
			a.stopActiveProbe()
			if a.EmuActive {
				a.stopEmulation()
			}
			return true
		}
		a.ConfirmExit = false
		return false
	}
	items := a.homeMenuItems()
	switch act {
	case KeyUp:
		a.HomeCursor = (a.HomeCursor + len(items) - 1) % len(items)
		a.clampHomeCursor(len(items))
	case KeyDown:
		a.HomeCursor = (a.HomeCursor + 1) % len(items)
		a.clampHomeCursor(len(items))
	case KeyEnter, KeyNav4:
		items[min(a.HomeCursor, len(items)-1)].Action(a)
	case KeyNav5:
		a.toggleExpertMode()
	case KeyNav6:
		a.enterReadWizard()
	case KeyNav7:
		a.openEmulateQuick()
	case KeyBack, KeyNav8:
		a.ConfirmExit = true
	}
	return false
}

func (a *App) toggleExpertMode() {
	a.Settings.ExpertMode = !a.Settings.ExpertMode
	a.clampHomeCursor(len(a.homeMenuItems()))
	msg := "Simple Mode ON"
	if a.Settings.ExpertMode {
		msg = "Expert Mode ON (+3 tools)"
	}
	a.persistSettings(false, msg)
}

func (a *App) drawHome(c *Canvas) {
	items := a.homeMenuItems()
	a.clampHomeCursor(len(items))

	c.DrawText(8, 21, "SELECT ACTION (F/X Move, Enter Open)", ColMuted, 1)
	if len(items) > homeVisibleRows {
		ind := fmt.Sprintf("%d/%d", a.HomeCursor+1, len(items))
		c.DrawText(290, 21, ind, ColMuted, 1)
	}
	for row := range homeVisibleRows {
		idx := a.HomeScroll + row
		if idx >= len(items) {
			break
		}
		a.drawHomeRow(c, idx, 32+row*22, items[idx])
	}
	if a.ConfirmExit {
		drawConfirmBanner(c, "Exit NFC Studio? [Enter/Esc=YES]")
	}
}

func (a *App) homeNavLabels() [5]string {
	expLbl := "EXP-"
	if a.Settings.ExpertMode {
		expLbl = "EXP+"
	}
	return [5]string{"OPEN", expLbl, "READ", "EMU", "QUIT"}
}

func (a *App) drawHomeRow(c *Canvas, idx, y int, it homeMenuItem) {
	focused := idx == a.HomeCursor
	bg, border, titleCol := ColPanel, ColBorder, ColText
	if focused {
		bg, border, titleCol = ColPanelAlt, ColEmerald, ColEmerald
	}
	c.FillRect(6, y, 308, 20, bg)
	c.DrawRect(6, y, 308, 20, border)
	if focused {
		c.FillRect(6, y, 3, 20, ColEmerald)
	}
	c.FillRect(12, y+4, 18, 12, ColBg)
	c.DrawRect(12, y+4, 18, 12, it.TagCol)
	c.DrawTextCentered(21, y+6, it.Tag, it.TagCol, 1)

	c.DrawText(36, y+6, truncate(it.Title, 16), titleCol, 1)
	c.DrawText(142, y+6, truncate(it.Subtitle, 26), ColMuted, 1)
}
