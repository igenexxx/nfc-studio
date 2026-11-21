package nfcml

import "testing"

func TestInductiveMaterialClassification(t *testing.T) {
	s := NewInductiveSensor()
	s.CalibrateAir(180, 120)

	tests := []struct {
		name         string
		amp, phase   uint8
		wantMaterial MaterialClass
		wantMinStr   int
		wantMaxStr   int
	}{
		{name: "exact_air_baseline", amp: 180, phase: 120, wantMaterial: MaterialAir, wantMinStr: 0, wantMaxStr: 0},
		{name: "within_deadband", amp: 177, phase: 123, wantMaterial: MaterialAir, wantMinStr: 0, wantMaxStr: 0},
		{name: "ferrous_steel_pos_phase_amp_drop", amp: 140, phase: 138, wantMaterial: MaterialFerrous, wantMinStr: 80, wantMaxStr: 100},
		{name: "non_ferrous_copper_neg_phase", amp: 150, phase: 102, wantMaterial: MaterialNonFerrous, wantMinStr: 70, wantMaxStr: 100},
		{name: "resonant_1356_tag", amp: 160, phase: 122, wantMaterial: MaterialResonantTag, wantMinStr: 25, wantMaxStr: 80},
		{name: "water_dielectric_mild_amp_pos_phase", amp: 176, phase: 128, wantMaterial: MaterialWaterDielectric, wantMinStr: 20, wantMaxStr: 60},
		{name: "extreme_zero_adc", amp: 0, phase: 0, wantMaterial: MaterialNonFerrous, wantMinStr: 100, wantMaxStr: 100},
		{name: "extreme_max_adc", amp: 255, phase: 255, wantMaterial: MaterialWaterDielectric, wantMinStr: 100, wantMaxStr: 100},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := s.Sample(tc.amp, tc.phase)
			if r.Material != tc.wantMaterial {
				t.Fatalf("Sample(%d,%d).Material = %v, want %v (dA=%d dP=%d)", tc.amp, tc.phase, r.Material, tc.wantMaterial, r.DeltaAmp, r.DeltaPhase)
			}
			if r.MaterialLabel != tc.wantMaterial.String() {
				t.Fatalf("MaterialLabel = %q, want %q", r.MaterialLabel, tc.wantMaterial.String())
			}
			if r.StrengthPct < tc.wantMinStr || r.StrengthPct > tc.wantMaxStr {
				t.Fatalf("StrengthPct = %d, want [%d..%d]", r.StrengthPct, tc.wantMinStr, tc.wantMaxStr)
			}
		})
	}
}

func TestInductiveMoistureSensor(t *testing.T) {
	s := NewInductiveSensor()
	s.CalibrateAir(180, 120)
	s.CalibrateDry(178, 121)

	a, p, cal := s.DryBaseline()
	if !cal || a != 178 || p != 121 {
		t.Fatalf("DryBaseline() = (%d, %d, %v), want (178, 121, true)", a, p, cal)
	}

	tests := []struct {
		name       string
		amp, phase uint8
		wantLevel  MoistureLevel
		wantMinPct int
		wantMaxPct int
	}{
		{name: "dry_reference", amp: 178, phase: 121, wantLevel: MoistureDry, wantMinPct: 0, wantMaxPct: 19},
		{name: "dry_noise", amp: 177, phase: 122, wantLevel: MoistureDry, wantMinPct: 0, wantMaxPct: 19},
		{name: "damp_soil", amp: 174, phase: 127, wantLevel: MoistureDamp, wantMinPct: 20, wantMaxPct: 59},
		{name: "wet_water", amp: 170, phase: 138, wantLevel: MoistureWet, wantMinPct: 60, wantMaxPct: 100},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := s.Sample(tc.amp, tc.phase)
			if r.MoistureLevel != tc.wantLevel {
				t.Fatalf("MoistureLevel = %v (pct=%d), want %v", r.MoistureLevel, r.MoisturePct, tc.wantLevel)
			}
			if r.MoisturePct < tc.wantMinPct || r.MoisturePct > tc.wantMaxPct {
				t.Fatalf("MoisturePct = %d, want [%d..%d]", r.MoisturePct, tc.wantMinPct, tc.wantMaxPct)
			}
		})
	}
}

func TestInductiveZeroValueAndSparklineWrap(t *testing.T) {
	// Zero-value InductiveSensor must work without panicking using default air baseline.
	var zero InductiveSensor
	r := zero.Sample(defaultAirAmp, defaultAirPhase)
	if r.Material != MaterialAir || r.StrengthPct != 0 {
		t.Fatalf("zero-value Sample(default) = %+v, want MaterialAir", r)
	}

	// Push 40 samples (> 32 capacity) and verify chronological order.
	for i := range 40 {
		zero.Sample(uint8(180-i), 120)
	}
	var dst [sparklineCapacity]int16
	n := zero.Sparkline(dst[:])
	if n != sparklineCapacity {
		t.Fatalf("Sparkline count = %d, want %d", n, sparklineCapacity)
	}
	if dst[0] > dst[n-1] {
		t.Fatalf("Sparkline not chronological: first=%d last=%d", dst[0], dst[n-1])
	}
	// Empty destination slice returns 0 safely.
	if got := zero.Sparkline(nil); got != 0 {
		t.Fatalf("Sparkline(nil) = %d, want 0", got)
	}
}

func TestInductiveLabels(t *testing.T) {
	if MaterialClass(99).String() != "AIR" {
		t.Fatalf("unknown MaterialClass label = %q", MaterialClass(99).String())
	}
	if MoistureLevel(99).String() != "DRY" || MoistureDamp.String() != "DAMP" || MoistureWet.String() != "WET" {
		t.Fatal("unexpected MoistureLevel String() output")
	}
}

func BenchmarkInductive(b *testing.B) {
	s := NewInductiveSensor()
	s.CalibrateAir(180, 120)
	s.CalibrateDry(178, 121)

	b.ReportAllocs()
	for b.Loop() {
		_ = s.Sample(148, 134)
	}
}
