package nfcml

import (
	"math"

	"nfc-studio/internal/nfcfile"
)

// DumpClass classifies the payload structure of an NFC tag's user memory (P04+).
type DumpClass int

const (
	ClassBlank DumpClass = iota
	ClassNDEF
	ClassProprietary
	ClassEncrypted
)

// String returns the compact LCD label for DumpClass.
func (c DumpClass) String() string {
	switch c {
	case ClassNDEF:
		return "NDEF"
	case ClassProprietary:
		return "PROPRIETARY"
	case ClassEncrypted:
		return "ENCRYPTED"
	default:
		return "BLANK"
	}
}

// DumpProfile holds zero-allocation structural and entropy metrics for a tag dump.
type DumpProfile struct {
	Class       DumpClass `json:"class"`
	ClassLabel  string    `json:"class_label"`
	Confidence  float32   `json:"confidence"`
	EntropyBits float32   `json:"entropy_bits"` // Shannon entropy 0.0..8.0 bits/byte
	FillRatio   float32   `json:"fill_ratio"`   // Fraction of non-zero bytes in user pages
	ASCIIRatio  float32   `json:"ascii_ratio"`  // Fraction of printable ASCII among non-zero bytes
	HasNDEF     bool      `json:"has_ndef"`
	UserBytes   int       `json:"user_bytes"`
}

// AnalyzeDump computes Shannon entropy, fill ratio, ASCII ratio, and NDEF presence
// across user memory pages (P04+) with zero heap allocations on the hot path.
func AnalyzeDump(pages [][]byte) DumpProfile {
	if len(pages) <= 4 {
		return DumpProfile{Class: ClassBlank, ClassLabel: ClassBlank.String(), Confidence: 1.0}
	}
	var hist [256]int
	total, nonZero, printable := 0, 0, 0
	for p := 4; p < len(pages); p++ {
		for _, b := range pages[p] {
			hist[b]++
			total++
			if b != 0x00 && b != 0xFF {
				nonZero++
				if b >= 32 && b <= 126 {
					printable++
				}
			}
		}
	}
	if total == 0 {
		return DumpProfile{Class: ClassBlank, ClassLabel: ClassBlank.String(), Confidence: 1.0}
	}
	_, hasNDEF := nfcfile.ExtractNDEF(pages)
	fill := float32(nonZero) / float32(total)
	var ascii float32
	if nonZero > 0 {
		ascii = float32(printable) / float32(nonZero)
	}
	ent := shannonEntropy(&hist, total)
	cls, conf := classifyDump(fill, ascii, ent, hasNDEF)
	return DumpProfile{
		Class:       cls,
		ClassLabel:  cls.String(),
		Confidence:  conf,
		EntropyBits: ent,
		FillRatio:   fill,
		ASCIIRatio:  ascii,
		HasNDEF:     hasNDEF,
		UserBytes:   total,
	}
}

func shannonEntropy(hist *[256]int, total int) float32 {
	if total <= 0 {
		return 0
	}
	invTotal := 1.0 / float64(total)
	var h float64
	for i := range 256 {
		if c := hist[i]; c > 0 {
			p := float64(c) * invTotal
			h -= p * math.Log2(p)
		}
	}
	return float32(h)
}

func classifyDump(fill, ascii, entropy float32, hasNDEF bool) (DumpClass, float32) {
	if fill < 0.03 {
		return ClassBlank, 0.98
	}
	if hasNDEF {
		return ClassNDEF, 0.96
	}
	if entropy >= 6.4 && ascii < 0.45 && fill >= 0.30 {
		conf := min(float32(0.99), float32(0.75)+(entropy-6.4)*0.15)
		return ClassEncrypted, conf
	}
	if ascii >= 0.55 {
		return ClassProprietary, 0.85
	}
	return ClassProprietary, 0.80
}
