package ui

import (
	"context"
	"time"

	"nfc-studio/internal/nfcfile"
	"nfc-studio/internal/st25r3916"
)

const (
	writeArmWindow = 5 * time.Second
	writeChanBuf   = 16
)

// NFCWriteChip is an optional consumer-owned capability interface for physical card writing.
type NFCWriteChip interface {
	ProbeBlankTarget(ctx context.Context) (*st25r3916.BlankTarget, error)
	WriteType2Pages(ctx context.Context, ops []st25r3916.WritePageOp, opts st25r3916.WriteOptions) (st25r3916.WriteReport, error)
	CloneMagicUID(ctx context.Context, gen st25r3916.MagicCardGen, uid []byte, atqa [2]byte, sak uint8, keyA [6]byte) (st25r3916.CloneReport, error)
	WriteMagicGen1ADump(ctx context.Context, uid []byte, atqa [2]byte, sak uint8, blocks []st25r3916.MifareBlockWrite, verify bool, onProgress func(int, int)) (st25r3916.MagicDumpReport, error)
	WriteMifareCrypto1Dump(ctx context.Context, gen st25r3916.MagicCardGen, uid []byte, atqa [2]byte, sak uint8, writeBlock0 bool, blocks []st25r3916.MifareBlockWrite, sectorAuth []st25r3916.MifareSectorAuth, verify bool, onProgress func(int, int)) (st25r3916.MagicDumpReport, error)
	WriteMifareCrypto1Auto(ctx context.Context, uid []byte, atqa [2]byte, sak uint8, blocks []st25r3916.MifareBlockWrite, sectorAuth []st25r3916.MifareSectorAuth, allowRegularFallback, verify bool, onProgress func(int, int)) (st25r3916.MagicDumpReport, error)
	WriteNFCVBlocksWithProgress(ctx context.Context, startBlock uint8, blocks [][4]byte, verify bool, onProgress func(int, int)) (int, error)
}

func newWriteState() WriteState {
	return WriteState{
		writeProgCh:  make(chan WriteProgress, writeChanBuf),
		writeDoneCh:  make(chan WriteDone, 2),
		writeProbeCh: make(chan WriteProbeDone, 2),
	}
}

func (a *App) openWriteView(returnMode Mode) {
	if a.CurrentTag == nil && len(a.SavedFiles) > 0 {
		a.CurrentTag = a.SavedFiles[0].Tag
		a.updateNDEF()
	}
	a.WriteReturnMode = returnMode
	a.WriteArmedUntil = time.Time{}
	a.WriteResultSet = false
	a.WriteResultOK = false
	a.WriteResultMsg = ""
	a.WriteDetectedBlank = ""
	a.WriteCompatLevel = ""
	a.WriteCompatNote = ""
	a.WriteDoneCount = 0
	a.WriteTotalCount = 0
	a.WriteStage = ""
	if a.CurrentTag == nil {
		a.WriteSub = WriteSubNDEF
	} else {
		a.WriteSub = WriteSubDump
	}
	a.SwitchMode(ModeWrite)
}

func (a *App) isWriteAutoMode() bool {
	return a.WriteMifareMode%len(mifareWriteModeLabels) == 0
}

func (a *App) writePlanOptions() nfcfile.PlanOptions {
	switch a.WriteMifareMode % len(mifareWriteModeLabels) {
	case 2:
		return nfcfile.PlanOptions{MagicGen: "gen1a", UIDOnly: true, IncludeTrailers: a.WriteTrailers}
	case 3:
		return nfcfile.PlanOptions{MagicGen: "gen2", UIDOnly: false, IncludeTrailers: a.WriteTrailers}
	case 4:
		return nfcfile.PlanOptions{MagicGen: "gen2", UIDOnly: true, IncludeTrailers: a.WriteTrailers}
	case 5:
		return nfcfile.PlanOptions{MagicGen: "regular", UIDOnly: false, IncludeTrailers: a.WriteTrailers}
	default:
		return nfcfile.PlanOptions{MagicGen: "gen1a", UIDOnly: false, IncludeTrailers: a.WriteTrailers}
	}
}

func (a *App) currentWritePlan() (nfcfile.WritePlan, error) {
	if a.WriteSub == WriteSubNDEF {
		return a.buildNDEFQuickPlan()
	}
	if a.CurrentTag == nil {
		return nfcfile.WritePlan{Strategy: nfcfile.StrategyUnsupported, Reason: "No card loaded - read or pick a .nfc"}, nil
	}
	return nfcfile.BuildWritePlan(a.CurrentTag, a.writePlanOptions())
}

