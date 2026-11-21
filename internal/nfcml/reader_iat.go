package nfcml

import (
	"fmt"
	"math"
	"time"
)

// ReaderActivity classifies the external NFC reader's polling cadence during card emulation.
type ReaderActivity int

const (
	ActivityQuiet ReaderActivity = iota
	ActivityPolling
	ActivityEngaged
)

// String returns a concise LCD label for ReaderActivity.
func (a ReaderActivity) String() string {
	switch a {
	case ActivityPolling:
		return "POLL"
	case ActivityEngaged:
		return "ENGAGED"
	default:
		return "QUIET"
	}
}

// ActivityReport summarizes the live reader cadence state.
type ActivityReport struct {
	Activity    ReaderActivity `json:"activity"`
	Label       string         `json:"label"`
	PeriodMs    int64          `json:"period_ms"`
	RecentBurst int            `json:"recent_burst"`
}

const (
	iatRingSize       = 16
	engagedMaxGapMs   = 90
	engagedBurstCount = 3
	quietTimeoutMs    = 2500
)

// ReaderIATTracker is a zero-allocation fixed-ring inter-arrival time tracker
// detecting when an external NFC reader transitions from idle polling to an active transaction.
type ReaderIATTracker struct {
	gapsMs    [iatRingSize]int64
	head      int
	count     int
	lastHit   time.Time
	fastRun   int
	baselineP float64
}

// NewReaderIATTracker creates a fresh ReaderIATTracker.
func NewReaderIATTracker() *ReaderIATTracker {
	return &ReaderIATTracker{}
}

// Reset clears all recorded reader hit timestamps.
func (t *ReaderIATTracker) Reset() {
	*t = ReaderIATTracker{}
}

// ObserveHit records an external reader target hit at timestamp now and returns the updated ActivityReport.
func (t *ReaderIATTracker) ObserveHit(now time.Time) ActivityReport {
	if !t.lastHit.IsZero() {
		gapMs := now.Sub(t.lastHit).Milliseconds()
		if gapMs >= 0 && gapMs <= quietTimeoutMs {
			t.gapsMs[t.head] = gapMs
			t.head = (t.head + 1) % iatRingSize
			if t.count < iatRingSize {
				t.count++
			}
			isFast := gapMs <= engagedMaxGapMs || (t.baselineP > 140 && float64(gapMs) < t.baselineP*0.5)
			if isFast {
				t.fastRun++
			} else {
				t.fastRun = 0
				if t.baselineP == 0 {
					t.baselineP = float64(gapMs)
				} else {
					t.baselineP = 0.85*t.baselineP + 0.15*float64(gapMs)
				}
			}
		} else {
			t.fastRun = 0
		}
	}
	t.lastHit = now
	return t.Report(now)
}

// Report returns the current reader activity classification at time now with 0 allocations.
func (t *ReaderIATTracker) Report(now time.Time) ActivityReport {
	if t.lastHit.IsZero() || now.Sub(t.lastHit).Milliseconds() > quietTimeoutMs {
		return ActivityReport{Activity: ActivityQuiet, Label: "QUIET"}
	}
	if t.fastRun >= engagedBurstCount {
		return ActivityReport{
			Activity:    ActivityEngaged,
			Label:       "ENGAGED!",
			PeriodMs:    int64(math.Round(t.baselineP)),
			RecentBurst: t.fastRun,
		}
	}
	if t.count >= 2 && t.baselineP >= 100 {
		p := int64(math.Round(t.baselineP))
		return ActivityReport{
			Activity: ActivityPolling,
			Label:    fmt.Sprintf("POLL %dms", p),
			PeriodMs: p,
		}
	}
	return ActivityReport{Activity: ActivityPolling, Label: "POLL"}
}
