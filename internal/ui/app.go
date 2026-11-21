package ui

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"nfc-studio/internal/config"
	"nfc-studio/internal/dbsync"
	"nfc-studio/internal/library"
	"nfc-studio/internal/nfcfile"
	"nfc-studio/internal/nfcml"
	"nfc-studio/internal/st25r3916"
)

const (
	modeCount        = 6
	helpPageCount    = 5
	toastDuration    = 2 * time.Second
	simHitTickModulo = 24
)

// NFCReader abstracts ST25R3916 hardware operations so the UI and tests stay decoupled.
type NFCReader interface {
	PollISO14443A() (*st25r3916.TagInfo, error)
	PollTargetHit() (bool, error)
	StartEmulation(uid []byte, atqa [2]byte, sak uint8) error
	FieldOff() error
}

type Mode int

const (
	ModeScan Mode = iota
	ModeDump
	ModeEmu
	ModeSaved
	ModeProbe
	ModeSettings
)

func (m Mode) String() string {
	switch m {
	case ModeScan:
		return "SCAN"
	case ModeDump:
		return "DUMP"
	case ModeEmu:
		return "EMU"
	case ModeSaved:
		return "SAVED"
	case ModeProbe:
		return "PROBE"
	case ModeSettings:
		return "SET"
	default:
		return "NFC"
	}
}

type SavedTag struct {
	Name string
	Path string
	Tag  *nfcfile.NfcTag
}

// ScanState holds active ISO 14443-A tag discovery and memory inspection state.
type ScanState struct {
	Scanning    bool
	CurrentTag  *nfcfile.NfcTag
	CurrentNDEF *nfcfile.NdefRecord
	LastSeen    time.Time
	DumpScroll  int
	ShowNdefTab bool
}

// EmuState holds ST25R3916 Passive Target A card emulation state.
type EmuState struct {
	EmuActive        bool
	ReaderHits       int
	ExternalFieldAmp uint8
	ExternalFieldPct int
	HitFlashUntil    time.Time
}

// SavedState holds the User Library (.nfc tags) browsing and multi-select state.
type SavedState struct {
	SavedFiles    []SavedTag
	SavedIdx      int
	SavedSelected map[string]bool
	savedDelUntil time.Time
}

// ProbeState holds separated library storage, Auto-Emu session, and pack multi-select state.
type ProbeState struct {
	Store         *library.Store
	Probe         *library.ProbeSession
	Packs         []library.PackInfo
	PackIdx       int
	ProbeSelected map[string]bool
	probeDelUntil time.Time
}

// ManagedSourceRow represents a remote .nfc source and/or local pack folder in Manage DB Sources.
type ManagedSourceRow struct {
	ID         string
	HasSource  bool
	Enabled    bool
	HasDir     bool
	FileCount  int
	SourceType dbsync.SourceType
}

// SourcesManageState holds the Manage DB Sources sub-view state under SET.
type SourcesManageState struct {
	ShowSources    bool
	SourceRows     []ManagedSourceRow
	SourceIdx      int
	SourceSelected map[string]bool
	sourceDelMode  int
	sourceDelUntil time.Time
}

// SettingsState holds persisted opt-in preferences and remote DB sync state for the SET tab.
type SettingsState struct {
	ConfigPath  string
	SourcesPath string
	Settings    config.Settings
	SettingsIdx int
	Syncer      *dbsync.Syncer
	SyncBusy    bool
	syncCancel  context.CancelFunc
	syncDoneCh  chan []dbsync.SyncReport
}

// ChromeState holds shared modal, toast, and animation tick state.
type ChromeState struct {
	ShowHelp   bool
	HelpPage   int
	ToastText  string
	ToastUntil time.Time
	tick       uint64
}

// MLState holds on-device NFC ML dump profiling, tag fingerprinting, reader IAT, and RF field calibration.
type MLState struct {
	LastProfile  nfcml.DumpProfile
	FPIndex      *nfcml.FingerprintIndex
	LastMatch    nfcml.MatchResult
	HasMatch     bool
	ReaderIAT    *nfcml.ReaderIATTracker
	LastActivity nfcml.ActivityReport
	FieldBase    *nfcml.FieldBaseline
	LastField    nfcml.FieldReading
}

