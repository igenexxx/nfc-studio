package ui

import (
	"sync/atomic"
	"time"

	"nfc-studio/internal/nfcfile"
	"nfc-studio/internal/st25r3916"
)

const (
	pollerInterval = 180 * time.Millisecond
	scanChanBuf    = 4
	hitChanBuf     = 8
)

// PollerState holds background SPI polling synchronization channels and flags.
type PollerState struct {
	wantScan   atomic.Bool
	wantEmu    atomic.Bool
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
			select {
			case a.scanCh <- tagInfoToNfcTag(info):
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
	a.wantScan.Store(!a.SimMode && a.Mode == ModeScan && a.Scanning && !a.EmuActive)
	a.wantEmu.Store(!a.SimMode && (a.Mode == ModeEmu || a.Mode == ModeProbe) && a.EmuActive)
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
