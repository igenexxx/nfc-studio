package ui

import (
	"os"
	"path/filepath"
	"time"

	"nfc-studio/internal/config"
	"nfc-studio/internal/dbsync"
	"nfc-studio/internal/library"
	"nfc-studio/internal/nfcfile"
	"nfc-studio/internal/nfcml"
)

// App coordinates the NFC Studio 320x170 framebuffer interface across all modes.
type App struct {
	Chip    NFCReader
	SimMode bool
	TagsDir string
	Mode    Mode

	HomeState
	ReadWizardState
	ScanState
	EmuState
	SavedState
	ProbeState
	SourcesManageState
	SettingsState
	MLState
	ChromeState
	PollerState
	WriteState
}

func NewApp(chip NFCReader, tagsDir string) *App {
	defaults := config.Default()
	var extraDirs []string
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		extraDirs = []string{filepath.Join(home, "zerosub", "nfc")}
	}
	store := library.NewStore(tagsDir, library.DefaultDatabasesDir(), extraDirs)

	a := &App{
		Chip:      chip,
		SimMode:   chip == nil,
		TagsDir:   tagsDir,
		Mode:      ModeHome,
		ScanState: ScanState{Scanning: defaults.AutoScan},
		ProbeState: ProbeState{
			Store: store,
			Probe: library.NewProbeSession(defaults.ProbeIntervalMs),
		},
		SettingsState: SettingsState{
			SourcesPath: dbsync.DefaultSourcesPath(),
			Settings:    defaults,
			Syncer:      dbsync.NewSyncer(store.DatabasesDir),
			syncDoneCh:  make(chan []dbsync.SyncReport, 1),
		},
		MLState: MLState{
			ReaderIAT: nfcml.NewReaderIATTracker(),
			FieldBase: nfcml.NewFieldBaseline(),
		},
		PollerState: newPollerState(),
		WriteState:  newWriteState(),
	}
	if a.SimMode {
		a.CurrentTag = defaultSimTag()
		a.updateNDEF()
	}
	a.syncPollFlags()
	a.refreshSavedFiles()
	a.refreshProbePacks()
	return a
}

// Close cancels any active background DB sync, card write, and stops emulation cleanly.
func (a *App) Close() {
	a.cancelWriteJob()
	if a.syncCancel != nil {
		a.syncCancel()
		a.syncCancel = nil
	}
	if a.EmuActive {
		a.stopEmulation()
	}
}

// SetLibraryStore injects a custom Store (e.g. for custom databasesDir or tests).
func (a *App) SetLibraryStore(store *library.Store) {
	if store == nil {
		return
	}
	a.Store = store
	a.TagsDir = store.UserDir
	a.Syncer = dbsync.NewSyncer(store.DatabasesDir)
	a.Packs = nil
	a.PackIdx = 0
	a.refreshSavedFiles()
	a.refreshProbePacks()
}

// SetSourcesPath configures the JSON path for remote .nfc DB sources.
func (a *App) SetSourcesPath(path string) { a.SourcesPath = path }

// SetConfigPath configures the file path used to persist settings changes.
func (a *App) SetConfigPath(path string) { a.ConfigPath = path }

func defaultSimTag() *nfcfile.NfcTag {
	return &nfcfile.NfcTag{
		FileType:   "Flipper NFC device",
		Version:    4,
		DeviceType: "NTAG215",
		UID:        []byte{0x04, 0xA1, 0xB2, 0xC3, 0xD4, 0xE5, 0xF6},
		ATQA:       [2]byte{0x44, 0x00},
		SAK:        0x00,
		PagesTotal: 9,
		Pages: [][]byte{
			{0x04, 0xA1, 0xB2, 0x77},
			{0xC3, 0xD4, 0xE5, 0xF6},
			{0x11, 0x48, 0x00, 0x00},
			{0xE1, 0x10, 0x3E, 0x00},
			{0x03, 0x11, 0xD1, 0x01},
			{0x0D, 0x55, 0x04, 'c'},
			{'a', 'r', 'd', 'p'},
			{'u', 't', 'e', 'r'},
			{'.', 'c', 'c', 0xFE},
		},
	}
}

func (a *App) SetToast(msg string) {
	a.ToastText = msg
	a.ToastUntil = time.Now().Add(toastDuration)
}

// ApplySettings applies loaded or reset settings to the application state.
func (a *App) ApplySettings(s config.Settings) {
	s = s.Normalize()
	a.Settings = s
	a.Scanning = s.AutoScan
	if a.Probe != nil {
		a.Probe.SetIntervalMs(s.ProbeIntervalMs)
	}
	a.syncPollFlags()
}

// SwitchMode activates mode m, stopping probe/emulation when leaving those screens.
func (a *App) SwitchMode(m Mode) {
	prev := a.Mode
	if prev == ModeWrite && m != ModeWrite {
		a.cancelWriteJob()
	}
	if prev == ModeProbe && m != ModeProbe {
		a.stopActiveProbe()
	}
	if prev == ModeEmu && a.EmuActive && m != ModeEmu {
		a.stopEmulation()
	}
	a.Mode = m % modeCount
	a.ConfirmExit = false
	a.syncPollFlags()
}

func (a *App) goHome() {
	a.ShowSources = false
	a.SwitchMode(ModeHome)
}

