package ui

import (
	"context"
	"fmt"

	"nfc-studio/internal/config"
	"nfc-studio/internal/dbsync"
)

type settingsRow struct {
	id    string
	label string
	val   string
	col   uint16
}

func (a *App) handleSettingsKey(act KeyAction) {
	rows := a.buildSettingsRows()
	if a.SettingsIdx >= len(rows) {
		a.SettingsIdx = max(0, len(rows)-1)
	}
	switch act {
	case KeyUp:
		a.SettingsIdx = max(0, a.SettingsIdx-1)
	case KeyDown:
		a.SettingsIdx = min(len(rows)-1, a.SettingsIdx+1)
	case KeyEnter, KeySpace, KeyNav4:
		a.toggleSelectedSetting(rows[a.SettingsIdx].id)
	case KeyNav5:
		a.persistSettings(true, "Settings saved")
	case KeyNav6:
		if !a.Settings.ExpertMode {
			a.triggerDBSync()
			return
		}
		a.ApplySettings(config.Default())
		a.persistSettings(false, "Settings reset")
	case KeyNav7:
		if a.Settings.ExpertMode {
			a.ShowHelp = true
			a.HelpPage = 0
		}
	case KeyNav8, KeyBack:
		a.goHome()
	}
}

func (a *App) toggleSelectedSetting(id string) {
	switch id {
	case "expert":
		a.toggleExpertMode()
		rows := a.buildSettingsRows()
		if a.SettingsIdx >= len(rows) {
			a.SettingsIdx = 0
		}
	case "mcp":
		a.Settings.MCPEnabled = !a.Settings.MCPEnabled
		a.persistSettings(false, toggleToastLabel("MCP Server", a.Settings.MCPEnabled))
	case "autoscan":
		a.Settings.AutoScan = !a.Settings.AutoScan
		a.Scanning = a.Settings.AutoScan
		a.syncPollFlags()
		a.persistSettings(false, toggleToastLabel("Auto Scan", a.Settings.AutoScan))
	case "sync":
		a.triggerDBSync()
	case "sources":
		a.openSourcesManage()
	}
}

func (a *App) triggerDBSync() {
	if a.SyncBusy {
		if a.syncCancel != nil {
			a.syncCancel()
		}
		a.SetToast("Cancelling sync...")
		return
	}
	sources, err := dbsync.LoadSources(a.SourcesPath)
	if err != nil {
		a.SetToast("Sources err!")
		return
	}
	if len(sources) == 0 {
		a.SetToast("No DB sources")
		return
	}
	syncer := a.Syncer
	if syncer == nil && a.Store != nil {
		syncer = dbsync.NewSyncer(a.Store.DatabasesDir)
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.syncCancel = cancel
	a.SyncBusy = true
	a.SetToast(fmt.Sprintf("Syncing %d DB(s)...", len(sources)))

	go func() {
		defer cancel()
		reports := syncer.SyncAll(ctx, sources)
		select {
		case a.syncDoneCh <- reports:
		default:
		}
	}()
}

func (a *App) pollSyncResults() {
	if a.syncDoneCh == nil {
		return
	}
	select {
	case reports := <-a.syncDoneCh:
		a.SyncBusy = false
		a.syncCancel = nil
		a.refreshProbePacks()
		a.refreshManagedSources()
		a.SetToast(summarizeSyncReports(reports))
	default:
	}
}

func summarizeSyncReports(reports []dbsync.SyncReport) string {
	var added, updated, errs int
	for _, r := range reports {
		added += r.Added
		updated += r.Updated
		if r.Err != "" || r.Failed > 0 {
			errs++
		}
	}
	if errs > 0 && added == 0 && updated == 0 {
		return fmt.Sprintf("Sync err (%d)", errs)
	}
	if added == 0 && updated == 0 {
		return "DBs up to date"
	}
	return fmt.Sprintf("Sync: +%d new ~%d upd", added, updated)
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
	rows := a.buildSettingsRows()
	if a.SettingsIdx >= len(rows) {
		a.SettingsIdx = max(0, len(rows)-1)
	}
	c.FillRect(6, 22, 308, 96, ColPanel)
	c.DrawRect(6, 22, 308, 96, ColBorder)

	stepY := 22
	if len(rows) > 4 {
		stepY = 18
	}
	for i, row := range rows {
		y := 26 + i*stepY
		bg, border := ColPanelAlt, ColBorder
		if i == a.SettingsIdx {
			bg, border = ColNavActive, ColEmerald
		}
		c.FillRect(10, y, 300, stepY-3, bg)
		c.DrawRect(10, y, 300, stepY-3, border)
		c.DrawText(16, y+4, row.label, ColText, 1)
		c.DrawText(304-len(row.val)*6, y+4, row.val, row.col, 1)
	}

	c.FillRect(6, 121, 308, 18, ColPanel)
	c.DrawRect(6, 121, 308, 18, ColBorder)
	c.DrawTextCentered(ScreenW/2, 126, "ssh <dev> nfc-studio --mcp", ColCyan, 1)
}

func (a *App) buildSettingsRows() []settingsRow {
	expVal, expCol := "OFF (Simple)", ColCyan
	if a.Settings.ExpertMode {
		expVal, expCol = "ON (Full Lab)", ColMagenta
	}
	mcpVal, mcpCol := "OFF (safe)", ColAmber
	if a.Settings.MCPEnabled {
		mcpVal, mcpCol = "ON (--mcp)", ColEmerald
	}
	scanVal, scanCol := "OFF", ColMuted
	if a.Settings.AutoScan {
		scanVal, scanCol = "ON", ColEmerald
	}
	syncVal, syncCol := "Run Now", ColCyan
	if a.SyncBusy {
		syncVal, syncCol = "SYNCING", ColAmber
	}
	rows := []settingsRow{
		{"expert", "Expert Mode", expVal, expCol},
		{"autoscan", "Auto Scan", scanVal, scanCol},
		{"mcp", "MCP Server (SSH)", mcpVal, mcpCol},
		{"sync", "Sync DB Sources", syncVal, syncCol},
	}
	if a.Settings.ExpertMode {
		rows = append(rows, settingsRow{"sources", "Manage DB Sources", "OPEN>", ColCyan})
	}
	return rows
}
