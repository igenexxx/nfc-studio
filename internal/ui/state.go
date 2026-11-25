package ui

import (
	"context"
	"time"

	"nfc-studio/internal/config"
	"nfc-studio/internal/dbsync"
	"nfc-studio/internal/library"
	"nfc-studio/internal/nfcfile"
	"nfc-studio/internal/nfcml"
	"nfc-studio/internal/st25r3916"
)

const (
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

// Mode identifies one of the NFC Studio screens.
type Mode int

const (
	ModeHome Mode = iota
	ModeRead
	ModeSaved
	ModeEmu
	ModeWrite
	ModeScan
	ModeDump
	ModeProbe
	ModeSettings
	modeCount
)

func (m Mode) String() string {
	switch m {
	case ModeHome:
		return "HOME"
	case ModeRead:
		return "READ"
	case ModeSaved:
		return "SAVED"
	case ModeEmu:
		return "EMU"
	case ModeScan:
		return "SCAN"
	case ModeDump:
		return "DUMP"
	case ModeProbe:
		return "PROBE"
	case ModeSettings:
		return "SET"
	case ModeWrite:
		return "WRITE"
	default:
		return "NFC"
	}
}

// Title returns the human-readable breadcrumb header label for m.
func (m Mode) Title() string {
	switch m {
	case ModeHome:
		return "NFC STUDIO"
	case ModeRead:
		return "READ & CLONE"
	case ModeSaved:
		return "MY CARDS"
	case ModeEmu:
		return "EMULATE CARD"
	case ModeScan:
		return "NFC RADAR LAB"
	case ModeDump:
		return "CARD MEMORY"
	case ModeProbe:
		return "AUTO-EMU PROBE"
	case ModeSettings:
		return "SETTINGS"
	case ModeWrite:
		return "WRITE TO CARD"
	default:
		return "NFC STUDIO"
	}
}

type SavedTag struct {
	Name string
	Path string
	Tag  *nfcfile.NfcTag
}

// HomeState holds cursor, scroll, and exit confirmation for the Home Menu.
type HomeState struct {
	HomeCursor  int
	HomeScroll  int
	ConfirmExit bool
}

// ReadWizardState holds the 2-step Read & Clone wizard state.
type ReadWizardState struct {
	ReadCaptured bool
}

// ScanState holds active ISO 14443-A tag discovery and memory inspection state.
type ScanState struct {
	Scanning    bool
	CurrentTag  *nfcfile.NfcTag
	CurrentNDEF *nfcfile.NdefRecord
	LastSeen    time.Time
	DumpScroll  int
	DumpView    int // For 16B MIFARE blocks: 0=bytes 0..7, 1=bytes 8..15, 2=full 16B ASCII+role
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

// WriteSubMode selects between cloning a loaded .nfc dump and writing a quick NDEF preset.
type WriteSubMode int

const (
	WriteSubDump WriteSubMode = iota
	WriteSubNDEF
)

// WriteProgress carries incremental progress updates from the background card-write worker.
type WriteProgress struct {
	Done  int
	Total int
	Stage string
}

// WriteDone carries the terminal outcome of an asynchronous physical card-write operation.
type WriteDone struct {
	OK            bool
	Summary       string
	ErrText       string
	DetectedBlank string
	CompatLevel   nfcfile.CompatLevel
	CompatNote    string
}

// WriteProbeDone carries the result of an on-demand Space blank-card probe preview.
type WriteProbeDone struct {
	BlankLabel  string
	CompatLevel nfcfile.CompatLevel
	CompatNote  string
	ErrText     string
}

// WriteState holds all UI and async worker state for ModeWrite (Write / Clone to Card).
type WriteState struct {
	WriteSub           WriteSubMode
	WriteReturnMode    Mode
	WriteMifareMode    int // 0=AUTO, 1=Gen1A Full, 2=Gen1A UID, 3=Gen2 Full, 4=Gen2 UID, 5=MF Sectors
	WriteTrailers      bool
	WriteNDEFKind      int // 0=URL, 1=Text, 2=WiFi
	WriteNDEFPreset    int
	WriteArmedUntil    time.Time
	WriteBusy          bool
	WriteDoneCount     int
	WriteTotalCount    int
	WriteStage         string
	WriteResultSet     bool
	WriteResultOK      bool
	WriteResultMsg     string
	WriteDetectedBlank string
	WriteCompatLevel   nfcfile.CompatLevel
	WriteCompatNote    string
	writeCancel        context.CancelFunc
	writeProgCh        chan WriteProgress
	writeDoneCh        chan WriteDone
	writeProbeCh       chan WriteProbeDone
}
