package ui

import (
	"bytes"
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"nfc-studio/internal/library"
	"nfc-studio/internal/nfcfile"
	"nfc-studio/internal/nfcml"
	"nfc-studio/internal/st25r3916"
)

const (
	pollerInterval     = 180 * time.Millisecond
	scanChanBuf        = 4
	hitChanBuf         = 8
	quickMifareTimeout = 4500 * time.Millisecond
)

var quickMifareDefaultKeys = [][6]byte{
	{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF},
	{0xA0, 0xA1, 0xA2, 0xA3, 0xA4, 0xA5},
	{0xD3, 0xF7, 0xD3, 0xF7, 0xD3, 0xF7},
}

// PollerState holds background SPI polling synchronization channels and flags.
type PollerState struct {
	wantScan       atomic.Bool
	wantEmu        atomic.Bool
	wantMifareDump atomic.Bool
	scanCh     chan *nfcfile.NfcTag
	hitCh      chan struct{}
	fieldCh    chan [2]uint8
	extFieldCh chan uint8
	stopCh     chan struct{}
	doneCh     chan struct{}
	pollerOn   bool
}

func newPollerState() PollerState {
	return PollerState{
		scanCh:     make(chan *nfcfile.NfcTag, scanChanBuf),
		hitCh:      make(chan struct{}, hitChanBuf),
		fieldCh:    make(chan [2]uint8, scanChanBuf),
		extFieldCh: make(chan uint8, scanChanBuf),
	}
}

// StartPoller launches the background NFC polling goroutine when hardware is present.
func (a *App) StartPoller() {
	if a.SimMode || a.Chip == nil || a.pollerOn {
		return
	}
	a.stopCh = make(chan struct{})
	a.doneCh = make(chan struct{})
	a.pollerOn = true
	a.syncPollFlags()
	go a.pollLoop()
}

func (a *App) pollLoop() {
	defer close(a.doneCh)
	ticker := time.NewTicker(pollerInterval)
	defer ticker.Stop()
	for {
		select {
		case <-a.stopCh:
			return
		case <-ticker.C:
			a.pollOnce()
		}
	}
}

func (a *App) pollOnce() {
	if a.wantScan.Load() {
		if fm, ok := a.Chip.(interface{ MeasureField() (uint8, uint8, error) }); ok {
			if amp, phase, err := fm.MeasureField(); err == nil {
				select {
				case a.fieldCh <- [2]uint8{amp, phase}:
				default:
				}
			}
		}
		if info, err := a.Chip.PollISO14443A(); err == nil && info != nil {
			tag := a.enrichMifareReadIfNeeded(info)
			select {
			case a.scanCh <- tag:
			default:
			}
		}
		return
	}
	if a.wantEmu.Load() {
		if ef, ok := a.Chip.(interface{ MeasureExternalField() (uint8, error) }); ok {
			if amp, err := ef.MeasureExternalField(); err == nil {
				select {
				case a.extFieldCh <- amp:
				default:
				}
			}
		}
		if hit, err := a.Chip.PollTargetHit(); err == nil && hit {
			select {
			case a.hitCh <- struct{}{}:
			default:
			}
		}
	}
}

// StopPoller stops the background NFC polling goroutine cleanly.
func (a *App) StopPoller() {
	if !a.pollerOn {
		return
	}
	close(a.stopCh)
	<-a.doneCh
	a.pollerOn = false
}

func (a *App) syncPollFlags() {
	if a.Mode == ModeWrite || a.WriteBusy {
		a.wantScan.Store(false)
		a.wantEmu.Store(false)
		a.wantMifareDump.Store(false)
		return
	}
	scanningScreen := a.Mode == ModeScan || (a.Mode == ModeRead && !a.ReadCaptured)
	a.wantScan.Store(!a.SimMode && scanningScreen && a.Scanning && !a.EmuActive)
	a.wantEmu.Store(!a.SimMode && (a.Mode == ModeEmu || a.Mode == ModeProbe) && a.EmuActive)
	a.wantMifareDump.Store(!a.SimMode && a.Mode == ModeRead && !a.ReadCaptured)
}

