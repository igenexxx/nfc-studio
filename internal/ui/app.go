package ui

import (
	"bytes"
	"time"

	"nfc-studio/internal/config"
	"nfc-studio/internal/nfcfile"
	"nfc-studio/internal/st25r3916"
)

const (
	modeCount        = 5
	helpPageCount    = 4
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
	EmuActive  bool
	ReaderHits int
}

// SavedState holds the Flipper .nfc library browsing state.
type SavedState struct {
	SavedFiles []SavedTag
	SavedIdx   int
}

// SettingsState holds persisted opt-in preferences for the SET tab.
type SettingsState struct {
	ConfigPath  string
	Settings    config.Settings
	SettingsIdx int
}

// ChromeState holds shared modal, toast, and animation tick state.
type ChromeState struct {
	ShowHelp   bool
	HelpPage   int
	ToastText  string
	ToastUntil time.Time
	tick       uint64
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
	SettingsState
	ChromeState
	PollerState
}

func NewApp(chip NFCReader, tagsDir string) *App {
	defaults := config.Default()
	a := &App{
		Chip:          chip,
		SimMode:       chip == nil,
		TagsDir:       tagsDir,
		Mode:          ModeScan,
		ScanState:     ScanState{Scanning: defaults.AutoScan},
		SettingsState: SettingsState{Settings: defaults},
		PollerState:   newPollerState(),
	}
	if a.SimMode {
		a.CurrentTag = defaultSimTag()
		a.updateNDEF()
	}
	a.syncPollFlags()
	a.refreshSavedFiles()
	return a
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
	a.Settings = s
	a.Scanning = s.AutoScan
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
	switch act {
	case KeyQuit:
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
	a.Mode = (a.Mode + 1) % modeCount
	if prevMode == ModeEmu && a.EmuActive && a.Mode != ModeEmu {
		a.stopEmulation()
	}
	if a.Mode == ModeSaved {
		a.refreshSavedFiles()
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
	case ModeSettings:
		a.handleSettingsKey(act)
	}
}

func (a *App) Step() {
	a.tick++
	if a.SimMode && a.Mode == ModeEmu && a.EmuActive && a.tick%simHitTickModulo == 0 {
		a.ReaderHits++
	}
	// Drain asynchronously polled tags without blocking the UI thread.
	for {
		select {
		case tag := <-a.scanCh:
			isNew := a.CurrentTag == nil || !bytes.Equal(a.CurrentTag.UID, tag.UID)
			a.CurrentTag = tag
			a.LastSeen = time.Now()
			a.updateNDEF()
			if isNew {
				a.SetToast("Found: " + tag.DeviceType)
			}
		case <-a.hitCh:
			a.ReaderHits++
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
	case ModeSettings:
		a.drawSettings(c)
		a.drawNavBar(c, [5]string{"TOGGLE", "SAVE", "RESET", "HELP", "MODE"})
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
