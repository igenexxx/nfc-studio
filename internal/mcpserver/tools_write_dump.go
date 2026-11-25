package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"nfc-studio/internal/nfcfile"
	"nfc-studio/internal/st25r3916"
)

// NFCMifareDumpWriter is an optional capability interface for multi-block Gen1A/Crypto1 writes.
type NFCMifareDumpWriter interface {
	WriteMagicGen1ADump(ctx context.Context, uid []byte, atqa [2]byte, sak uint8, blocks []st25r3916.MifareBlockWrite, verify bool, onProgress func(int, int)) (st25r3916.MagicDumpReport, error)
	WriteMifareCrypto1Dump(ctx context.Context, gen st25r3916.MagicCardGen, uid []byte, atqa [2]byte, sak uint8, writeBlock0 bool, blocks []st25r3916.MifareBlockWrite, sectorAuth []st25r3916.MifareSectorAuth, verify bool, onProgress func(int, int)) (st25r3916.MagicDumpReport, error)
	WriteMifareCrypto1Auto(ctx context.Context, uid []byte, atqa [2]byte, sak uint8, blocks []st25r3916.MifareBlockWrite, sectorAuth []st25r3916.MifareSectorAuth, allowRegularFallback, verify bool, onProgress func(int, int)) (st25r3916.MagicDumpReport, error)
}

