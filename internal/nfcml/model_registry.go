package nfcml

import (
	_ "embed"
	"os"
	"path/filepath"
	"sync"

	"nfc-studio/internal/nfcfile"
)

//go:embed models/nfc_inductive_v1.czml
var embeddedInductiveNet []byte

//go:embed models/nfc_dump_v1.czml
var embeddedDumpNet []byte

var (
	indOnce sync.Once
	indDef  *CZMLModel
	indInfo ModelInfo

	dumpOnce sync.Once
	dumpDef  *CZMLModel
	dumpInfo ModelInfo

	nfcWS = sync.Pool{New: func() any { return new(NFCWorkspace) }}
)

// DefaultModelsDir returns $HOME/.config/nfc-studio/models.
func DefaultModelsDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(".", ".config", "nfc-studio", "models")
	}
	return filepath.Join(home, ".config", "nfc-studio", "models")
}

// LoadDefaultInductiveNet loads an optional override or falls back to the embedded model.
func LoadDefaultInductiveNet(overridePath string) (*CZMLModel, ModelInfo) {
	if overridePath != "" {
		if m, err := LoadCZMLFile(overridePath, DomainNFCInductive); err == nil && len(m.Layers) == 8 {
			return m, m.Info("override")
		}
	}
	m, err := LoadCZMLBytes(embeddedInductiveNet, DomainNFCInductive)
	if err != nil || len(m.Layers) != 8 {
		return nil, ModelInfo{Source: "unavailable"}
	}
	return m, m.Info("embedded")
}

// LoadDefaultDumpNet loads an optional override or falls back to the embedded model.
func LoadDefaultDumpNet(overridePath string) (*CZMLModel, ModelInfo) {
	if overridePath != "" {
		if m, err := LoadCZMLFile(overridePath, DomainNFCDump); err == nil && len(m.Layers) == 5 {
			return m, m.Info("override")
		}
	}
	m, err := LoadCZMLBytes(embeddedDumpNet, DomainNFCDump)
	if err != nil || len(m.Layers) != 5 {
		return nil, ModelInfo{Source: "unavailable"}
	}
	return m, m.Info("embedded")
}

func defaultInductiveNet() *CZMLModel {
	indOnce.Do(func() {
		indDef, indInfo = LoadDefaultInductiveNet(filepath.Join(DefaultModelsDir(), "nfc_inductive.czml"))
	})
	return indDef
}

func defaultDumpNet() *CZMLModel {
	dumpOnce.Do(func() {
		dumpDef, dumpInfo = LoadDefaultDumpNet(filepath.Join(DefaultModelsDir(), "nfc_dump.czml"))
	})
	return dumpDef
}

// ActiveModelsInfo returns provenance for both embedded/override NFC neural models.
func ActiveModelsInfo() (ModelInfo, ModelInfo) {
	_ = defaultInductiveNet()
	_ = defaultDumpNet()
	return indInfo, dumpInfo
}

// ClassifyInductiveTrajectory evaluates 32 (dAmp, dPhase) ST25R3916 readings with NFC-InductiveNet.
func ClassifyInductiveTrajectory(dAmp, dPhase *[InductiveSeqLen]int16) (InductiveNeuralResult, bool) {
	m := defaultInductiveNet()
	if m == nil || dAmp == nil || dPhase == nil {
		return InductiveNeuralResult{}, false
	}
	ws := nfcWS.Get().(*NFCWorkspace)
	defer nfcWS.Put(ws)
	return m.InferInductive(ws, dAmp, dPhase), true
}

// ClassifyTagDump evaluates a parsed NfcTag with NFC-DumpNet for ecosystem & 32D embedding.
func ClassifyTagDump(tag *nfcfile.NfcTag) (DumpNeuralResult, bool) {
	m := defaultDumpNet()
	if m == nil || tag == nil {
		return DumpNeuralResult{}, false
	}
	ws := nfcWS.Get().(*NFCWorkspace)
	defer nfcWS.Put(ws)
	return m.InferDump(ws, tag), true
}
