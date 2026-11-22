package nfcml

import (
	"math"

	"nfc-studio/internal/nfcfile"
)

const (
	// InductiveSeqLen is the temporal sweep window for NFC-InductiveNet.
	InductiveSeqLen = 32
	// InductiveClasses is the number of learned material classes.
	InductiveClasses = 7
	// DumpFeatureDim is the input feature count for NFC-DumpNet.
	DumpFeatureDim = 48
	// DumpEcosystemClasses is the number of card ecosystem classes.
	DumpEcosystemClasses = 10
	// LearnedEmbedDim is the 32D learned NFC tag fingerprint dimension.
	LearnedEmbedDim = 32
)

// InductiveNeuralResult holds the zero-alloc output of NFC-InductiveNet.
type InductiveNeuralResult struct {
	ClassID     int                       `json:"class_id"`
	Label       string                    `json:"label"`
	Confidence  float32                   `json:"confidence"`
	MoisturePct int                       `json:"moisture_pct"`
	Probs       [InductiveClasses]float32 `json:"probs"`
}

// DumpNeuralResult holds the zero-alloc output of NFC-DumpNet.
type DumpNeuralResult struct {
	ClassID    int                           `json:"class_id"`
	Label      string                        `json:"label"`
	Confidence float32                       `json:"confidence"`
	Embedding  [LearnedEmbedDim]float32      `json:"embedding"`
	Probs      [DumpEcosystemClasses]float32 `json:"probs"`
}

// NFCWorkspace holds reusable buffers for 0-alloc NFC-InductiveNet and NFC-DumpNet inference.
type NFCWorkspace struct {
	indIn  [4 * InductiveSeqLen]float32
	bufA   [64 * InductiveSeqLen]float32
	bufB   [64 * InductiveSeqLen]float32
	qBuf   [64 * InductiveSeqLen]int8
	pooled [128]float32
	fc     [96]float32
	fc2    [96]float32
	fc3    [64]float32
	dumpIn [DumpFeatureDim]float32
	logits [DumpEcosystemClasses]float32
	probs  [DumpEcosystemClasses]float32
	moist  [1]float32
	embRaw [LearnedEmbedDim]float32
}

// InferInductive runs NFC-InductiveNet on a 32-sample (dAmp, dPhase) ST25R3916 trajectory.
func (m *CZMLModel) InferInductive(ws *NFCWorkspace, dAmp, dPhase *[InductiveSeqLen]int16) InductiveNeuralResult {
	for t := range InductiveSeqLen {
		a, p := float32(dAmp[t]), float32(dPhase[t])
		da, dp := float32(0), float32(0)
		if t > 0 {
			da = a - float32(dAmp[t-1])
			dp = p - float32(dPhase[t-1])
		}
		ws.indIn[t] = clampSigned(a / 64.0)
		ws.indIn[InductiveSeqLen+t] = clampSigned(p / 64.0)
		ws.indIn[2*InductiveSeqLen+t] = clampSigned(da / 16.0)
		ws.indIn[3*InductiveSeqLen+t] = clampSigned(dp / 16.0)
	}
	seq := runNFCLayer1D(ws.bufA[:], ws.indIn[:], ws.qBuf[:], InductiveSeqLen, &m.Layers[0])
	seq = runNFCLayer1D(ws.bufB[:], ws.bufA[:], ws.qBuf[:], seq, &m.Layers[1])
	seq = runNFCLayer1D(ws.bufA[:], ws.bufB[:], ws.qBuf[:], seq, &m.Layers[2])
	seq = runNFCLayer1D(ws.bufB[:], ws.bufA[:], ws.qBuf[:], seq, &m.Layers[3])
	seq = runNFCLayer1D(ws.bufA[:], ws.bufB[:], ws.qBuf[:], seq, &m.Layers[4])

	poolAvgMaxNFC(ws.pooled[:], ws.bufA[:64*seq], 64, seq)
	runNFCLinear(ws.fc[:48], ws.pooled[:], ws.qBuf[:], &m.Layers[5])
	runNFCLinear(ws.logits[:InductiveClasses], ws.fc[:48], ws.qBuf[:], &m.Layers[6])
	runNFCLinear(ws.moist[:], ws.fc[:48], ws.qBuf[:], &m.Layers[7])

	bestIdx, bestProb := softmaxNFC(ws.probs[:InductiveClasses], ws.logits[:InductiveClasses], InductiveClasses, m.Temp)
	mPct := int(math.Round(100.0 / (1.0 + math.Exp(-float64(ws.moist[0])))))
	var outProbs [InductiveClasses]float32
	copy(outProbs[:], ws.probs[:InductiveClasses])
	return InductiveNeuralResult{
		ClassID:     bestIdx,
		Label:       m.Labels[bestIdx],
		Confidence:  bestProb,
		MoisturePct: min(100, max(0, mPct)),
		Probs:       outProbs,
	}
}

