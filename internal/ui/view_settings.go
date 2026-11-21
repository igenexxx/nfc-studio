package ui

import "nfc-studio/internal/config"

const settingsRowCount = 2

func (a *App) handleSettingsKey(act KeyAction) {
	switch act {
	case KeyUp:
		if a.SettingsIdx > 0 {
			a.SettingsIdx--
		}
	case KeyDown:
		if a.SettingsIdx < settingsRowCount-1 {
			a.SettingsIdx++
		}
	case KeyEnter, KeySpace, KeyNav4:
		a.toggleSelectedSetting()
	case KeyNav5:
		a.persistSettings(true, "Settings saved")
	case KeyNav6:
		a.ApplySettings(config.Default())
		a.persistSettings(false, "Settings reset")
	case KeyNav7:
		a.ShowHelp = true
		a.HelpPage = 0
	}
}

func (a *App) toggleSelectedSetting() {
	var label string
	if a.SettingsIdx == 0 {
		a.Settings.MCPEnabled = !a.Settings.MCPEnabled
		label = toggleToastLabel("MCP Server", a.Settings.MCPEnabled)
	} else {
		a.Settings.AutoScan = !a.Settings.AutoScan
		a.Scanning = a.Settings.AutoScan
		a.syncPollFlags()
		label = toggleToastLabel("Auto Scan", a.Settings.AutoScan)
	}
	a.persistSettings(false, label)
}

func toggleToastLabel(name string, enabled bool) string {
	if enabled {
		return name + " ENABLED"
	}
	return name + " DISABLED"
}

func (a *App) persistSettings(useDefaultFallback bool, successToast string) {
	path := a.ConfigPath
	if path == "" && useDefaultFallback {
		path = config.DefaultPath()
	}
	if path != "" {
		if err := config.Save(path, a.Settings); err != nil {
			a.SetToast("Save err!")
			return
		}
	}
	a.SetToast(successToast)
}

func (a *App) drawSettings(c *Canvas) {
	c.DrawText(6, 21, "Settings [F/X Select, Enter Toggle]", ColCyan, 1)
	c.FillRect(6, 32, 308, 86, ColPanel)
	c.DrawRect(6, 32, 308, 86, ColBorder)

	a.drawSettingRow(c, 0, "MCP Server (SSH)", a.Settings.MCPEnabled)
	a.drawSettingRow(c, 1, "Auto Scan on Start", a.Settings.AutoScan)

	c.DrawText(14, 96, "MCP is opt-in (disabled by default)", ColMuted, 1)

	c.FillRect(6, 121, 308, 18, ColPanel)
	c.DrawRect(6, 121, 308, 18, ColBorder)
	c.DrawText(12, 126, "ssh <dev> nfc-studio --mcp", ColCyan, 1)
}

func (a *App) drawSettingRow(c *Canvas, idx int, label string, enabled bool) {
	y := 38 + idx*26
	bg, border := ColPanelAlt, ColBorder
	if idx == a.SettingsIdx {
		bg, border = ColNavActive, ColEmerald
	}
	c.FillRect(10, y, 300, 21, bg)
	c.DrawRect(10, y, 300, 21, border)
	c.DrawText(16, y+7, label, ColText, 1)

	badgeText, badgeCol := "DISABLED", ColAmber
	if enabled {
		badgeText, badgeCol = "ENABLED", ColEmerald
	}
	c.FillRect(244, y+3, 60, 15, ColBg)
	c.DrawRect(244, y+3, 60, 15, badgeCol)
	c.DrawTextCentered(274, y+7, badgeText, badgeCol, 1)
}
