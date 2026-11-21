package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"nfc-studio/internal/nfcfile"
	"nfc-studio/internal/st25r3916"
)

// MultiProtocolReader is an optional capability interface for NFC-A/B/F/V discovery.
type MultiProtocolReader interface {
	PollMultiProtocol(ctx context.Context, protos []st25r3916.Protocol) (*st25r3916.MultiTagInfo, error)
}

func (s *Server) toolScanMultiProtocol(ctx context.Context, raw json.RawMessage) (toolResult, *rpcError) {
	var in struct {
		Protocols  []string `json:"protocols"`
		Attempts   int      `json:"attempts"`
		MaxVBlocks int      `json:"max_v_blocks"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return toolResult{}, &rpcError{Code: errCodeInvalidParams, Message: "Invalid arguments: " + err.Error()}
	}
	protos, err := parseProtocolList(in.Protocols)
	if err != nil {
		return errResult(err.Error()), nil
	}
	attempts := in.Attempts
	if attempts == 0 {
		attempts = 2
	}
	if attempts < 1 || attempts > 10 {
		return errResult("attempts must be between 1 and 10"), nil
	}
	if s.backend == nil {
		return simMultiProtocolResult(protos[0]), nil
	}
	reader, ok := s.backend.(MultiProtocolReader)
	if !ok {
		return errResult("active backend does not support multi-protocol scanning"), nil
	}

	for range attempts {
		if ctx.Err() != nil {
			return errResult("scan cancelled: " + ctx.Err().Error()), nil
		}
		s.hwMu.Lock()
		info, err := reader.PollMultiProtocol(ctx, protos)
		s.hwMu.Unlock()
		if err == nil && info != nil {
			return formatMultiTagResult(info), nil
		}
		if err != nil && !errors.Is(err, st25r3916.ErrNoTag) {
			return errResult("multi-protocol poll failed: " + err.Error()), nil
		}
	}
	return okJSON(map[string]any{"found": false, "attempts": attempts}), nil
}

func parseProtocolList(raw []string) ([]st25r3916.Protocol, error) {
	if len(raw) == 0 {
		return []st25r3916.Protocol{st25r3916.ProtoNFCA, st25r3916.ProtoNFCB, st25r3916.ProtoNFCF, st25r3916.ProtoNFCV}, nil
	}
	out := make([]st25r3916.Protocol, 0, len(raw))
	for _, p := range raw {
		switch strings.ToUpper(strings.TrimSpace(p)) {
		case "A", "NFC-A":
			out = append(out, st25r3916.ProtoNFCA)
		case "B", "NFC-B":
			out = append(out, st25r3916.ProtoNFCB)
		case "F", "NFC-F", "FELICA":
			out = append(out, st25r3916.ProtoNFCF)
		case "V", "NFC-V", "ISO15693":
			out = append(out, st25r3916.ProtoNFCV)
		default:
			return nil, fmt.Errorf("invalid protocol %q: expected A, B, F, or V", p)
		}
	}
	return out, nil
}

func simMultiProtocolResult(proto st25r3916.Protocol) toolResult {
	switch proto {
	case st25r3916.ProtoNFCB:
		return okJSON(map[string]any{
			"found":             true,
			"sim_mode":          true,
			"protocol":          "NFC-B",
			"device_type":       "ISO14443-3B",
			"uid_hex":           "12 34 56 78",
			"pupi_hex":          "12 34 56 78",
			"app_data_hex":      "00 00 00 00",
			"protocol_info_hex": "00 81 71",
		})
	case st25r3916.ProtoNFCF:
		return okJSON(map[string]any{
			"found":           true,
			"sim_mode":        true,
			"protocol":        "NFC-F",
			"device_type":     "FeliCa",
			"uid_hex":         "01 2E 45 67 89 AB CD EF",
			"idm_hex":         "01 2E 45 67 89 AB CD EF",
			"pmm_hex":         "00 F1 00 00 00 01 43 00",
			"system_code_hex": "88 B4",
		})
	case st25r3916.ProtoNFCV:
		return okJSON(map[string]any{
			"found":       true,
			"sim_mode":    true,
			"protocol":    "NFC-V",
			"device_type": "ISO15693-3",
			"uid_hex":     "E0 04 01 50 12 34 56 78",
			"dsfid_hex":   "00",
			"blocks_hex":  []string{"01 02 03 04", "05 06 07 08"},
		})
	default:
		return simScanResult()
	}
}

func formatMultiTagResult(info *st25r3916.MultiTagInfo) toolResult {
	switch info.Protocol {
	case st25r3916.ProtoNFCA:
		if info.NFCA == nil {
			return okJSON(map[string]any{"found": false})
		}
		return okJSON(map[string]any{
			"found":       true,
			"protocol":    "NFC-A",
			"device_type": nfcfile.IdentifyCard(info.NFCA.ATQA, info.NFCA.SAK, len(info.NFCA.UID)),
			"uid_hex":     nfcfile.FormatHex(info.NFCA.UID),
			"atqa":        fmt.Sprintf("%02X %02X", info.NFCA.ATQA[0], info.NFCA.ATQA[1]),
			"sak":         fmt.Sprintf("%02X", info.NFCA.SAK),
			"pages_hex":   pagesToHex(info.NFCA.Pages),
			"ndef":        extractNdefDTO(info.NFCA.Pages),
		})
	case st25r3916.ProtoNFCB:
		b := info.NFCB
		return okJSON(map[string]any{
			"found":             true,
			"protocol":          "NFC-B",
			"device_type":       "ISO14443-3B",
			"uid_hex":           nfcfile.FormatHex(b.PUPI[:]),
			"pupi_hex":          nfcfile.FormatHex(b.PUPI[:]),
			"app_data_hex":      nfcfile.FormatHex(b.AppData[:]),
			"protocol_info_hex": nfcfile.FormatHex(b.ProtocolInfo[:]),
		})
	case st25r3916.ProtoNFCF:
		f := info.NFCF
		out := map[string]any{
			"found":       true,
			"protocol":    "NFC-F",
			"device_type": "FeliCa",
			"uid_hex":     nfcfile.FormatHex(f.IDm[:]),
			"idm_hex":     nfcfile.FormatHex(f.IDm[:]),
			"pmm_hex":     nfcfile.FormatHex(f.PMm[:]),
		}
		if f.HasSysCode {
			out["system_code_hex"] = nfcfile.FormatHex(f.SystemCode[:])
		}
		return okJSON(out)
	case st25r3916.ProtoNFCV:
		v := info.NFCV
		return okJSON(map[string]any{
			"found":       true,
			"protocol":    "NFC-V",
			"device_type": "ISO15693-3",
			"uid_hex":     nfcfile.FormatHex(v.UID[:]),
			"dsfid_hex":   fmt.Sprintf("%02X", v.DSFID),
			"blocks_hex":  pagesToHex(v.Blocks),
		})
	default:
		return okJSON(map[string]any{"found": false})
	}
}