// InferDump runs NFC-DumpNet on a parsed NfcTag to classify ecosystem & compute 32D embedding.
func (m *CZMLModel) InferDump(ws *NFCWorkspace, tag *nfcfile.NfcTag) DumpNeuralResult {
	extractDumpFeatures(&ws.dumpIn, tag)
	runNFCLinear(ws.fc[:96], ws.dumpIn[:], ws.qBuf[:], &m.Layers[0])
	runNFCLinear(ws.fc2[:96], ws.fc[:96], ws.qBuf[:], &m.Layers[1])
	runNFCLinear(ws.fc3[:64], ws.fc2[:96], ws.qBuf[:], &m.Layers[2])
	runNFCLinear(ws.logits[:DumpEcosystemClasses], ws.fc3[:64], ws.qBuf[:], &m.Layers[3])
	runNFCLinear(ws.embRaw[:], ws.fc3[:64], ws.qBuf[:], &m.Layers[4])

	bestIdx, bestProb := softmaxNFC(ws.probs[:DumpEcosystemClasses], ws.logits[:DumpEcosystemClasses], DumpEcosystemClasses, m.Temp)
	var outProbs [DumpEcosystemClasses]float32
	copy(outProbs[:], ws.probs[:DumpEcosystemClasses])
	return DumpNeuralResult{
		ClassID:    bestIdx,
		Label:      m.Labels[bestIdx],
		Confidence: bestProb,
		Embedding:  normalize32(ws.embRaw[:]),
		Probs:      outProbs,
	}
}

func extractDumpFeatures(dst *[DumpFeatureDim]float32, tag *nfcfile.NfcTag) {
	clear(dst[:])
	if tag == nil {
		return
	}
	prof := AnalyzeDump(tag.Pages)
	dst[0] = clamp01(float32(len(tag.UID)) / 10.0)
	dst[1] = float32(tag.ATQA[0]) / 255.0
	dst[2] = float32(tag.ATQA[1]) / 255.0
	dst[3] = clamp01(float32(tag.SAK) / 64.0)
	dst[4] = clamp01(float32(len(tag.Pages)) / 256.0)
	dst[5] = prof.FillRatio
	dst[6] = clamp01(prof.EntropyBits / 8.0)
	dst[7] = prof.ASCIIRatio
	if prof.HasNDEF {
		dst[8] = 1.0
		if pagesContainFoldASCII(tag.Pages, "wfa.wsc") || pagesContainFoldASCII(tag.Pages, "vcard") {
			dst[9] = 1.0
		}
	}
	isDESFire := tag.SAK == 0x20 && tag.ATQA[0] == 0x44 && tag.ATQA[1] == 0x03
	if (tag.SAK == 0x20 && !isDESFire) || pagesContainFoldASCII(tag.Pages, "2pay.sys") {
		dst[10] = 1.0
	}
	if tag.SAK == 0x08 || tag.SAK == 0x18 {
		dst[11] = 1.0
	}
	if len(tag.Pages) >= 3 && (len(tag.Pages[2]) >= 4 && (tag.Pages[2][2] != 0 || tag.Pages[2][3] != 0)) {
		dst[12] = 0.85
	}
	switch tag.Protocol {
	case "NFC-B":
		dst[15] = 0.33
	case "NFC-F":
		dst[15] = 0.66
	case "NFC-V":
		dst[15] = 1.0
	}
	fillHistAndPageEntropy(dst, tag.Pages, prof.HasNDEF)
}

func fillHistAndPageEntropy(dst *[DumpFeatureDim]float32, pages [][]byte, hasNDEF bool) {
	var bins [16]int
	totalBytes, zeroPages, ffPages := 0, 0, 0
	for pIdx, pg := range pages {
		allZero, allFF := true, true
		var pHist [256]int
		for _, b := range pg {
			pHist[b]++
			if b != 0x00 {
				allZero = false
			}
			if b != 0xFF {
				allFF = false
			}
			if !hasNDEF || (b != 0x00 && b != 0xFF) {
				bins[b>>4]++
				totalBytes++
			}
		}
		if len(pg) == 0 {
			continue
		}
		if allZero {
			zeroPages++
		}
		if allFF {
			ffPages++
		}
		if slot := (pIdx * 16) / max(len(pages), 1); slot < 16 {
			if ent := clamp01(shannonEntropy(&pHist, len(pg)) / 8.0); ent > dst[32+slot] {
				dst[32+slot] = ent
			}
		}
	}
	if len(pages) > 0 && !hasNDEF {
		dst[13] = clamp01(float32(zeroPages) / float32(len(pages)))
		dst[14] = clamp01(float32(ffPages) / float32(len(pages)))
	}
	if totalBytes > 0 {
		for b := range 16 {
			dst[16+b] = clamp01(float32(bins[b]) / float32(totalBytes))
		}
	}
}

func pagesContainFoldASCII(pages [][]byte, sub string) bool {
	if len(sub) == 0 {
		return false
	}
	matched := 0
	for _, pg := range pages {
		for _, b := range pg {
			if b >= 'A' && b <= 'Z' {
				b += 'a' - 'A'
			}
			if b == sub[matched] {
				matched++
				if matched == len(sub) {
					return true
				}
			} else if b == sub[0] {
				matched = 1
			} else {
				matched = 0
			}
		}
	}
	return false
}
