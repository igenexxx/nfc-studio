package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"nfc-studio/internal/nfcfile"
	"nfc-studio/internal/st25r3916"
)

const (
	defaultDictMaxKeys = 64
	maxDictKeysCap     = 256
	dictScanTimeout    = 30 * time.Second
)

// NFCInspector is an optional capability interface for MIFARE dictionary and read-only EMV inspection.
type NFCInspector interface {
	ReadMifareClassicDict(ctx context.Context, keys [][6]byte, onProgress func(st25r3916.DictProgress)) (*st25r3916.MifareDump, error)
	InspectEMV(ctx context.Context) (*nfcfile.EMVInfo, error)
}

func (s *Server) toolDictScan(ctx context.Context, raw json.RawMessage) (toolResult, *rpcError) {
	var in struct {
		MaxKeys  int    `json:"max_keys"`
		DictPath string `json:"dict_path"`
		SaveName string `json:"save_name"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return toolResult{}, &rpcError{Code: errCodeInvalidParams, Message: "Invalid arguments: " + err.Error()}
	}
	maxKeys := in.MaxKeys
	if maxKeys == 0 {
		maxKeys = defaultDictMaxKeys
	}
	if maxKeys < 1 || maxKeys > maxDictKeysCap {
		return errResult("max_keys must be between 1 and 256"), nil
	}
	dictPath, err := resolveSafeDictPath(in.DictPath)
	if err != nil {
		return errResult(err.Error()), nil
	}
	keyList, err := nfcfile.LoadMifareKeyDict(dictPath)
	if err != nil {
		return errResult("load key dictionary failed: " + err.Error()), nil
	}
	if len(keyList) > maxKeys {
		keyList = keyList[:maxKeys]
	}
	rawKeys := make([][6]byte, len(keyList))
	for i, k := range keyList {
		rawKeys[i] = k
	}

	if s.backend == nil {
		return s.simDictScanResult(in.SaveName), nil
	}
	insp, ok := s.backend.(NFCInspector)
	if !ok {
		return errResult("active backend does not support MIFARE dictionary scan"), nil
	}
	scanCtx, cancel := context.WithTimeout(ctx, dictScanTimeout)
	defer cancel()

	s.hwMu.Lock()
	dump, err := insp.ReadMifareClassicDict(scanCtx, rawKeys, nil)
	s.hwMu.Unlock()
	if err != nil && dump == nil {
		return errResult("dict scan failed: " + err.Error()), nil
	}
	return s.formatDictScanOutput(dump, in.SaveName), nil
}

func resolveSafeDictPath(override string) (string, error) {
	defPath := nfcfile.DefaultKeyDictPath()
	if strings.TrimSpace(override) == "" {
		return defPath, nil
	}
	cleanOverride := filepath.Clean(override)
	if defPath != "" {
		allowedDir := filepath.Clean(filepath.Dir(defPath))
		if filepath.Dir(cleanOverride) != allowedDir {
			return "", errors.New("dict_path must reside inside ~/.config/nfc-studio")
		}
	}
	return cleanOverride, nil
}

func (s *Server) simDictScanResult(saveName string) toolResult {
	dump := &st25r3916.MifareDump{
		UID:      []byte{0xDE, 0xAD, 0xBE, 0xEF},
		ATQA:     [2]byte{0x04, 0x00},
		SAK:      0x08,
		CardSize: "1K",
		Sectors:  make([]st25r3916.SectorResult, 16),
	}
	for i := range 16 {
		sec := st25r3916.SectorResult{
			Sector:   i,
			Blocks:   make([][16]byte, 4),
			Readable: make([]bool, 4),
		}
		if i < 4 {
			sec.HasKeyA = true
			sec.KeyA = [6]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
			for b := range 4 {
				sec.Readable[b] = true
			}
		}
		dump.Sectors[i] = sec
	}
	res := s.formatDictScanOutput(dump, saveName)
	if res.IsError {
		return res
	}
	var m map[string]any
	_ = json.Unmarshal([]byte(res.Content[0].Text), &m)
	m["sim_mode"] = true
	return okJSON(m)
}

func (s *Server) formatDictScanOutput(dump *st25r3916.MifareDump, saveName string) toolResult {
	keysA, keysB, blocksRead, blocksTotal := 0, 0, 0, 0
	sectorDTOs := make([]map[string]any, 0, len(dump.Sectors))
	for _, sec := range dump.Sectors {
		if sec.HasKeyA {
			keysA++
		}
		if sec.HasKeyB {
			keysB++
		}
		blocksTotal += len(sec.Readable)
		for _, r := range sec.Readable {
			if r {
				blocksRead++
			}
		}
		entry := map[string]any{
			"sector":   sec.Sector,
			"readable": sec.Readable,
		}
		if sec.HasKeyA {
			entry["key_a"] = nfcfile.FormatHex(sec.KeyA[:])
		}
		if sec.HasKeyB {
			entry["key_b"] = nfcfile.FormatHex(sec.KeyB[:])
		}
		sectorDTOs = append(sectorDTOs, entry)
	}

	out := map[string]any{
		"uid_hex":       nfcfile.FormatHex(dump.UID),
		"card_size":     dump.CardSize,
		"sectors_total": len(dump.Sectors),
		"keys_found_a":  keysA,
		"keys_found_b":  keysB,
		"blocks_read":   blocksRead,
		"blocks_total":  blocksTotal,
		"sectors":       sectorDTOs,
	}
	if saveName != "" {
		savedPath, err := s.saveMifareDumpFile(saveName, dump)
		if err != nil {
			return errResult(err.Error())
		}
		out["saved_path"] = savedPath
	}
	return okJSON(out)
}

func (s *Server) saveMifareDumpFile(name string, dump *st25r3916.MifareDump) (string, error) {
	safe, err := sanitizeFilename(name)
	if err != nil {
		return "", err
	}
	if !strings.HasSuffix(strings.ToLower(safe), ".nfc") {
		safe += ".nfc"
	}
	if s.tagsDir == "" {
		return "", errors.New("tags directory is not configured")
	}
	if err := os.MkdirAll(s.tagsDir, 0o755); err != nil {
		return "", err
	}
	dest := filepath.Join(s.tagsDir, safe)
	if err := os.WriteFile(dest, []byte(dump.ToNfcTag().Format()), 0o644); err != nil {
		return "", err
	}
	return dest, nil
}

func (s *Server) toolInspectEMV(ctx context.Context) (toolResult, *rpcError) {
	if s.backend == nil {
		return okJSON(map[string]any{
			"sim_mode":  true,
			"read_only": true,
			"uid_hex":   "04 12 34 56 78 9A BC",
			"ats_hex":   "05 78 80 70 02",
			"apps": []nfcfile.EMVApp{
				{
					AID:       "A0000000031010",
					Label:     "VISA CREDIT",
					MaskedPAN: "400000******0002",
					Expiry:    "28/12",
				},
			},
		}), nil
	}
	insp, ok := s.backend.(NFCInspector)
	if !ok {
		return errResult("active backend does not support EMV inspection"), nil
	}
	s.hwMu.Lock()
	info, err := insp.InspectEMV(ctx)
	s.hwMu.Unlock()
	if err != nil {
		return errResult("EMV inspect failed: " + err.Error()), nil
	}
	return okJSON(map[string]any{
		"read_only": true,
		"uid_hex":   info.UIDHex,
		"ats_hex":   info.ATSHex,
		"apps":      info.Apps,
	}), nil
}
