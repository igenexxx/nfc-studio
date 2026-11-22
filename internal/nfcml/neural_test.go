package nfcml

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"nfc-studio/internal/nfcfile"
)

func TestNFCNeuralModelsLoadedAndFallback(t *testing.T) {
	indInfo, dumpInfo := ActiveModelsInfo()
	if indInfo.Source != "embedded" || indInfo.ByteSize == 0 || len(indInfo.Classes) != InductiveClasses {
		t.Fatalf("unexpected NFC-InductiveNet info: %+v", indInfo)
	}
	if dumpInfo.Source != "embedded" || dumpInfo.ByteSize == 0 || len(dumpInfo.Classes) != DumpEcosystemClasses {
		t.Fatalf("unexpected NFC-DumpNet info: %+v", dumpInfo)
	}

	badSHA := append([]byte(nil), embeddedInductiveNet...)
	badSHA[len(badSHA)-1] ^= 0xFF
	if _, err := LoadCZMLBytes(badSHA, DomainNFCInductive); !errors.Is(err, ErrCZMLChecksum) {
		t.Fatalf("expected ErrCZMLChecksum, got %v", err)
	}
	if _, err := LoadCZMLBytes(embeddedInductiveNet, DomainNFCDump); !errors.Is(err, ErrCZMLDomainMismatch) {
		t.Fatalf("expected ErrCZMLDomainMismatch, got %v", err)
	}

	dir := t.TempDir()
	badFile := filepath.Join(dir, "nfc_inductive.czml")
	if err := os.WriteFile(badFile, badSHA, 0o600); err != nil {
		t.Fatal(err)
	}
	m, info := LoadDefaultInductiveNet(badFile)
	if m == nil || info.Source != "embedded" {
		t.Fatalf("expected embedded fallback on bad override, got %+v", info)
	}
}

func TestNFCInductiveNetMaterials(t *testing.T) {
	cases := []struct {
		name      string
		amp, ph   int16
		wantLabel string
	}{
		{"air", 0, 0, "AIR"},
		{"ferrous_steel", -35, 25, "FERROUS_STEEL"},
		{"aluminum", -24, -26, "ALUMINUM"},
		{"copper_brass", -10, -44, "COPPER_BRASS"},
		{"resonant_tag", -28, 0, "RESONANT_TAG"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var da, dp [InductiveSeqLen]int16
			for i := range InductiveSeqLen {
				da[i] = tc.amp
				dp[i] = tc.ph
			}
			res, ok := ClassifyInductiveTrajectory(&da, &dp)
			if !ok {
				t.Fatal("ClassifyInductiveTrajectory unavailable")
			}
			if res.Label != tc.wantLabel {
				t.Errorf("%s: got %s (%.2f), want %s", tc.name, res.Label, res.Confidence, tc.wantLabel)
			}
		})
	}
}

func TestNFCDumpNetEcosystemsAndEmbedding(t *testing.T) {
	// Blank NTAG215
	blankPages := make([][]byte, 135)
	for i := range blankPages {
		blankPages[i] = []byte{0, 0, 0, 0}
	}
	blankTag := &nfcfile.NfcTag{
		Protocol: "NFC-A",
		UID:      []byte{0x04, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66},
		ATQA:     [2]byte{0x44, 0x00},
		SAK:      0x00,
		Pages:    blankPages,
	}

	// NDEF URI tag
	ndefPages := make([][]byte, 45)
	for i := range ndefPages {
		ndefPages[i] = []byte{0, 0, 0, 0}
	}
	ndefPages[4] = []byte{0x03, 0x11, 0xD1, 0x01}
	ndefPages[5] = []byte{0x0D, 0x55, 0x04, 'e'}
	ndefPages[6] = []byte{'x', 'a', 'm', 'p'}
	ndefPages[7] = []byte{'l', 'e', '.', 'c'}
	ndefPages[8] = []byte{'o', 'm', 0xFE, 0x00}
	ndefTag := &nfcfile.NfcTag{
		Protocol: "NFC-A",
		UID:      []byte{0x04, 0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF},
		ATQA:     [2]byte{0x44, 0x00},
		SAK:      0x00,
		Pages:    ndefPages,
	}

	rBlank, ok := ClassifyTagDump(blankTag)
	if !ok || rBlank.Label != "Blank" {
		t.Fatalf("blank tag got %+v ok=%v, want Blank", rBlank, ok)
	}
	rNDEF, ok := ClassifyTagDump(ndefTag)
	if !ok || rNDEF.Label != "NDEF_URI_Text" {
		t.Fatalf("ndef tag got %+v ok=%v, want NDEF_URI_Text", rNDEF, ok)
	}

	// Verify unit-norm 32D learned embedding & concurrent safety.
	var normSq float64
	for _, v := range rNDEF.Embedding {
		normSq += float64(v) * float64(v)
	}
	if math.Abs(math.Sqrt(normSq)-1.0) > 1e-3 {
		t.Fatalf("expected unit L2 norm for 32D embedding, got %.6f", math.Sqrt(normSq))
	}

	var wg sync.WaitGroup
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 10 {
				_, ok := ClassifyTagDump(ndefTag)
				if !ok {
					t.Errorf("ClassifyTagDump failed under concurrency")
				}
			}
		}()
	}
	wg.Wait()
}

func TestNFCInductiveZeroAlloc(t *testing.T) {
	m, _ := LoadDefaultInductiveNet("")
	if m == nil {
		t.Fatal("embedded NFC-InductiveNet unavailable")
	}
	var ws NFCWorkspace
	var da, dp [InductiveSeqLen]int16
	for i := range InductiveSeqLen {
		da[i] = -30
		dp[i] = 20
	}
	if allocs := testing.AllocsPerRun(50, func() {
		_ = m.InferInductive(&ws, &da, &dp)
	}); allocs != 0 {
		t.Fatalf("NFC-InductiveNet allocated %.1f times/op, want 0", allocs)
	}
}

func BenchmarkNFCInductiveInfer(b *testing.B) {
	m, _ := LoadDefaultInductiveNet("")
	if m == nil {
		b.Fatal("embedded NFC-InductiveNet unavailable")
	}
	var ws NFCWorkspace
	var da, dp [InductiveSeqLen]int16
	b.ReportAllocs()
	for b.Loop() {
		_ = m.InferInductive(&ws, &da, &dp)
	}
}

func BenchmarkNFCDumpInfer(b *testing.B) {
	m, _ := LoadDefaultDumpNet("")
	if m == nil {
		b.Fatal("embedded NFC-DumpNet unavailable")
	}
	pages := make([][]byte, 45)
	for i := range pages {
		pages[i] = []byte{0x01, 0x02, 0x03, 0x04}
	}
	tag := &nfcfile.NfcTag{
		Protocol: "NFC-A",
		UID:      []byte{0x04, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66},
		ATQA:     [2]byte{0x44, 0x00},
		Pages:    pages,
	}
	var ws NFCWorkspace
	b.ReportAllocs()
	for b.Loop() {
		_ = m.InferDump(&ws, tag)
	}
}