func (s *Server) toolWriteSavedTag(ctx context.Context, raw json.RawMessage) (toolResult, *rpcError) {
	var in struct {
		Name                 string `json:"name"`
		MagicGen             string `json:"magic_gen"`
		UIDOnly              bool   `json:"uid_only"`
		IncludeTrailers      bool   `json:"include_trailers"`
		AllowRegularFallback *bool  `json:"allow_regular_fallback"`
		Verify               *bool  `json:"verify"`
		ConfirmWrite         bool   `json:"confirm_write"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return toolResult{}, &rpcError{Code: errCodeInvalidParams, Message: "Invalid arguments: " + err.Error()}
	}
	if !in.ConfirmWrite {
		return errResult("confirm_write must be true to write a saved .nfc tag to a blank card"), nil
	}
	safeName, err := sanitizeFilename(in.Name)
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
	gen := strings.ToLower(strings.TrimSpace(in.MagicGen))
	autoMode := gen == "" || gen == "auto"
	planOpts := nfcfile.PlanOptions{
		MagicGen:        gen,
		UIDOnly:         in.UIDOnly,
		IncludeTrailers: in.IncludeTrailers,
	}
	if autoMode {
		planOpts.MagicGen = "gen1a"
	}
	verify := true
	if in.Verify != nil {
		verify = *in.Verify
	}
	allowFallback := true
	if in.AllowRegularFallback != nil {
		allowFallback = *in.AllowRegularFallback
	}
	if s.backend == nil {
		plan, err := nfcfile.BuildWritePlan(tag, planOpts)
		if err != nil {
			return errResult(err.Error()), nil
		}
		if plan.Strategy == nfcfile.StrategyUnsupported {
			return errResult("cannot write saved tag: " + plan.Reason), nil
		}
		return simWriteSavedTagResult(safeName, plan, verify, autoMode), nil
	}
	return s.execWriteSavedTagHW(ctx, safeName, tag, planOpts, autoMode, allowFallback, verify), nil
}

func simWriteSavedTagResult(name string, plan nfcfile.WritePlan, verify, autoMode bool) toolResult {
	out := map[string]any{
		"sim_mode":           true,
		"auto_mode":          autoMode,
		"name":               name,
		"strategy":           plan.Strategy,
		"summary":            plan.Summary,
		"notice":             plan.Notice,
		"verified":           verify,
		"skipped_trailers":   plan.SkippedTrailers,
		"skipped_unreadable": plan.SkippedUnreadable,
	}
	switch plan.Strategy {
	case nfcfile.StrategyNTAGPages:
		out["pages_written"] = len(plan.NTAGPages)
	case nfcfile.StrategyNFCVBlocks:
		out["blocks_written"] = len(plan.NFCVBlocks)
	case nfcfile.StrategyMifareSectors:
		out["blocks_written"] = len(plan.MifareBlocks)
	default:
		out["blocks_written"] = 1 + len(plan.MifareBlocks)
		out["target_uid"] = nfcfile.FormatHex(plan.UID)
		out["read_back_uid"] = nfcfile.FormatHex(plan.UID)
	}
	return okJSON(out)
}

func (s *Server) execWriteSavedTagHW(
	ctx context.Context,
	name string,
	tag *nfcfile.NfcTag,
	planOpts nfcfile.PlanOptions,
	autoMode, allowFallback, verify bool,
) toolResult {
	var plan nfcfile.WritePlan
	var blank *st25r3916.BlankTarget
	var compatNote string
	var compatLevel nfcfile.CompatLevel

	if autoMode {
		prober, ok := s.backend.(NFCBlankProber)
		if !ok {
			return errResult("active backend does not support auto blank detection")
		}
		s.hwMu.Lock()
		var err error
		blank, err = prober.ProbeBlankTarget(ctx)
		s.hwMu.Unlock()
		if err != nil {
			return errResult("auto blank probe failed: " + err.Error())
		}
		compat, err := nfcfile.ReconcileWritePlan(tag, blankToDescriptor(blank), planOpts)
		if err != nil {
			return errResult(err.Error())
		}
		compatLevel, compatNote = compat.Level, compat.Reason
		if compat.Level == nfcfile.CompatIncompatible {
			return errResult("incompatible blank card: " + compat.Reason)
		}
		plan = compat.Plan
	} else {
		var err error
		plan, err = nfcfile.BuildWritePlan(tag, planOpts)
		if err != nil {
			return errResult(err.Error())
		}
		if plan.Strategy == nfcfile.StrategyUnsupported {
			return errResult("cannot write saved tag: " + plan.Reason)
		}
	}

	out := map[string]any{"name": name, "strategy": plan.Strategy, "summary": plan.Summary}
	if blank != nil {
		out["detected_blank"] = blank
		out["compat_level"] = compatLevel
		out["compat_note"] = compatNote
	}

	switch plan.Strategy {
	case nfcfile.StrategyNTAGPages:
		writer, ok := s.backend.(NFCWriter)
		if !ok {
			return errResult("active backend does not support NTAG writes")
		}
		ops := make([]st25r3916.WritePageOp, len(plan.NTAGPages))
		for i, pg := range plan.NTAGPages {
			ops[i] = st25r3916.WritePageOp{Page: uint8(int(plan.NTAGStartPage) + i), Data: pg}
		}
		s.hwMu.Lock()
		rep, err := writer.WriteType2Pages(ctx, ops, st25r3916.WriteOptions{VerifyReadBack: verify})
		s.hwMu.Unlock()
		if err != nil {
			return errResult("NTAG saved write failed: " + err.Error())
		}
		out["report"] = rep
		return okJSON(out)

	case nfcfile.StrategyMagicGen1A:
		writer, ok := s.backend.(NFCMifareDumpWriter)
		if !ok {
			return errResult("active backend does not support Gen1A dump writes")
		}
		s.hwMu.Lock()
		rep, err := writer.WriteMagicGen1ADump(ctx, plan.UID, plan.ATQA, plan.SAK, convertPlanBlocks(plan.MifareBlocks), verify, nil)
		s.hwMu.Unlock()
		if err != nil {
			return errResult("Gen1A dump write failed: " + err.Error())
		}
		rep.BlocksSkipped = plan.SkippedTrailers + plan.SkippedUnreadable
		out["report"] = rep
		return okJSON(out)

	case nfcfile.StrategyMagicGen2, nfcfile.StrategyMifareSectors:
		writer, ok := s.backend.(NFCMifareDumpWriter)
		if !ok {
			return errResult("active backend does not support Crypto1 MIFARE writes")
		}
		var rep st25r3916.MagicDumpReport
		var err error
		s.hwMu.Lock()
		if autoMode && plan.Strategy == nfcfile.StrategyMagicGen2 {
			rep, err = writer.WriteMifareCrypto1Auto(ctx, plan.UID, plan.ATQA, plan.SAK, convertPlanBlocks(plan.MifareBlocks), convertPlanKeys(plan.SectorKeys), allowFallback, verify, nil)
		} else {
			gen := st25r3916.MagicGen2CUID
			writeBlk0 := true
			if plan.Strategy == nfcfile.StrategyMifareSectors {
				gen = st25r3916.MagicRegular
				writeBlk0 = false
			}
			rep, err = writer.WriteMifareCrypto1Dump(ctx, gen, plan.UID, plan.ATQA, plan.SAK, writeBlk0, convertPlanBlocks(plan.MifareBlocks), convertPlanKeys(plan.SectorKeys), verify, nil)
		}
		s.hwMu.Unlock()
		if err != nil {
			return errResult("Crypto1 MIFARE write failed: " + err.Error())
		}
		rep.BlocksSkipped = plan.SkippedTrailers + plan.SkippedUnreadable
		out["report"] = rep
		return okJSON(out)

	case nfcfile.StrategyNFCVBlocks:
		writer, ok := s.backend.(NFCVWriter)
		if !ok {
			return errResult("active backend does not support NFC-V writes")
		}
		s.hwMu.Lock()
		n, err := writer.WriteNFCVBlocks(ctx, plan.NFCVStartBlock, plan.NFCVBlocks, verify)
		s.hwMu.Unlock()
		if err != nil {
			return errResult("NFC-V saved write failed: " + err.Error())
		}
		out["blocks_written"] = n
		out["verified"] = verify
		return okJSON(out)

	default:
		return errResult("unsupported write strategy: " + plan.Reason)
	}
}

func (s *Server) toolWriteMifareGen1A(ctx context.Context, raw json.RawMessage) (toolResult, *rpcError) {
	var in struct {
		UIDHex  string `json:"uid_hex"`
		ATQAHex string `json:"atqa_hex"`
		SAKHex  string `json:"sak_hex"`
		Blocks  []struct {
			Block int    `json:"block"`
			Hex   string `json:"hex"`
		} `json:"blocks"`
		IncludeTrailers bool  `json:"include_trailers"`
		Verify          *bool `json:"verify"`
		ConfirmWrite    bool  `json:"confirm_write"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return toolResult{}, &rpcError{Code: errCodeInvalidParams, Message: "Invalid arguments: " + err.Error()}
	}
	if !in.ConfirmWrite {
		return errResult("confirm_write must be true to write a Gen1A Magic Card"), nil
	}
	uid, atqa, sak, _, err := parseMagicCloneArgs(in.UIDHex, in.ATQAHex, in.SAKHex, "")
	if err != nil {
		return errResult(err.Error()), nil
	}
	ops := make([]st25r3916.MifareBlockWrite, 0, len(in.Blocks))
	for i, b := range in.Blocks {
		if b.Block < 1 || b.Block > 255 {
			return errResult(fmt.Sprintf("blocks[%d].block must be 1..255", i)), nil
		}
		if nfcfile.IsMifareTrailerBlock(b.Block) && !in.IncludeTrailers {
			return errResult(fmt.Sprintf("block %d is a sector trailer; set include_trailers=true to override", b.Block)), nil
		}
		rawBlk, err := parseHexFlex(b.Hex)
		if err != nil || len(rawBlk) != 16 {
			return errResult(fmt.Sprintf("blocks[%d].hex must be 16 hex bytes", i)), nil
		}
		var data [16]byte
		copy(data[:], rawBlk)
		ops = append(ops, st25r3916.MifareBlockWrite{Block: uint8(b.Block), Data: data})
	}
	verify := true
	if in.Verify != nil {
		verify = *in.Verify
	}
	if s.backend == nil {
		return okJSON(map[string]any{
			"sim_mode":       true,
			"gen":            st25r3916.MagicGen1A,
			"target_uid":     nfcfile.FormatHex(uid),
			"read_back_uid":  nfcfile.FormatHex(uid),
			"blocks_written": 1 + len(ops),
			"verified":       verify,
		}), nil
	}
	writer, ok := s.backend.(NFCMifareDumpWriter)
	if !ok {
		return errResult("active backend does not support Gen1A dump writes"), nil
	}
	s.hwMu.Lock()
	rep, err := writer.WriteMagicGen1ADump(ctx, uid, atqa, sak, ops, verify, nil)
	s.hwMu.Unlock()
	if err != nil {
		return errResult("Gen1A write failed: " + err.Error()), nil
	}
	return okJSON(rep), nil
}

func convertPlanBlocks(in []nfcfile.MifareBlockOp) []st25r3916.MifareBlockWrite {
	out := make([]st25r3916.MifareBlockWrite, len(in))
	for i, op := range in {
		out[i] = st25r3916.MifareBlockWrite{Block: op.Block, Data: op.Data}
	}
	return out
}

func convertPlanKeys(in []nfcfile.MifareSectorKey) []st25r3916.MifareSectorAuth {
	out := make([]st25r3916.MifareSectorAuth, len(in))
	for i, k := range in {
		out[i] = st25r3916.MifareSectorAuth{
			Sector: k.Sector, HasKeyA: k.HasKeyA, KeyA: k.KeyA, HasKeyB: k.HasKeyB, KeyB: k.KeyB,
		}
	}
	return out
}
