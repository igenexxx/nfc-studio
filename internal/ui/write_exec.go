package ui

import (
	"context"
	"errors"
	"fmt"
	"time"

	"nfc-studio/internal/nfcfile"
	"nfc-studio/internal/st25r3916"
)

func executeWritePlanAsync(
	ctx context.Context,
	simMode bool,
	reader NFCReader,
	autoMode bool,
	sub WriteSubMode,
	srcTag *nfcfile.NfcTag,
	opts nfcfile.PlanOptions,
	plan nfcfile.WritePlan,
	report func(done, total int, stage string),
) WriteDone {
	if simMode {
		return executeSimWritePlan(ctx, plan, planTotalSteps(plan), report)
	}
	writer, ok := reader.(NFCWriteChip)
	if !ok || writer == nil {
		return WriteDone{OK: false, ErrText: "Active chip does not support writes"}
	}
	detectedBlank, compatLevel, compatNote := "", nfcfile.CompatLevel(""), ""
	if autoMode {
		report(0, 1, "Probing blank card...")
		blank, err := writer.ProbeBlankTarget(ctx)
		if err != nil {
			return WriteDone{OK: false, ErrText: formatWriteErr(err, -1)}
		}
		detectedBlank = blank.Label
		desc := blankToTargetDescriptor(blank)
		if sub == WriteSubNDEF {
			if desc.Family != nfcfile.TargetNTAG {
				return WriteDone{OK: false, DetectedBlank: detectedBlank, CompatLevel: nfcfile.CompatIncompatible, ErrText: fmt.Sprintf("NDEF needs NTAG, got %s", blank.Label)}
			}
			if desc.MaxUserPages > 0 && len(plan.NTAGPages) > desc.MaxUserPages {
				return WriteDone{OK: false, DetectedBlank: detectedBlank, CompatLevel: nfcfile.CompatIncompatible, ErrText: fmt.Sprintf("NDEF needs %d pg, %s holds %d", len(plan.NTAGPages), blank.Label, desc.MaxUserPages)}
			}
			compatLevel = nfcfile.CompatExact
		} else if srcTag != nil {
			compat, err := nfcfile.ReconcileWritePlan(srcTag, desc, opts)
			if err != nil {
				return WriteDone{OK: false, DetectedBlank: detectedBlank, ErrText: err.Error()}
			}
			compatLevel, compatNote = compat.Level, compat.Reason
			if compat.Level == nfcfile.CompatIncompatible {
				return WriteDone{OK: false, DetectedBlank: detectedBlank, CompatLevel: compat.Level, CompatNote: compat.Reason, ErrText: compat.Reason}
			}
			plan = compat.Plan
		}
	}
	res := runHardwareWriteStrategy(ctx, writer, autoMode, plan, report)
	res.DetectedBlank = detectedBlank
	res.CompatLevel = compatLevel
	res.CompatNote = compatNote
	return res
}

