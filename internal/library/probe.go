package library

import (
	"fmt"
	"time"
)

// ProbeStatus represents the Auto-Emu playlist execution state.
type ProbeStatus int

const (
	ProbeIdle ProbeStatus = iota
	ProbeRunning
	ProbePaused
	ProbeDone
)

// String returns the short LCD badge label for ProbeStatus.
func (s ProbeStatus) String() string {
	switch s {
	case ProbeRunning:
		return "RUN"
	case ProbePaused:
		return "PAUSE"
	case ProbeDone:
		return "DONE"
	default:
		return "IDLE"
	}
}

const maxRecentSent = 12

// IntervalPresets defines the SSOT discrete emulation dwell intervals for NFC Auto-Emu.
var IntervalPresets = []time.Duration{
	500 * time.Millisecond,
	1000 * time.Millisecond,
	1500 * time.Millisecond,
	2000 * time.Millisecond,
	3000 * time.Millisecond,
	5000 * time.Millisecond,
	8000 * time.Millisecond,
}

// ProbeSession manages the NFC Auto-Emu cursor, dwell interval, pause/rewind state,
// and recent emulated entry markers for human reaction-time backtracking.
type ProbeSession struct {
	Pack        PackInfo
	Entries     []EntryRef
	CursorIdx   int
	LastSentIdx int
	RecentSent  []int
	IntervalIdx int
	Status      ProbeStatus
	NextDueAt   time.Time
}

// NewProbeSession creates an idle ProbeSession with the default 2.0s dwell interval.
func NewProbeSession(defaultIntervalMs int) *ProbeSession {
	ps := &ProbeSession{
		LastSentIdx: -1,
		IntervalIdx: 3, // 2000ms
		Status:      ProbeIdle,
	}
	ps.SetIntervalMs(defaultIntervalMs)
	return ps
}

// SetCatalog loads a pack and its entries, resetting cursor and emulation history.
func (p *ProbeSession) SetCatalog(pack PackInfo, entries []EntryRef) {
	p.Pack = pack
	p.Entries = entries
	p.CursorIdx = 0
	p.LastSentIdx = -1
	p.RecentSent = nil
	p.Status = ProbeIdle
	p.NextDueAt = time.Time{}
}

// Interval returns the active dwell duration per emulated NFC tag.
func (p *ProbeSession) Interval() time.Duration {
	if p.IntervalIdx < 0 || p.IntervalIdx >= len(IntervalPresets) {
		return 2 * time.Second
	}
	return IntervalPresets[p.IntervalIdx]
}

// IntervalLabel returns a compact formatted interval label (e.g. "0.5s", "2.0s").
func (p *ProbeSession) IntervalLabel() string {
	return fmt.Sprintf("%.1fs", p.Interval().Seconds())
}

// SetIntervalMs snaps ms to the nearest supported IntervalPresets entry.
func (p *ProbeSession) SetIntervalMs(ms int) {
	if ms <= 0 {
		p.IntervalIdx = 3
		return
	}
	target := time.Duration(ms) * time.Millisecond
	bestIdx := 0
	bestDiff := absDuration(IntervalPresets[0] - target)
	for i := 1; i < len(IntervalPresets); i++ {
		if d := absDuration(IntervalPresets[i] - target); d < bestDiff {
			bestDiff = d
			bestIdx = i
		}
	}
	p.IntervalIdx = bestIdx
}

// StepInterval adjusts the dwell interval up (+1) or down (-1) within bounds.
func (p *ProbeSession) StepInterval(delta int) time.Duration {
	next := p.IntervalIdx + delta
	if next < 0 {
		next = 0
	}
	if next >= len(IntervalPresets) {
		next = len(IntervalPresets) - 1
	}
	p.IntervalIdx = next
	return p.Interval()
}

// Start begins or resumes Auto-Emu from the current CursorIdx immediately.
func (p *ProbeSession) Start(now time.Time) bool {
	if len(p.Entries) == 0 {
		return false
	}
	if p.CursorIdx < 0 || p.CursorIdx >= len(p.Entries) {
		p.CursorIdx = 0
	}
	p.Status = ProbeRunning
	p.NextDueAt = now
	return true
}

// Pause halts Auto-Emu and preserves CursorIdx, LastSentIdx, and RecentSent for rewinding.
func (p *ProbeSession) Pause() {
	if p.Status == ProbeRunning {
		p.Status = ProbePaused
	}
	p.NextDueAt = time.Time{}
}

// MoveCursor moves CursorIdx by delta (clamped to list bounds).
func (p *ProbeSession) MoveCursor(delta int) {
	if len(p.Entries) == 0 {
		p.CursorIdx = 0
		return
	}
	next := p.CursorIdx + delta
	if next < 0 {
		next = 0
	}
	if next >= len(p.Entries) {
		next = len(p.Entries) - 1
	}
	p.CursorIdx = next
}

// CurrentEntry returns the entry under CursorIdx, if any.
func (p *ProbeSession) CurrentEntry() (EntryRef, bool) {
	if p.CursorIdx < 0 || p.CursorIdx >= len(p.Entries) {
		return EntryRef{}, false
	}
	return p.Entries[p.CursorIdx], true
}

// PollDue returns the next entry to emulate if Status == ProbeRunning and NextDueAt has elapsed.
func (p *ProbeSession) PollDue(now time.Time) (EntryRef, int, bool) {
	if p.Status != ProbeRunning || len(p.Entries) == 0 {
		return EntryRef{}, -1, false
	}
	if !p.NextDueAt.IsZero() && now.Before(p.NextDueAt) {
		return EntryRef{}, -1, false
	}
	if p.CursorIdx >= len(p.Entries) {
		p.Status = ProbeDone
		return EntryRef{}, -1, false
	}
	idx := p.CursorIdx
	return p.Entries[idx], idx, true
}

// OnEmulated records that entry idx has been loaded into Passive Target A and schedules the next step.
func (p *ProbeSession) OnEmulated(idx int, now time.Time) {
	p.LastSentIdx = idx
	p.recordRecent(idx)
	if p.Status != ProbeRunning {
		return
	}
	if idx+1 >= len(p.Entries) {
		p.Status = ProbeDone
		p.NextDueAt = time.Time{}
		return
	}
	p.CursorIdx = idx + 1
	p.NextDueAt = now.Add(p.Interval())
}

// IsRecentSent reports whether idx is in the recent emulation trail (excluding LastSentIdx).
func (p *ProbeSession) IsRecentSent(idx int) bool {
	if idx == p.LastSentIdx {
		return false
	}
	for _, r := range p.RecentSent {
		if r == idx {
			return true
		}
	}
	return false
}

func (p *ProbeSession) recordRecent(idx int) {
	filtered := make([]int, 0, maxRecentSent)
	for _, r := range p.RecentSent {
		if r != idx {
			filtered = append(filtered, r)
		}
	}
	filtered = append(filtered, idx)
	if len(filtered) > maxRecentSent {
		filtered = filtered[len(filtered)-maxRecentSent:]
	}
	p.RecentSent = filtered
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}