func (a *App) startBlankProbePreview() {
	if a.WriteBusy {
		return
	}
	a.SetToast("Probing blank card...")
	simMode := a.SimMode
	chip := a.Chip
	sub := a.WriteSub
	tag := a.CurrentTag
	opts := a.writePlanOptions()
	probeCh := a.writeProbeCh

	go func() {
		if simMode {
			probeCh <- WriteProbeDone{BlankLabel: "SIM NTAG215 (126 pg)", CompatLevel: nfcfile.CompatExact, CompatNote: "SIM ready"}
			return
		}
		writer, ok := chip.(NFCWriteChip)
		if !ok || writer == nil {
			probeCh <- WriteProbeDone{ErrText: "Chip cannot probe blank"}
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		blank, err := writer.ProbeBlankTarget(ctx)
		if err != nil {
			probeCh <- WriteProbeDone{ErrText: formatWriteErr(err, -1)}
			return
		}
		desc := blankToTargetDescriptor(blank)
		if sub == WriteSubNDEF {
			if desc.Family == nfcfile.TargetNTAG {
				probeCh <- WriteProbeDone{BlankLabel: blank.Label, CompatLevel: nfcfile.CompatExact, CompatNote: "Ready for Quick NDEF"}
			} else {
				probeCh <- WriteProbeDone{BlankLabel: blank.Label, CompatLevel: nfcfile.CompatIncompatible, CompatNote: "Quick NDEF requires NTAG21x"}
			}
			return
		}
		if tag == nil {
			probeCh <- WriteProbeDone{BlankLabel: blank.Label, CompatNote: "Detected blank"}
			return
		}
		compat, err := nfcfile.ReconcileWritePlan(tag, desc, opts)
		if err != nil {
			probeCh <- WriteProbeDone{BlankLabel: blank.Label, ErrText: err.Error()}
			return
		}
		probeCh <- WriteProbeDone{BlankLabel: blank.Label, CompatLevel: compat.Level, CompatNote: compat.Reason}
	}()
}

func (a *App) startWriteJob() {
	if a.WriteBusy {
		return
	}
	plan, err := a.currentWritePlan()
	if err != nil || plan.Strategy == nfcfile.StrategyUnsupported {
		msg := "Unsupported card"
		if err != nil {
			msg = err.Error()
		} else if plan.Reason != "" {
			msg = plan.Reason
		}
		a.SetToast(truncate(msg, 28))
		return
	}
	a.WriteArmedUntil = time.Time{}
	a.WriteBusy = true
	a.WriteResultSet = false
	a.WriteDoneCount = 0
	a.WriteTotalCount = planTotalSteps(plan)
	a.WriteStage = "Probing blank card..."
	a.syncPollFlags()

	ctx, cancel := context.WithCancel(context.Background())
	a.writeCancel = cancel
	progCh := a.writeProgCh
	doneCh := a.writeDoneCh
	simMode := a.SimMode
	chip := a.Chip
	autoMode := a.isWriteAutoMode() || a.WriteSub == WriteSubNDEF
	sub := a.WriteSub
	srcTag := a.CurrentTag
	opts := a.writePlanOptions()

	go func() {
		defer cancel()
		res := executeWritePlanAsync(ctx, simMode, chip, autoMode, sub, srcTag, opts, plan, func(done, total int, stage string) {
			select {
			case progCh <- WriteProgress{Done: done, Total: total, Stage: stage}:
			default:
			}
		})
		doneCh <- res
	}()
}

func (a *App) cancelWriteJob() {
	if a.writeCancel != nil {
		a.writeCancel()
		a.writeCancel = nil
	}
	a.WriteArmedUntil = time.Time{}
	if a.WriteBusy {
		a.WriteBusy = false
		a.WriteResultSet = true
		a.WriteResultOK = false
		a.WriteResultMsg = "Write cancelled"
		a.syncPollFlags()
	}
}

func (a *App) pollWriteResults() {
	for {
		select {
		case pr := <-a.writeProbeCh:
			if pr.ErrText != "" {
				a.WriteDetectedBlank = ""
				a.WriteCompatLevel = nfcfile.CompatIncompatible
				a.WriteCompatNote = pr.ErrText
				a.SetToast(truncate(pr.ErrText, 26))
			} else {
				a.WriteDetectedBlank = pr.BlankLabel
				a.WriteCompatLevel = pr.CompatLevel
				a.WriteCompatNote = pr.CompatNote
				a.SetToast("Blank: " + truncate(pr.BlankLabel, 20))
			}
		case p := <-a.writeProgCh:
			a.WriteDoneCount = p.Done
			a.WriteTotalCount = max(1, p.Total)
			if p.Stage != "" {
				a.WriteStage = p.Stage
			}
		case res := <-a.writeDoneCh:
			a.WriteBusy = false
			a.writeCancel = nil
			a.WriteResultSet = true
			a.WriteResultOK = res.OK
			if res.DetectedBlank != "" {
				a.WriteDetectedBlank = res.DetectedBlank
			}
			if res.CompatLevel != "" {
				a.WriteCompatLevel = res.CompatLevel
				a.WriteCompatNote = res.CompatNote
			}
			if res.OK {
				a.WriteDoneCount = max(a.WriteDoneCount, a.WriteTotalCount)
				a.WriteResultMsg = res.Summary
				a.SetToast("WRITE OK!")
			} else {
				a.WriteResultMsg = res.ErrText
				a.SetToast("WRITE FAILED")
			}
			a.syncPollFlags()
		default:
			return
		}
	}
}

func planTotalSteps(p nfcfile.WritePlan) int {
	switch p.Strategy {
	case nfcfile.StrategyNTAGPages:
		return max(1, len(p.NTAGPages))
	case nfcfile.StrategyMagicGen1A, nfcfile.StrategyMagicGen2:
		return 1 + len(p.MifareBlocks)
	case nfcfile.StrategyMifareSectors:
		return max(1, len(p.MifareBlocks))
	case nfcfile.StrategyNFCVBlocks:
		return max(1, len(p.NFCVBlocks))
	default:
		return 1
	}
}
