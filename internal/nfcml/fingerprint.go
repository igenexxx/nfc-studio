package nfcml

import (
	"cmp"
	"math"
	"slices"

	"nfc-studio/internal/nfcfile"
)

// Embedding is an L2-normalized 32-dimensional learned NFC tag signature (NFC-DumpNet).
type Embedding [LearnedEmbedDim]float32

// FingerprintEntry holds a precomputed tag embedding for cosine k-NN matching.
type FingerprintEntry struct {
	Name       string
	Path       string
	DeviceType string
	UIDLen     int
	Embedding  Embedding
}

// MatchResult describes a matched NFC tag and its cosine similarity [0..1].
type MatchResult struct {
	Name       string  `json:"name"`
	Path       string  `json:"path"`
	DeviceType string  `json:"device_type"`
	Similarity float32 `json:"similarity"`
}

// FingerprintIndex stores tag embeddings in memory for fast cosine k-NN search.
type FingerprintIndex struct {
	entries []FingerprintEntry
}

// NewFingerprintIndex constructs a FingerprintIndex from precomputed entries.
func NewFingerprintIndex(entries []FingerprintEntry) *FingerprintIndex {
	return &FingerprintIndex{entries: entries}
}

// Count returns the number of indexed tags.
func (idx *FingerprintIndex) Count() int {
	if idx == nil {
		return 0
	}
	return len(idx.entries)
}

// ComputeEmbedding extracts an L2-normalized 32D feature vector from an NfcTag using NFC-DumpNet
// (falling back to handcrafted structural normalization only if no model is loaded).
func ComputeEmbedding(tag *nfcfile.NfcTag) Embedding {
	if tag == nil {
		return Embedding{}
	}
	if res, ok := ClassifyTagDump(tag); ok {
		return Embedding(res.Embedding)
	}
	prof := AnalyzeDump(tag.Pages)
	var v Embedding
	v[0] = float32(len(tag.UID)) / 10.0
	v[1] = float32(tag.ATQA[0]) / 255.0
	v[2] = float32(tag.ATQA[1]) / 255.0
	v[3] = float32(tag.SAK) / 64.0
	v[4] = min(float32(1.0), float32(len(tag.Pages))/256.0)
	v[5] = prof.FillRatio
	v[6] = prof.EntropyBits / 8.0
	if prof.HasNDEF {
		v[7] = 0.6 + prof.ASCIIRatio*0.4
	} else {
		v[7] = prof.ASCIIRatio * 0.4
	}
	return normalizeEmbedding(v)
}

func normalizeEmbedding(v Embedding) Embedding {
	var sumSq float64
	for _, x := range v {
		sumSq += float64(x) * float64(x)
	}
	if sumSq <= 1e-12 {
		return v
	}
	inv := float32(1.0 / math.Sqrt(sumSq))
	for i := range v {
		v[i] *= inv
	}
	return v
}

// CosineSimilarity returns the dot product of two L2-normalized Embeddings.
func CosineSimilarity(a, b Embedding) float32 {
	var dot float32
	for i := range a {
		dot += a[i] * b[i]
	}
	return dot
}

// Match returns the single closest indexed tag with matching UID length and similarity >= minSim.
func (idx *FingerprintIndex) Match(query Embedding, uidLen int, minSim float32) (MatchResult, bool) {
	if idx == nil || len(idx.entries) == 0 {
		return MatchResult{}, false
	}
	bestSim := float32(-1)
	bestIdx := -1
	for i := range idx.entries {
		e := &idx.entries[i]
		if uidLen > 0 && e.UIDLen > 0 && e.UIDLen != uidLen {
			continue
		}
		if sim := CosineSimilarity(query, e.Embedding); sim >= minSim && sim > bestSim {
			bestSim = sim
			bestIdx = i
		}
	}
	if bestIdx < 0 {
		return MatchResult{}, false
	}
	e := idx.entries[bestIdx]
	return MatchResult{Name: e.Name, Path: e.Path, DeviceType: e.DeviceType, Similarity: bestSim}, true
}

// TopK returns up to k closest matches sorted descending by cosine similarity.
func (idx *FingerprintIndex) TopK(query Embedding, uidLen int, minSim float32, k int) []MatchResult {
	if idx == nil || len(idx.entries) == 0 || k <= 0 {
		return nil
	}
	out := make([]MatchResult, 0, min(k, len(idx.entries)))
	for i := range idx.entries {
		e := &idx.entries[i]
		if uidLen > 0 && e.UIDLen > 0 && e.UIDLen != uidLen {
			continue
		}
		if sim := CosineSimilarity(query, e.Embedding); sim >= minSim {
			out = append(out, MatchResult{Name: e.Name, Path: e.Path, DeviceType: e.DeviceType, Similarity: sim})
		}
	}
	slices.SortFunc(out, func(a, b MatchResult) int { return cmp.Compare(b.Similarity, a.Similarity) })
	if len(out) > k {
		out = out[:k]
	}
	return out
}
