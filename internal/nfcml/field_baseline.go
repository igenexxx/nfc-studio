package nfcml

import "math"

// FieldState classifies the 13.56 MHz antenna coupling state from ST25R3916 ADC readings.
type FieldState int

const (
	FieldWarmup FieldState = iota
	FieldIdle
	FieldWeak
	FieldSweetSpot
	FieldMetalDetune
)

// String returns a concise LCD label for FieldState.
func (s FieldState) String() string {
	switch s {
	case FieldIdle:
		return "IDLE"
	case FieldWeak:
		return "WEAK"
	case FieldSweetSpot:
		return "SWEET"
	case FieldMetalDetune:
		return "METAL"
	default:
		return "CAL"
	}
}

// FieldReading holds a single ST25R3916 ADC amplitude/phase coupling evaluation.
type FieldReading struct {
	Amp        uint8      `json:"amp"`
	Phase      uint8      `json:"phase"`
	AmpZ       float32    `json:"amp_z"`
	PhaseZ     float32    `json:"phase_z"`
	QualityPct int        `json:"quality_pct"`
	State      FieldState `json:"state"`
	StateLabel string     `json:"state_label"`
}

const (
	fieldWarmupSamples = 6
	fieldEWMAAlpha     = 0.06
	fieldFreezeZ       = 3.0
	fieldMinStd        = 1.5
)

type scalarStat struct {
	n      int
	mean   float64
	m2     float64
	varVal float64
}

func (s *scalarStat) zScore(x float64) float64 {
	std := math.Sqrt(s.varVal)
	if std < fieldMinStd {
		std = fieldMinStd
	}
	return (x - s.mean) / std
}

func (s *scalarStat) update(x float64, freeze bool) {
	if s.n < fieldWarmupSamples {
		s.n++
		delta := x - s.mean
		s.mean += delta / float64(s.n)
		s.m2 += delta * (x - s.mean)
		if s.n > 1 {
			s.varVal = s.m2 / float64(s.n-1)
		}
		return
	}
	if freeze {
		return
	}
	diff := x - s.mean
	s.mean += fieldEWMAAlpha * diff
	s.varVal = (1.0-fieldEWMAAlpha)*s.varVal + fieldEWMAAlpha*diff*diff
}

// FieldBaseline tracks the unloaded 13.56 MHz ST25R3916 ADC amplitude & phase baseline
// with Welford warmup, EWMA drift tracking, and a detune-freeze guard (0 allocations).
type FieldBaseline struct {
	amp   scalarStat
	phase scalarStat
}

// NewFieldBaseline creates a fresh ST25R3916 RF field baseline estimator.
func NewFieldBaseline() *FieldBaseline {
	return &FieldBaseline{}
}

// Reset clears the learned unloaded field baseline.
func (fb *FieldBaseline) Reset() {
	*fb = FieldBaseline{}
}

// Ready reports whether Welford warmup has completed.
func (fb *FieldBaseline) Ready() bool {
	return fb.amp.n >= fieldWarmupSamples
}

// Update ingests a live (amp, phase) ADC reading and returns the coupling classification.
func (fb *FieldBaseline) Update(amp, phase uint8) FieldReading {
	a, p := float64(amp), float64(phase)
	if !fb.Ready() {
		fb.amp.update(a, false)
		fb.phase.update(p, false)
		return FieldReading{Amp: amp, Phase: phase, State: FieldWarmup, StateLabel: FieldWarmup.String()}
	}
	az := fb.amp.zScore(a)
	pz := fb.phase.zScore(p)
	// Freeze unloaded baseline whenever a card or metal detunes the antenna.
	freeze := math.Abs(az) >= fieldFreezeZ || math.Abs(pz) >= fieldFreezeZ
	fb.amp.update(a, freeze)
	fb.phase.update(p, freeze)

	state, qual := classifyField(az, pz)
	return FieldReading{
		Amp:        amp,
		Phase:      phase,
		AmpZ:       float32(az),
		PhaseZ:     float32(pz),
		QualityPct: qual,
		State:      state,
		StateLabel: state.String(),
	}
}

func classifyField(az, pz float64) (FieldState, int) {
	absA, absP := math.Abs(az), math.Abs(pz)
	// Severe phase shift with collapsed amplitude indicates conductive metal detuning.
	if absP >= 6.0 && az <= -6.0 {
		return FieldMetalDetune, 15
	}
	// Resonant 13.56 MHz card loads amplitude downward (-3..-12 sigma) with moderate phase shift.
	if az <= -2.5 && absP < 6.0 {
		q := int(math.Round(min(100.0, 55.0+math.Abs(az)*7.5)))
		if q >= 75 {
			return FieldSweetSpot, q
		}
		return FieldWeak, q
	}
	if absA >= 2.0 || absP >= 2.5 {
		return FieldWeak, 45
	}
	return FieldIdle, 0
}
