package ui

import (
	"context"
	"fmt"

	"nfc-studio/internal/config"
	"nfc-studio/internal/dbsync"
)

const settingsRowCount = 4

type settingsRow struct {
	label string
	val   string
	col   uint16
}

func (a *App) handleSettingsKey(act KeyAction) {
	switch act {
	case KeyUp:
		a.SettingsIdx = max(0, a.SettingsIdx-1)
	case KeyDown:
		a.SettingsIdx = min(settingsRowCount-1, a.SettingsIdx+1)
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
	switch a.SettingsIdx {
	case 0:
		a.Settings.MCPEnabled = !a.Settings.MCPEnabled
		a.persistSettings(false, toggleToastLabel("MCP Server", a.Settings.MCPEnabled))
	case 1:
		a.Settings.AutoScan = !a.Settings.AutoScan
		a.Scanning = a.Settings.AutoScan
		a.syncPollFlags()
		a.persistSettings(false, toggleToastLabel("Auto Scan", a.Settings.AutoScan))
	case 2:
		a.triggerDBSync()
	case 3:
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
	c.DrawText(6, 21, "Settings [F/X Select, Enter Action]", ColCyan, 1)
	c.FillRect(6, 32, 308, 86, ColPanel)
	c.DrawRect(6, 32, 308, 86, ColBorder)

	for i, row := range a.buildSettingsRows() {
		y := 36 + i*20
		bg, border := ColPanelAlt, ColBorder
		if i == a.SettingsIdx {
			bg, border = ColNavActive, ColEmerald
		}
		c.FillRect(10, y, 300, 17, bg)
		c.DrawRect(10, y, 300, 17, border)
		c.DrawText(16, y+5, row.label, ColText, 1)
		c.DrawText(304-len(row.val)*6, y+5, row.val, row.col, 1)
	}

	c.FillRect(6, 121, 308, 18, ColPanel)
	c.DrawRect(6, 121, 308, 18, ColBorder)
	c.DrawText(12, 126, "ssh <dev> nfc-studio --mcp", ColCyan, 1)
}

func (a *App) buildSettingsRows() [settingsRowCount]settingsRow {
	mcpVal, mcpCol := "DISABLED", ColAmber
	if a.Settings.MCPEnabled {
		mcpVal, mcpCol = "ENABLED", ColEmerald
	}
	scanVal, scanCol := "OFF", ColMuted
	if a.Settings.AutoScan {
		scanVal, scanCol = "ON", ColEmerald
	}
	syncVal, syncCol := "SYNC", ColCyan
	if a.SyncBusy {
		syncVal, syncCol = "SYNCING", ColAmber
	}
	return [settingsRowCount]settingsRow{
		{"MCP Server (SSH)", mcpVal, mcpCol},
		{"Auto Scan on Start", scanVal, scanCol},
		{"Sync DB Sources", syncVal, syncCol},
		{"Manage DB Sources", "OPEN>", ColCyan},
	}
}