func (a *App) enrichMifareReadIfNeeded(info *st25r3916.TagInfo) *nfcfile.NfcTag {
	if a.wantMifareDump.Load() && (info.SAK == 0x08 || info.SAK == 0x18) {
		if dr, ok := a.Chip.(interface {
			ReadMifareClassicDict(context.Context, [][6]byte, func(st25r3916.DictProgress)) (*st25r3916.MifareDump, error)
		}); ok {
			ctx, cancel := context.WithTimeout(context.Background(), quickMifareTimeout)
			defer cancel()
			if dump, _ := dr.ReadMifareClassicDict(ctx, quickMifareDefaultKeys, nil); dump != nil && len(dump.Sectors) > 0 {
				return dump.ToNfcTag()
			}
		}
	}
	return tagInfoToNfcTag(info)
}

func tagInfoToNfcTag(info *st25r3916.TagInfo) *nfcfile.NfcTag {
	pages := make([][]byte, len(info.Pages))
	for i, p := range info.Pages {
		pages[i] = append([]byte(nil), p...)
	}
	uid := append([]byte(nil), info.UID...)
	return &nfcfile.NfcTag{
		FileType:   "Flipper NFC device",
		Version:    4,
		DeviceType: nfcfile.IdentifyCard(info.ATQA, info.SAK, len(uid)),
		UID:        uid,
		ATQA:       info.ATQA,
		SAK:        info.SAK,
		PagesTotal: len(pages),
		Pages:      pages,
	}
}

func (a *App) stepSimTelemetry() {
	if a.SimMode && (a.Mode == ModeEmu || a.Mode == ModeProbe) && a.EmuActive {
		simAmp := uint8(135 + (a.tick%8)*6)
		a.ExternalFieldAmp = simAmp
		a.ExternalFieldPct = st25r3916.ExternalFieldPercent(simAmp)
		if a.tick%simHitTickModulo == 0 {
			a.ReaderHits++
			a.HitFlashUntil = time.Now().Add(350 * time.Millisecond)
			if a.ReaderIAT != nil {
				a.LastActivity = a.ReaderIAT.ObserveHit(time.Now())
			}
		}
	} else if !a.EmuActive {
		a.ExternalFieldAmp = 0
		a.ExternalFieldPct = 0
	}
	if a.SimMode && (a.Mode == ModeScan || a.Mode == ModeRead) && a.FieldBase != nil {
		amp, phase := uint8(180), uint8(120)
		if a.CurrentTag != nil && a.tick > 8 {
			amp, phase = 166, 122
		}
		a.LastField = a.FieldBase.Update(amp, phase)
	}
}

func (a *App) drainPollerEvents() {
	for {
		select {
		case tag := <-a.scanCh:
			isNew := a.CurrentTag == nil || !bytes.Equal(a.CurrentTag.UID, tag.UID)
			a.CurrentTag = tag
			a.LastSeen = time.Now()
			a.updateNDEF()
			if a.Mode == ModeRead && !a.ReadCaptured {
				a.ReadCaptured = true
				a.syncPollFlags()
				a.SetToast("Tag read! 4=EMU 5=SAVE 6=WR")
			} else if isNew {
				if a.HasMatch {
					a.SetToast(fmt.Sprintf("Match: %s (%.0f%%)", truncate(a.LastMatch.Name, 12), a.LastMatch.Similarity*100))
				} else {
					a.SetToast("Found: " + tag.DeviceType)
				}
			}
		case fld := <-a.fieldCh:
			if a.FieldBase != nil {
				a.LastField = a.FieldBase.Update(fld[0], fld[1])
			}
		case extAmp := <-a.extFieldCh:
			a.ExternalFieldAmp = extAmp
			a.ExternalFieldPct = st25r3916.ExternalFieldPercent(extAmp)
		case <-a.hitCh:
			a.ReaderHits++
			a.HitFlashUntil = time.Now().Add(350 * time.Millisecond)
			if a.ReaderIAT != nil {
				a.LastActivity = a.ReaderIAT.ObserveHit(time.Now())
				if a.Mode == ModeProbe && a.Probe != nil && a.Probe.Status == library.ProbeRunning && a.LastActivity.Activity == nfcml.ActivityEngaged {
					a.stopActiveProbe()
					a.SetToast("Reader ENGAGED! Paused")
				}
			}
		default:
			return
		}
	}
}
