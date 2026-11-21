package nfcml

import (
	"testing"
	"time"

	"nfc-studio/internal/nfcfile"
)

func TestAnalyzeDumpClasses(t *testing.T) {
	blankPages := make([][]byte, 12)
	for i := range blankPages {
		blankPages[i] = []byte{0x00, 0x00, 0x00, 0x00}
	}
	ndefPages := [][]byte{
		{0x04, 0xA1, 0xB2, 0x77}, {0xC3, 0xD4, 0xE5, 0xF6}, {0x11, 0x48, 0x00, 0x00}, {0xE1, 0x10, 0x3E, 0x00},
		{0x03, 0x0F, 0xD1, 0x01}, {0x0B, 0x55, 0x04, 'e'}, {'x', 'a', 'm', 'p'}, {'l', 'e', '.', 'c'}, {'o', 'm', 0xFE, 0x00},
	}
	encPages := make([][]byte, 68)
	for i := range encPages {
		encPages[i] = []byte{byte(i * 4), byte(i*4 + 1), byte(i*4 + 2), byte(i*4 + 3)}
	}
	propPages := [][]byte{
		{0, 0, 0, 0}, {0, 0, 0, 0}, {0, 0, 0, 0}, {0, 0, 0, 0},
		{0xAA, 0x55, 0x01, 0x02}, {0xAA, 0x55, 0x03, 0x04},
	}

	tests := []struct {
		name  string
		pages [][]byte
		want  DumpClass
	}{
		{"nil_pages", nil, ClassBlank},
		{"blank", blankPages, ClassBlank},
		{"ndef", ndefPages, ClassNDEF},
		{"encrypted_high_entropy", encPages, ClassEncrypted},
		{"proprietary", propPages, ClassProprietary},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := AnalyzeDump(tc.pages)
			if got.Class != tc.want {
				t.Fatalf("AnalyzeDump = %v (ent=%.2f fill=%.2f), want %v", got.Class, got.EntropyBits, got.FillRatio, tc.want)
			}
		})
	}
}

func TestFingerprintMatchAndUIDGate(t *testing.T) {
	tagA := &nfcfile.NfcTag{
		DeviceType: "NTAG215", UID: []byte{0x04, 1, 2, 3, 4, 5, 6}, ATQA: [2]byte{0x44, 0x00}, SAK: 0x00,
		Pages: [][]byte{{0, 0, 0, 0}, {0, 0, 0, 0}, {0, 0, 0, 0}, {0xE1, 0x10, 0x3E, 0}, {0x03, 0x07, 0xD1, 0x01}, {0x03, 0x55, 0x04, 'a'}, {'b', 0xFE, 0, 0}},
	}
	tagB := &nfcfile.NfcTag{
		DeviceType: "MIFARE Classic 1K", UID: []byte{0xDE, 0xAD, 0xBE, 0xEF}, ATQA: [2]byte{0x04, 0x00}, SAK: 0x08,
	}
	idx := NewFingerprintIndex([]FingerprintEntry{
		{Name: "url.nfc", DeviceType: tagA.DeviceType, UIDLen: 7, Embedding: ComputeEmbedding(tagA)},
		{Name: "mfc.nfc", DeviceType: tagB.DeviceType, UIDLen: 4, Embedding: ComputeEmbedding(tagB)},
	})
	m, ok := idx.Match(ComputeEmbedding(tagA), 7, 0.90)
	if !ok || m.Name != "url.nfc" || m.Similarity < 0.99 {
		t.Fatalf("expected url.nfc match, got %+v ok=%v", m, ok)
	}
	if _, ok := idx.Match(ComputeEmbedding(tagA), 4, 0.99); ok {
		t.Fatal("UID length gate must block 7B match when querying 4B")
	}
}

func TestReaderIATPollingToEngaged(t *testing.T) {
	tr := NewReaderIATTracker()
	t0 := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	for i := range 5 {
		tr.ObserveHit(t0.Add(time.Duration(i*200) * time.Millisecond))
	}
	rep := tr.Report(t0.Add(900 * time.Millisecond))
	if rep.Activity != ActivityPolling {
		t.Fatalf("expected ActivityPolling, got %+v", rep)
	}
	// Rapid 30ms multi-hit transaction burst -> ENGAGED!
	base := t0.Add(1000 * time.Millisecond)
	for i := range 4 {
		rep = tr.ObserveHit(base.Add(time.Duration(i*30) * time.Millisecond))
	}
	if rep.Activity != ActivityEngaged {
		t.Fatalf("expected ActivityEngaged on rapid burst, got %+v", rep)
	}
}

func TestFieldBaselineSweetSpotMetalAndFreeze(t *testing.T) {
	fb := NewFieldBaseline()
	// Warmup unloaded field around amp=180, phase=120.
	for range 8 {
		fb.Update(180, 120)
	}
	if !fb.Ready() {
		t.Fatal("expected FieldBaseline ready")
	}
	idle := fb.Update(180, 120)
	if idle.State != FieldIdle {
		t.Fatalf("expected FieldIdle, got %+v", idle)
	}
	// Resonant card loads amplitude downward (-14 counts) with mild phase shift.
	sweet := fb.Update(166, 122)
	if sweet.State != FieldSweetSpot || sweet.QualityPct < 75 {
		t.Fatalf("expected FieldSweetSpot, got %+v", sweet)
	}
	// Metal collapses amplitude AND shifts phase strongly.
	metal := fb.Update(120, 160)
	if metal.State != FieldMetalDetune {
		t.Fatalf("expected FieldMetalDetune, got %+v", metal)
	}
	// Unloaded baseline must not have been poisoned by metal/card readings.
	after := fb.Update(180, 120)
	if after.State != FieldIdle {
		t.Fatalf("baseline poisoned after detune: %+v", after)
	}
}

func BenchmarkNFCMLZeroAlloc(b *testing.B) {
	pages := make([][]byte, 16)
	for i := range pages {
		pages[i] = []byte{byte(i), byte(i + 1), 0x41, 0x42}
	}
	fb := NewFieldBaseline()
	for range 8 {
		fb.Update(180, 120)
	}
	b.Run("AnalyzeDump", func(b *testing.B) {
		for b.Loop() {
			_ = AnalyzeDump(pages)
		}
	})
	b.Run("FieldBaselineUpdate", func(b *testing.B) {
		for b.Loop() {
			_ = fb.Update(166, 122)
		}
	})
}