// App coordinates the NFC Studio 320x170 framebuffer interface across all modes.
type App struct {
	Chip    NFCReader
	SimMode bool
	TagsDir string
	Mode    Mode

	ScanState
	EmuState
	SavedState
	ProbeState
	SourcesManageState
	SettingsState
	MLState
	ChromeState
	PollerState
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
		Mode:      ModeScan,
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

// Close cancels any active background DB sync and stops emulation cleanly.
func (a *App) Close() {
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
func (a *App) SetSourcesPath(path string) {
	a.SourcesPath = path
}

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

// SetConfigPath configures the file path used to persist settings changes.
func (a *App) SetConfigPath(path string) {
	a.ConfigPath = path
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
	switch act {
	case KeyQuit:
		a.stopActiveProbe()
		return true
	case KeyTab, KeyNav8:
		a.cycleMode()
	}
	a.dispatchModeKey(act)
	a.syncPollFlags()
	return false
}

func (a *App) handleHelpKey(act KeyAction) {
	switch act {
	case KeyLeft, KeyUp:
		a.HelpPage = (a.HelpPage + helpPageCount - 1) % helpPageCount
	case KeyRight, KeyDown, KeySpace, KeyTab, KeyNav5:
		a.HelpPage = (a.HelpPage + 1) % helpPageCount
	case KeyQuit, KeyEnter, KeyNav4, KeyNav8:
		a.ShowHelp = false
	}
}

func (a *App) cycleMode() {
	prevMode := a.Mode
	if prevMode == ModeProbe {
		a.stopActiveProbe()
	}
	a.Mode = (a.Mode + 1) % modeCount
	if prevMode == ModeEmu && a.EmuActive && a.Mode != ModeEmu {
		a.stopEmulation()
	}
	if a.Mode == ModeSaved {
		a.refreshSavedFiles()
	} else if a.Mode == ModeProbe {
		a.refreshProbePacks()
	}
	a.syncPollFlags()
}

func (a *App) dispatchModeKey(act KeyAction) {
	switch a.Mode {
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
	}
}

func (a *App) Step() {
	a.tick++
	a.pollSyncResults()
	if a.Mode == ModeProbe {
		a.stepProbe()
	}
	if a.SimMode && (a.Mode == ModeEmu || a.Mode == ModeProbe) && a.EmuActive {
		simAmp := uint8(135 + (a.tick%8)*6)
		a.ExternalFieldAmp = simAmp
		a.ExternalFieldPct = st25r3916.ExternalFieldPercent(simAmp)
		if a.tick%simHitTickModulo == 0 {
			a.ReaderHits++
			a.HitFlashUntil = time.Now().Add(350 * time.Millisecond)
			if a.ReaderIAT != nil {
				a.LastActivity = a.ReaderIAT.ObserveHit(time.Now())
			}
		}
	} else if !a.EmuActive {
		a.ExternalFieldAmp = 0
		a.ExternalFieldPct = 0
	}
	if a.SimMode && a.Mode == ModeScan && a.FieldBase != nil {
		amp := uint8(180)
		phase := uint8(120)
		if a.CurrentTag != nil && a.tick > 8 {
			amp, phase = 166, 122
		}
		a.LastField = a.FieldBase.Update(amp, phase)
	}
	// Drain asynchronously polled tags and reader hits without blocking the UI thread.
	for {
		select {
		case tag := <-a.scanCh:
			isNew := a.CurrentTag == nil || !bytes.Equal(a.CurrentTag.UID, tag.UID)
			a.CurrentTag = tag
			a.LastSeen = time.Now()
			a.updateNDEF()
			if isNew {
				if a.HasMatch {
					a.SetToast(fmt.Sprintf("Match: %s (%.0f%%)", truncate(a.LastMatch.Name, 12), a.LastMatch.Similarity*100))
				} else {
					a.SetToast("Found: " + tag.DeviceType)
				}
			}
		case fld := <-a.fieldCh:
			if a.FieldBase != nil {
				a.LastField = a.FieldBase.Update(fld[0], fld[1])
			}
		case extAmp := <-a.extFieldCh:
			a.ExternalFieldAmp = extAmp
			a.ExternalFieldPct = st25r3916.ExternalFieldPercent(extAmp)
		case <-a.hitCh:
			a.ReaderHits++
			a.HitFlashUntil = time.Now().Add(350 * time.Millisecond)
			if a.ReaderIAT != nil {
				a.LastActivity = a.ReaderIAT.ObserveHit(time.Now())
				if a.Mode == ModeProbe && a.Probe != nil && a.Probe.Status == library.ProbeRunning && a.LastActivity.Activity == nfcml.ActivityEngaged {
					a.stopActiveProbe()
					a.SetToast("Reader ENGAGED! Paused")
				}
			}
		default:
			return
		}
	}
}

func (a *App) Render(c *Canvas) {
	c.Clear(ColBg)
	a.drawHeader(c)

	switch a.Mode {
	case ModeScan:
		a.drawScan(c)
		a.drawNavBar(c, [5]string{scanLabel(a.Scanning), "EMU", "SAVE", "DUMP", "MODE"})
	case ModeDump:
		a.drawDump(c)
		a.drawNavBar(c, [5]string{dumpTabLabel(a.ShowNdefTab), "EMU", "SAVE", "TOP", "MODE"})
	case ModeEmu:
		a.drawEmu(c)
		a.drawNavBar(c, [5]string{emuLabel(a.EmuActive), "RND", "4B/7B", "RST", "MODE"})
	case ModeSaved:
		a.drawSaved(c)
		a.drawNavBar(c, [5]string{"EMU", "DUMP", "DEL", "SYNC", "MODE"})
	case ModeProbe:
		a.drawProbe(c)
		a.drawNavBar(c, a.probeNavLabels())
	case ModeSettings:
		if a.ShowSources {
			a.drawSourcesManage(c)
			a.drawNavBar(c, [5]string{"ON/OFF", "DIR", "ALL", "SYNC", "BACK"})
		} else {
			a.drawSettings(c)
			a.drawNavBar(c, [5]string{"TOGGLE", "SAVE", "RESET", "HELP", "MODE"})
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
