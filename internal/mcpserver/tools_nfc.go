package mcpserver

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"nfc-studio/internal/nfcfile"
	"nfc-studio/internal/st25r3916"
)

const (
	defaultScanAttempts  = 3
	minScanAttempts      = 1
	maxScanAttempts      = 10
	defaultEmuDurationMS = 3000
	minEmuDurationMS     = 500
	maxEmuDurationMS     = 30000
	simEmuCap            = 50 * time.Millisecond
)

func (s *Server) toolGetStatus() toolResult {
	out := map[string]any{
		"sim_mode": s.backend == nil,
		"tags_dir": s.tagsDir,
		"settings": s.settings,
	}
	if s.backend != nil {
		s.hwMu.Lock()
		info := s.backend.Info()
		s.hwMu.Unlock()
		out["identity"] = fmt.Sprintf("0x%02X", info.Identity)
		out["revision"] = info.Revision
		out["supply_mv"] = info.SupplyMV
	}
	return okJSON(out)
}

func (s *Server) toolScanTag(ctx context.Context, raw json.RawMessage) (toolResult, *rpcError) {
	var in struct {
		Attempts int `json:"attempts"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return toolResult{}, &rpcError{Code: errCodeInvalidParams, Message: "Invalid arguments: " + err.Error()}
	}
	attempts := in.Attempts
	if attempts == 0 {
		attempts = defaultScanAttempts
	}
	if attempts < minScanAttempts || attempts > maxScanAttempts {
		return errResult("attempts must be between 1 and 10"), nil
	}
	if s.backend == nil {
		return simScanResult(), nil
	}

	for range attempts {
		if ctx.Err() != nil {
			return errResult("scan cancelled: " + ctx.Err().Error()), nil
		}
		s.hwMu.Lock()
		tag, err := s.backend.PollISO14443A()
		s.hwMu.Unlock()
		if err == nil && tag != nil {
			return formatScannedTag(tag), nil
		}
		if err != nil && !errors.Is(err, st25r3916.ErrNoTag) {
			return errResult("hardware poll failed: " + err.Error()), nil
		}
	}
	return okJSON(map[string]any{"found": false, "attempts": attempts}), nil
}

func simScanResult() toolResult {
	simPages := [][]byte{
		{0x04, 0xA1, 0xB2, 0x77},
		{0xC3, 0xD4, 0xE5, 0xF6},
		{0x11, 0x48, 0x00, 0x00},
		{0xE1, 0x10, 0x3E, 0x00},
		{0x03, 0x11, 0xD1, 0x01},
		{0x0D, 0x55, 0x04, 'c'},
		{'a', 'r', 'd', 'p'},
		{'u', 't', 'e', 'r'},
		{'.', 'c', 'c', 0xFE},
	}
	return okJSON(map[string]any{
		"found":       true,
		"sim_mode":    true,
		"uid_hex":     "04 A1 B2 C3 D4 E5 F6",
		"atqa":        "44 00",
		"sak":         "00",
		"device_type": "NTAG215",
		"pages_hex":   pagesToHex(simPages),
		"ndef":        extractNdefDTO(simPages),
	})
}

func formatScannedTag(tag *st25r3916.TagInfo) toolResult {
	return okJSON(map[string]any{
		"found":       true,
		"uid_hex":     nfcfile.FormatHex(tag.UID),
		"atqa":        fmt.Sprintf("%02X %02X", tag.ATQA[0], tag.ATQA[1]),
		"sak":         fmt.Sprintf("%02X", tag.SAK),
		"device_type": nfcfile.IdentifyCard(tag.ATQA, tag.SAK, len(tag.UID)),
		"pages_hex":   pagesToHex(tag.Pages),
		"ndef":        extractNdefDTO(tag.Pages),
	})
}

func extractNdefDTO(pages [][]byte) *ndefDTO {
	if rec, ok := nfcfile.ExtractNDEF(pages); ok {
		return &ndefDTO{Type: rec.Type, Payload: rec.Payload}
	}
	return nil
}

func (s *Server) toolParseNDEF(raw json.RawMessage) (toolResult, *rpcError) {
	var in struct {
		PagesHex []string `json:"pages_hex"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return toolResult{}, &rpcError{Code: errCodeInvalidParams, Message: "Invalid arguments: " + err.Error()}
	}
	if len(in.PagesHex) == 0 {
		return errResult("pages_hex is required and must be non-empty"), nil
	}
	pages, err := parsePagesHex(in.PagesHex)
	if err != nil {
		return errResult(err.Error()), nil
	}
	ndef := extractNdefDTO(pages)
	return okJSON(map[string]any{
		"found":       ndef != nil,
		"ndef":        ndef,
		"pages_count": len(pages),
	}), nil
}

