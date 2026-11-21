package mcpserver

import (
	"encoding/json"

	"nfc-studio/internal/nfcfile"
	"nfc-studio/internal/nfcml"
)

func (s *Server) toolAnalyzeTag(raw json.RawMessage) (toolResult, *rpcError) {
	var in struct {
		Name     string   `json:"name"`
		PagesHex []string `json:"pages_hex"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return toolResult{}, &rpcError{Code: errCodeInvalidParams, Message: "Invalid arguments: " + err.Error()}
	}
	var pages [][]byte
	var tag *nfcfile.NfcTag
	if in.Name != "" {
		safe, err := sanitizeFilename(in.Name)
		if err != nil {
			return errResult(err.Error()), nil
		}
		_, data, ok := s.findSavedTagFile(safe)
		if !ok {
			return errResult("saved tag not found: " + safe), nil
		}
		t, err := nfcfile.Parse(string(data))
		if err != nil {
			return errResult("parse .nfc: " + err.Error()), nil
		}
		tag = t
		pages = t.Pages
	} else {
		p, err := parsePagesHex(in.PagesHex)
		if err != nil {
			return errResult(err.Error()), nil
		}
		pages = p
		tag = &nfcfile.NfcTag{Pages: pages}
	}
	prof := nfcml.AnalyzeDump(pages)
	emb := nfcml.ComputeEmbedding(tag)
	return okJSON(map[string]any{
		"profile":   prof,
		"embedding": emb,
	}), nil
}

func (s *Server) toolMatchFingerprint(raw json.RawMessage) (toolResult, *rpcError) {
	var in struct {
		UIDHex   string   `json:"uid_hex"`
		ATQAHex  string   `json:"atqa_hex"`
		SAKHex   string   `json:"sak_hex"`
		PagesHex []string `json:"pages_hex"`
		MinSim   float32  `json:"min_similarity"`
		TopK     int      `json:"top_k"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return toolResult{}, &rpcError{Code: errCodeInvalidParams, Message: "Invalid arguments: " + err.Error()}
	}
	uid, atqa, sak, err := parseTagIdent(in.UIDHex, in.ATQAHex, in.SAKHex, false)
	if err != nil {
		return errResult(err.Error()), nil
	}
	var pages [][]byte
	if len(in.PagesHex) > 0 {
		if pages, err = parsePagesHex(in.PagesHex); err != nil {
			return errResult(err.Error()), nil
		}
	}
	queryTag := &nfcfile.NfcTag{UID: uid, ATQA: atqa, SAK: sak, Pages: pages}
	queryEmb := nfcml.ComputeEmbedding(queryTag)

	userEntries := s.store.ListUserEntries()
	fpEntries := make([]nfcml.FingerprintEntry, 0, len(userEntries))
	for _, ref := range userEntries {
		if ref.Tag == nil {
			continue
		}
		fpEntries = append(fpEntries, nfcml.FingerprintEntry{
			Name: ref.Name, Path: ref.Path, DeviceType: ref.Tag.DeviceType,
			UIDLen: len(ref.Tag.UID), Embedding: nfcml.ComputeEmbedding(ref.Tag),
		})
	}
	minSim := in.MinSim
	if minSim <= 0 {
		minSim = 0.85
	}
	topK := in.TopK
	if topK <= 0 {
		topK = 3
	}
	matches := nfcml.NewFingerprintIndex(fpEntries).TopK(queryEmb, len(uid), minSim, topK)
	return okJSON(map[string]any{
		"indexed": len(fpEntries),
		"matches": matches,
	}), nil
}

func (s *Server) toolMeasureField() toolResult {
	fb := nfcml.NewFieldBaseline()
	// Warmup baseline + live reading.
	var last nfcml.FieldReading
	simulated := true
	if fm, ok := s.backend.(interface{ MeasureField() (uint8, uint8, error) }); ok && s.backend != nil {
		simulated = false
		s.hwMu.Lock()
		for range 8 {
			if amp, phase, err := fm.MeasureField(); err == nil {
				last = fb.Update(amp, phase)
			}
		}
		s.hwMu.Unlock()
	} else {
		for range 7 {
			fb.Update(180, 120)
		}
		last = fb.Update(166, 122)
	}
	return okJSON(map[string]any{
		"simulated": simulated,
		"reading":   last,
	})
}