func runHardwareWriteStrategy(
	ctx context.Context,
	writer NFCWriteChip,
	autoMode bool,
	plan nfcfile.WritePlan,
	report func(done, total int, stage string),
) WriteDone {
	switch plan.Strategy {
	case nfcfile.StrategyNTAGPages:
		ops := make([]st25r3916.WritePageOp, len(plan.NTAGPages))
		for i, pg := range plan.NTAGPages {
			ops[i] = st25r3916.WritePageOp{Page: uint8(int(plan.NTAGStartPage) + i), Data: pg}
		}
		rep, err := writer.WriteType2Pages(ctx, ops, st25r3916.WriteOptions{
			VerifyReadBack: true,
			OnProgress:     func(done, tot int) { report(done, tot, fmt.Sprintf("NTAG P%02d (%d/%d)...", int(plan.NTAGStartPage)+done-1, done, tot)) },
		})
		if err != nil {
			return WriteDone{OK: false, ErrText: formatWriteErr(err, rep.FailedPage)}
		}
		return WriteDone{OK: true, Summary: fmt.Sprintf("VERIFIED OK: %d/%d %s pg", rep.PagesWritten, len(ops), rep.Model)}

	case nfcfile.StrategyMagicGen1A:
		rep, err := writer.WriteMagicGen1ADump(ctx, plan.UID, plan.ATQA, plan.SAK, toDriverMifareBlocks(plan.MifareBlocks), true, func(done, tot int) {
			report(done, tot, fmt.Sprintf("Gen1A blk %d/%d...", done, tot))
		})
		if err != nil {
			return WriteDone{OK: false, ErrText: formatWriteErr(err, rep.FailedBlock)}
		}
		return WriteDone{OK: true, Summary: fmt.Sprintf("GEN1A OK: %d blk UID %s", rep.BlocksWritten, rep.ReadBackUID)}

	case nfcfile.StrategyMagicGen2:
		blks := toDriverMifareBlocks(plan.MifareBlocks)
		keys := toDriverSectorAuth(plan.SectorKeys)
		if autoMode {
			rep, err := writer.WriteMifareCrypto1Auto(ctx, plan.UID, plan.ATQA, plan.SAK, blks, keys, true, true, func(done, tot int) {
				report(done, tot, fmt.Sprintf("Crypto1 blk %d/%d...", done, tot))
			})
			if err != nil {
				return WriteDone{OK: false, ErrText: formatWriteErr(err, rep.FailedBlock)}
			}
			if rep.Block0LockedFallback {
				return WriteDone{OK: true, Summary: fmt.Sprintf("DATA OK (%d blk) - UID locked (Std)", rep.BlocksWritten)}
			}
			return WriteDone{OK: true, Summary: fmt.Sprintf("GEN2 OK: %d blk UID %s", rep.BlocksWritten, rep.ReadBackUID)}
		}
		rep, err := writer.WriteMifareCrypto1Dump(ctx, st25r3916.MagicGen2CUID, plan.UID, plan.ATQA, plan.SAK, true, blks, keys, true, func(done, tot int) {
			report(done, tot, fmt.Sprintf("Crypto1 blk %d/%d...", done, tot))
		})
		if err != nil {
			return WriteDone{OK: false, ErrText: formatWriteErr(err, rep.FailedBlock)}
		}
		return WriteDone{OK: true, Summary: fmt.Sprintf("GEN2 OK: %d blk verified", rep.BlocksWritten)}

	case nfcfile.StrategyMifareSectors:
		rep, err := writer.WriteMifareCrypto1Dump(ctx, st25r3916.MagicRegular, plan.UID, plan.ATQA, plan.SAK, false, toDriverMifareBlocks(plan.MifareBlocks), toDriverSectorAuth(plan.SectorKeys), true, func(done, tot int) {
			report(done, tot, fmt.Sprintf("Crypto1 blk %d/%d...", done, tot))
		})
		if err != nil {
			return WriteDone{OK: false, ErrText: formatWriteErr(err, rep.FailedBlock)}
		}
		return WriteDone{OK: true, Summary: fmt.Sprintf("MF DATA OK: %d blk verified", rep.BlocksWritten)}

	case nfcfile.StrategyNFCVBlocks:
		n, err := writer.WriteNFCVBlocksWithProgress(ctx, plan.NFCVStartBlock, plan.NFCVBlocks, true, func(done, tot int) {
			report(done, tot, fmt.Sprintf("NFC-V blk %d/%d...", done, tot))
		})
		if err != nil {
			return WriteDone{OK: false, ErrText: formatWriteErr(err, n)}
		}
		return WriteDone{OK: true, Summary: fmt.Sprintf("NFC-V OK: %d/%d blk verified", n, len(plan.NFCVBlocks))}

	default:
		return WriteDone{OK: false, ErrText: plan.Reason}
	}
}

func blankToTargetDescriptor(b *st25r3916.BlankTarget) nfcfile.TargetDescriptor {
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

func executeSimWritePlan(ctx context.Context, plan nfcfile.WritePlan, total int, report func(int, int, string)) WriteDone {
	stepDelay := 12 * time.Millisecond
	if total > 32 {
		stepDelay = 4 * time.Millisecond
	}
	for i := 1; i <= total; i++ {
		if ctx.Err() != nil {
			return WriteDone{OK: false, ErrText: "Cancelled"}
		}
		report(i, total, fmt.Sprintf("SIM writing %d/%d...", i, total))
		time.Sleep(stepDelay)
	}
	return WriteDone{OK: true, Summary: fmt.Sprintf("SIM VERIFIED: %s", truncate(plan.Summary, 30))}
}

func toDriverMifareBlocks(in []nfcfile.MifareBlockOp) []st25r3916.MifareBlockWrite {
	out := make([]st25r3916.MifareBlockWrite, len(in))
	for i, op := range in {
		out[i] = st25r3916.MifareBlockWrite{Block: op.Block, Data: op.Data}
	}
	return out
}

func toDriverSectorAuth(in []nfcfile.MifareSectorKey) []st25r3916.MifareSectorAuth {
	out := make([]st25r3916.MifareSectorAuth, len(in))
	for i, k := range in {
		out[i] = st25r3916.MifareSectorAuth{
			Sector: k.Sector, HasKeyA: k.HasKeyA, KeyA: k.KeyA, HasKeyB: k.HasKeyB, KeyB: k.KeyB,
		}
	}
	return out
}

func formatWriteErr(err error, failedIdx int) string {
	switch {
	case errors.Is(err, st25r3916.ErrNoTag):
		return "No 13.56MHz blank (125kHz unsupported)"
	case errors.Is(err, st25r3916.ErrBlock0Locked):
		return "Regular MIFARE: Blk0 locked (need Magic)"
	case errors.Is(err, st25r3916.ErrWriteNAK):
		if failedIdx >= 0 {
			return fmt.Sprintf("NAK at #%d (locked/wrong card type)", failedIdx)
		}
		return "Card rejected write (NAK / locked)"
	case errors.Is(err, st25r3916.ErrVerifyFailed):
		if failedIdx >= 0 {
			return fmt.Sprintf("Verify mismatch at #%d (hold steady)", failedIdx)
		}
		return "Read-back verify mismatch"
	case errors.Is(err, context.Canceled):
		return "Write cancelled"
	default:
		return truncate(err.Error(), 46)
	}
}
