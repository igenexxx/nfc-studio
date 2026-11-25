package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"nfc-studio/internal/nfcfile"
	"nfc-studio/internal/st25r3916"
)

// NFCBlankProber is an optional capability interface for non-destructive blank-card identification.
type NFCBlankProber interface {
	ProbeBlankTarget(ctx context.Context) (*st25r3916.BlankTarget, error)
}

func (s *Server) toolProbeBlank(ctx context.Context, raw json.RawMessage) (toolResult, *rpcError) {
	var in struct {
		SourceName      string `json:"source_name"`
		IncludeTrailers bool   `json:"include_trailers"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return toolResult{}, &rpcError{Code: errCodeInvalidParams, Message: "Invalid arguments: " + err.Error()}
	}
	var blank *st25r3916.BlankTarget
	simMode := s.backend == nil
	if simMode {
		blank = &st25r3916.BlankTarget{
			Kind: st25r3916.BlankNTAG215, Label: "NTAG215 (126 pg)",
			Protocol: st25r3916.ProtoNFCA, UID: []byte{0x04, 0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF},
			UIDHex: "04 AA BB CC DD EE FF", SAK: 0x00, MaxUserPages: 126,
		}
	} else {
		prober, ok := s.backend.(NFCBlankProber)
		if !ok {
			return errResult("active backend does not support blank target probing"), nil
		}
		s.hwMu.Lock()
		var err error
		blank, err = prober.ProbeBlankTarget(ctx)
		s.hwMu.Unlock()
		if err != nil {
			return errResult("probe blank failed: " + err.Error()), nil
		}
	}
	out := map[string]any{
		"sim_mode": simMode,
		"blank":    blank,
	}
	if strings.TrimSpace(in.SourceName) != "" {
		safeName, err := sanitizeFilename(in.SourceName)
		if err != nil {
			return errResult(err.Error()), nil
		}
		_, content, ok := s.findSavedTagFile(safeName)
		if !ok {
			return errResult(fmt.Sprintf("saved tag %q not found", safeName)), nil
		}
		tag, err := nfcfile.Parse(string(content))
		if err != nil {
			return errResult("parse .nfc failed: " + err.Error()), nil
		}
		compat, err := nfcfile.ReconcileWritePlan(tag, blankToDescriptor(blank), nfcfile.PlanOptions{IncludeTrailers: in.IncludeTrailers})
		if err != nil {
			return errResult(err.Error()), nil
		}
		out["source_name"] = safeName
		out["compat_level"] = compat.Level
		out["compat_note"] = compat.Reason
		out["strategy"] = compat.Plan.Strategy
	}
	return okJSON(out), nil
}

func blankToDescriptor(b *st25r3916.BlankTarget) nfcfile.TargetDescriptor {
	if b == nil {
		return nfcfile.TargetDescriptor{Family: nfcfile.TargetUnsupported, Label: "Unknown"}
	}
	fam := nfcfile.TargetUnsupported
	switch b.Kind {
	case st25r3916.BlankNTAG213, st25r3916.BlankNTAG215, st25r3916.BlankNTAG216:
		fam = nfcfile.TargetNTAG
	case st25r3916.BlankMagicGen1A:
		fam = nfcfile.TargetMagicGen1A
	case st25r3916.BlankMifareClassic1K, st25r3916.BlankMifareClassic4K:
		fam = nfcfile.TargetMifare
	case st25r3916.BlankNFCV:
		fam = nfcfile.TargetNFCV
	}
	return nfcfile.TargetDescriptor{
		Family: fam, Label: b.Label, UIDLen: len(b.UID),
		MaxUserPages: b.MaxUserPages, MaxBlocks: b.MaxBlocks,
	}
}