func (s *Server) toolEmulateTag(ctx context.Context, raw json.RawMessage) (toolResult, *rpcError) {
	var in struct {
		UIDHex     string `json:"uid_hex"`
		ATQAHex    string `json:"atqa_hex"`
		SAKHex     string `json:"sak_hex"`
		DurationMS int    `json:"duration_ms"`
		ConfirmRF  bool   `json:"confirm_rf"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return toolResult{}, &rpcError{Code: errCodeInvalidParams, Message: "Invalid arguments: " + err.Error()}
	}
	if !in.ConfirmRF {
		return errResult("confirm_rf must be true to start RF card emulation"), nil
	}
	uid, atqa, sak, err := parseTagIdent(in.UIDHex, in.ATQAHex, in.SAKHex, true)
	if err != nil {
		return errResult(err.Error()), nil
	}
	durMS := in.DurationMS
	if durMS == 0 {
		durMS = defaultEmuDurationMS
	}
	if durMS < minEmuDurationMS || durMS > maxEmuDurationMS {
		return errResult("duration_ms must be between 500 and 30000"), nil
	}
	if s.backend == nil {
		return emulateSim(ctx, uid, atqa, sak, durMS), nil
	}
	return s.emulateHardware(ctx, uid, atqa, sak, durMS), nil
}

func parseTagIdent(uidHex, atqaHex, sakHex string, strictUIDLen bool) ([]byte, [2]byte, uint8, error) {
	uid, err := parseHexFlex(uidHex)
	if strictUIDLen && (err != nil || (len(uid) != 4 && len(uid) != 7)) {
		return nil, [2]byte{}, 0, errors.New("uid_hex must be 4 or 7 hex bytes")
	}
	if !strictUIDLen && (err != nil || len(uid) == 0) {
		return nil, [2]byte{}, 0, errors.New("uid_hex is required and must be valid hex bytes")
	}
	atqa, sak := defaultATQASAK(len(uid))
	if strings.TrimSpace(atqaHex) != "" {
		b, err := parseHexFlex(atqaHex)
		if err != nil || len(b) != 2 {
			return nil, [2]byte{}, 0, errors.New("atqa_hex must be 2 hex bytes")
		}
		atqa = [2]byte{b[0], b[1]}
	}
	if strings.TrimSpace(sakHex) != "" {
		b, err := parseHexFlex(sakHex)
		if err != nil || len(b) != 1 {
			return nil, [2]byte{}, 0, errors.New("sak_hex must be 1 hex byte")
		}
		sak = b[0]
	}
	return uid, atqa, sak, nil
}

func defaultATQASAK(uidLen int) ([2]byte, uint8) {
	if uidLen == 4 {
		return [2]byte{0x04, 0x00}, 0x08
	}
	return [2]byte{0x44, 0x00}, 0x00
}

func emulateSim(ctx context.Context, uid []byte, atqa [2]byte, sak uint8, durMS int) toolResult {
	duration := time.Duration(durMS) * time.Millisecond
	timer := time.NewTimer(min(duration, simEmuCap))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return errResult("emulation cancelled: " + ctx.Err().Error())
	case <-timer.C:
	}
	return okJSON(map[string]any{
		"sim_mode":    true,
		"uid_hex":     nfcfile.FormatHex(uid),
		"atqa":        fmt.Sprintf("%02X %02X", atqa[0], atqa[1]),
		"sak":         fmt.Sprintf("%02X", sak),
		"duration_ms": durMS,
		"reader_hits": 0,
	})
}

func (s *Server) emulateHardware(ctx context.Context, uid []byte, atqa [2]byte, sak uint8, durMS int) toolResult {
	s.hwMu.Lock()
	defer s.hwMu.Unlock()

	if err := s.backend.StartEmulation(uid, atqa, sak); err != nil {
		return errResult("start emulation failed: " + err.Error())
	}
	defer func() { _ = s.backend.FieldOff() }()

	pollStep := s.emuPollInterval
	if pollStep <= 0 {
		pollStep = defaultEmuPollInterval
	}
	deadline := time.NewTimer(time.Duration(durMS) * time.Millisecond)
	defer deadline.Stop()
	ticker := time.NewTicker(pollStep)
	defer ticker.Stop()

	hits := 0
	for {
		select {
		case <-ctx.Done():
			return formatEmuResult(uid, atqa, sak, durMS, hits, true)
		case <-deadline.C:
			return formatEmuResult(uid, atqa, sak, durMS, hits, false)
		case <-ticker.C:
			if hit, err := s.backend.PollTargetHit(); err == nil && hit {
				hits++
			}
		}
	}
}

func formatEmuResult(uid []byte, atqa [2]byte, sak uint8, durMS, hits int, cancelled bool) toolResult {
	out := map[string]any{
		"uid_hex":     nfcfile.FormatHex(uid),
		"atqa":        fmt.Sprintf("%02X %02X", atqa[0], atqa[1]),
		"sak":         fmt.Sprintf("%02X", sak),
		"duration_ms": durMS,
		"reader_hits": hits,
	}
	if cancelled {
		out["cancelled"] = true
	}
	return okJSON(out)
}

func parseHexFlex(s string) ([]byte, error) {
	cleaned := strings.NewReplacer(" ", "", ":", "", "-", "", "\t", "").Replace(strings.TrimSpace(s))
	if cleaned == "" {
		return nil, errors.New("empty hex string")
	}
	return hex.DecodeString(cleaned)
}

func parsePagesHex(pagesHex []string) ([][]byte, error) {
	pages := make([][]byte, 0, len(pagesHex))
	for i, p := range pagesHex {
		b, err := parseHexFlex(p)
		if err != nil {
			return nil, fmt.Errorf("invalid hex at page %d: %w", i, err)
		}
		pages = append(pages, b)
	}
	return pages, nil
}

func pagesToHex(pages [][]byte) []string {
	out := make([]string, len(pages))
	for i, p := range pages {
		out[i] = nfcfile.FormatHex(p)
	}
	return out
}