func (a *App) HandleKey(act KeyAction) bool {
	if act == KeyHelp {
		a.ShowHelp = !a.ShowHelp
		a.HelpPage = 0
		return false
	}
	if a.ShowHelp {
		a.handleHelpKey(act)
		return false
	}
	if a.ShowSources {
		a.handleSourcesManageKey(act)
		return false
	}
	if act == KeyQuit {
		a.stopActiveProbe()
		if a.EmuActive {
			a.stopEmulation()
		}
		return true
	}
	if a.Mode == ModeHome {
		exit := a.handleHomeKey(act)
		a.syncPollFlags()
		return exit
	}
	switch act {
	case KeyBack:
		if a.Mode != ModeRead && a.Mode != ModeWrite {
			a.goHome()
			return false
		}
	case KeyTab:
		if a.Mode != ModeWrite {
			if a.Settings.ExpertMode {
				a.cycleMode()
			} else {
				a.goHome()
			}
			return false
		}
	case KeyNav8:
		if a.Mode != ModeRead && a.Mode != ModeSettings && a.Mode != ModeWrite {
			if a.Settings.ExpertMode {
				a.cycleMode()
			} else {
				a.goHome()
			}
			return false
		}
	}
	a.dispatchModeKey(act)
	a.syncPollFlags()
	return false
}

func (a *App) cycleMode() {
	next := (a.Mode + 1) % modeCount
	if next == ModeHome {
		next = ModeScan
	}
	a.SwitchMode(next)
	if a.Mode == ModeSaved {
		a.refreshSavedFiles()
	} else if a.Mode == ModeProbe {
		a.refreshProbePacks()
	}
}

func (a *App) dispatchModeKey(act KeyAction) {
	switch a.Mode {
	case ModeRead:
		a.handleReadKey(act)
	case ModeScan:
		a.handleScanKey(act)
	case ModeDump:
		a.handleDumpKey(act)
	case ModeEmu:
		a.handleEmuKey(act)
	case ModeSaved:
		a.handleSavedKey(act)
	case ModeProbe:
		a.handleProbeKey(act)
	case ModeSettings:
		a.handleSettingsKey(act)
	case ModeWrite:
		a.handleWriteKey(act)
	}
}

func (a *App) Step() {
	a.tick++
	a.pollSyncResults()
	a.pollWriteResults()
	if a.Mode == ModeProbe {
		a.stepProbe()
	}
	a.stepSimTelemetry()
	a.drainPollerEvents()
}

func (a *App) Render(c *Canvas) {
	c.Clear(ColBg)
	a.drawHeader(c)

	switch a.Mode {
	case ModeHome:
		a.drawHome(c)
		a.drawNavBar(c, a.homeNavLabels())
	case ModeRead:
		a.drawRead(c)
		a.drawNavBar(c, a.readNavLabels())
	case ModeScan:
		a.drawScan(c)
		a.drawNavBar(c, [5]string{scanLabel(a.Scanning), "EMU", "SAVE", "DUMP", "MODE"})
	case ModeDump:
		a.drawDump(c)
		s8 := "MENU"
		if a.Settings.ExpertMode {
			s8 = "MODE"
		}
		a.drawNavBar(c, [5]string{dumpTabLabel(a.ShowNdefTab), "EMU", "SAVE", "TOP", s8})
	case ModeEmu:
		a.drawEmu(c)
		if a.Settings.ExpertMode {
			a.drawNavBar(c, [5]string{emuLabel(a.EmuActive), "RND", "TYPE", "RST", "MODE"})
		} else {
			a.drawNavBar(c, [5]string{emuLabel(a.EmuActive), "CARDS", "", "RST", "MENU"})
		}
	case ModeSaved:
		a.drawSaved(c)
		if a.Settings.ExpertMode {
			a.drawNavBar(c, [5]string{"EMU", "WRITE", "DUMP", "DEL", "MODE"})
		} else {
			a.drawNavBar(c, [5]string{"EMU", "WRITE", "INFO", "DEL", "MENU"})
		}
	case ModeWrite:
		a.drawWrite(c)
		a.drawNavBar(c, a.writeNavLabels())
	case ModeProbe:
		a.drawProbe(c)
		a.drawNavBar(c, a.probeNavLabels())
	case ModeSettings:
		if a.ShowSources {
			a.drawSourcesManage(c)
			a.drawNavBar(c, [5]string{"ON/OF", "DIR", "ALL", "SYNC", "BACK"})
		} else {
			a.drawSettings(c)
			if a.Settings.ExpertMode {
				a.drawNavBar(c, [5]string{"TOGL", "SAVE", "RST", "HELP", "MENU"})
			} else {
				a.drawNavBar(c, [5]string{"TOGL", "SAVE", "SYNC", "", "MENU"})
			}
		}
	}

	if a.ShowHelp {
		a.drawHelpModal(c)
	}
	a.drawToast(c)
}

func (a *App) drawToast(c *Canvas) {
	if a.ToastText == "" || !time.Now().Before(a.ToastUntil) {
		return
	}
	tw := len(a.ToastText)*6 + 14
	tx := (ScreenW - tw) / 2
	c.FillRect(tx, 22, tw, 15, ColNavActive)
	c.DrawRect(tx, 22, tw, 15, ColEmerald)
	c.DrawText(tx+7, 26, a.ToastText, ColText, 1)
}
