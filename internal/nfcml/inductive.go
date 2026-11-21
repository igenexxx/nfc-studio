package nfcml

// MaterialClass classifies objects perturbing the ST25R3916 13.56 MHz LC tank.
type MaterialClass int

const (
	MaterialAir MaterialClass = iota
	MaterialFerrous
	MaterialNonFerrous
	MaterialResonantTag
	MaterialWaterDielectric
)

// String returns a concise English label for MaterialClass.
func (m MaterialClass) String() string {
	switch m {
	case MaterialFerrous:
		return "FERROUS"
	case MaterialNonFerrous:
		return "NON-FERROUS"
	case MaterialResonantTag:
		return "RESONANT-TAG"
	case MaterialWaterDielectric:
		return "WATER/DIELECTRIC"
	default:
		return "AIR"
	}
}

// MoistureLevel categorizes contactless dielectric water/moisture shift.
type MoistureLevel int

const (
	MoistureDry MoistureLevel = iota
	MoistureDamp
	MoistureWet
)

// String returns a concise English label for MoistureLevel.
func (m MoistureLevel) String() string {
	switch m {
	case MoistureDamp:
		return "DAMP"
	case MoistureWet:
		return "WET"
	default:
		return "DRY"
	}
}

// InductiveReading holds one zero-alloc inductive & dielectric evaluation.
type InductiveReading struct {
	Amp           uint8         `json:"amp"`
	Phase         uint8         `json:"phase"`
	DeltaAmp      int16         `json:"delta_amp"`
	DeltaPhase    int16         `json:"delta_phase"`
	StrengthPct   int           `json:"strength_pct"`
	Material      MaterialClass `json:"material"`
	MaterialLabel string        `json:"material_label"`
	MoistureLevel MoistureLevel `json:"moisture_level"`
	MoisturePct   int           `json:"moisture_pct"`
}

const (
	defaultAirAmp       uint8 = 180
	defaultAirPhase     uint8 = 120
	inductiveDeadband   int16 = 4
	phaseShiftThreshold int16 = 6
	ferrousAmpDrop      int16 = -8
	resonantAmpDrop     int16 = -6
	strengthScaleDenom        = 120
	moistureScaleDenom        = 60
	moistureDampPct           = 20
	moistureWetPct            = 60
	sparklineCapacity         = 32
)

// InductiveSensor performs zero-allocation metal/material classification and
// contactless dielectric moisture sensing from ST25R3916 ADC readings.
type InductiveSensor struct {
	baseAmp       uint8
	basePhase     uint8
	airCalibrated bool
	dryAmp        uint8
	dryPhase      uint8
	dryCalibrated bool
	sparkline     [sparklineCapacity]int16
	sparkHead     int
	sparkLen      int
}

// NewInductiveSensor returns a sensor initialized with nominal free-air defaults.
func NewInductiveSensor() *InductiveSensor {
	return &InductiveSensor{
		baseAmp:   defaultAirAmp,
		basePhase: defaultAirPhase,
	}
}

// CalibrateAir stores the unloaded free-air ADC baseline and clears sparkline history.
func (s *InductiveSensor) CalibrateAir(amp, phase uint8) {
	s.baseAmp = amp
	s.basePhase = phase
	s.airCalibrated = true
	s.sparkHead = 0
	s.sparkLen = 0
	clear(s.sparkline[:])
}

// CalibrateDry stores the dry-reference ADC baseline for contactless moisture sensing.
func (s *InductiveSensor) CalibrateDry(amp, phase uint8) {
	s.dryAmp = amp
	s.dryPhase = phase
	s.dryCalibrated = true
}

// AirBaseline returns the active air baseline and whether CalibrateAir was called.
func (s *InductiveSensor) AirBaseline() (amp, phase uint8, calibrated bool) {
	if !s.airCalibrated && s.baseAmp == 0 && s.basePhase == 0 {
		return defaultAirAmp, defaultAirPhase, false
	}
	return s.baseAmp, s.basePhase, s.airCalibrated
}

// DryBaseline returns the dry baseline and whether CalibrateDry was called.
func (s *InductiveSensor) DryBaseline() (amp, phase uint8, calibrated bool) {
	if !s.dryCalibrated {
		a, p, _ := s.AirBaseline()
		return a, p, false
	}
	return s.dryAmp, s.dryPhase, true
}

// Sample evaluates a live (amp, phase) ADC pair with zero heap allocations.
func (s *InductiveSensor) Sample(amp, phase uint8) InductiveReading {
	baseA, baseP, _ := s.AirBaseline()
	dAmp := int16(amp) - int16(baseA)
	dPhase := int16(phase) - int16(baseP)

	mat, strength := classifyInductiveMaterial(dAmp, dPhase)
	s.pushSparkline(int16(strength))

	dryA, dryP, _ := s.DryBaseline()
	mLevel, mPct := classifyDielectricMoisture(int16(amp)-int16(dryA), int16(phase)-int16(dryP))

	return InductiveReading{
		Amp:           amp,
		Phase:         phase,
		DeltaAmp:      dAmp,
		DeltaPhase:    dPhase,
		StrengthPct:   strength,
		Material:      mat,
		MaterialLabel: mat.String(),
		MoistureLevel: mLevel,
		MoisturePct:   mPct,
	}
}

// Sparkline copies up to len(dst) most recent StrengthPct values in chronological order.
func (s *InductiveSensor) Sparkline(dst []int16) int {
	n := min(len(dst), s.sparkLen)
	if n == 0 {
		return 0
	}
	start := (s.sparkHead - n + sparklineCapacity) % sparklineCapacity
	for i := range n {
		dst[i] = s.sparkline[(start+i)%sparklineCapacity]
	}
	return n
}

func (s *InductiveSensor) pushSparkline(v int16) {
	s.sparkline[s.sparkHead] = v
	s.sparkHead = (s.sparkHead + 1) % sparklineCapacity
	if s.sparkLen < sparklineCapacity {
		s.sparkLen++
	}
}

func classifyInductiveMaterial(dAmp, dPhase int16) (MaterialClass, int) {
	absA := abs16(dAmp)
	absP := abs16(dPhase)
	if absA < inductiveDeadband && absP < inductiveDeadband {
		return MaterialAir, 0
	}
	strength := min(100, (int(absA)*2+int(absP)*3)*100/strengthScaleDenom)

	switch {
	case dPhase <= -phaseShiftThreshold:
		return MaterialNonFerrous, strength
	case dPhase >= phaseShiftThreshold && dAmp <= ferrousAmpDrop:
		return MaterialFerrous, strength
	case dAmp <= resonantAmpDrop && absP < phaseShiftThreshold:
		return MaterialResonantTag, strength
	case dPhase >= inductiveDeadband && dAmp > ferrousAmpDrop:
		return MaterialWaterDielectric, strength
	case dPhase >= phaseShiftThreshold:
		return MaterialFerrous, strength
	default:
		return MaterialWaterDielectric, strength
	}
}

func classifyDielectricMoisture(dryDAmp, dryDPhase int16) (MoistureLevel, int) {
	ampDrop := max(int16(0), -dryDAmp)
	shift := int(abs16(dryDPhase))*3 + int(ampDrop)*2
	if shift < 6 {
		return MoistureDry, 0
	}
	pct := min(100, shift*100/moistureScaleDenom)
	switch {
	case pct >= moistureWetPct:
		return MoistureWet, pct
	case pct >= moistureDampPct:
		return MoistureDamp, pct
	default:
		return MoistureDry, pct
	}
}

func abs16(v int16) int16 {
	if v < 0 {
		return -v
	}
	return v
}
