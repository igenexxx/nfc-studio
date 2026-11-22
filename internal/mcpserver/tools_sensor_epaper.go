package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"nfc-studio/internal/nfcml"
	"nfc-studio/internal/st25r3916"
)

// EPaperProgrammer is an optional capability interface for Waveshare NFC e-Paper uploads.
type EPaperProgrammer interface {
	ProgramEPaper(ctx context.Context, model st25r3916.EPaperModel, bitmap []byte, onProgress func(int)) error
}

func (s *Server) toolInductiveSense(raw json.RawMessage) (toolResult, *rpcError) {
	var in struct {
		Mode      string `json:"mode"`
		Calibrate bool   `json:"calibrate"`
		Samples   int    `json:"samples"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return toolResult{}, &rpcError{Code: errCodeInvalidParams, Message: "Invalid arguments: " + err.Error()}
	}
	mode := strings.ToLower(strings.TrimSpace(in.Mode))
	if mode == "" {
		mode = "metal"
	}
	if mode != "metal" && mode != "moisture" {
		return errResult(fmt.Sprintf("invalid mode %q: expected metal or moisture", in.Mode)), nil
	}
	samples := in.Samples
	if samples == 0 {
		samples = 1
	}
	if samples < 1 || samples > 32 {
		return errResult("samples must be between 1 and 32"), nil
	}

	s.hwMu.Lock()
	defer s.hwMu.Unlock()
	if s.inductive == nil {
		s.inductive = nfcml.NewInductiveSensor()
	}

	reading, simulated, err := s.runInductiveSamplingLocked(mode, in.Calibrate, samples)
	if err != nil {
		return errResult("inductive sense failed: " + err.Error()), nil
	}
	var spark [32]int16
	n := s.inductive.Sparkline(spark[:])
	airA, airP, airCal := s.inductive.AirBaseline()
	dryA, dryP, dryCal := s.inductive.DryBaseline()

	resp := map[string]any{
		"mode":           mode,
		"simulated":      simulated,
		"calibrated":     in.Calibrate,
		"reading":        reading,
		"moisture_label": reading.MoistureLevel.String(),
		"sparkline":      spark[:n],
		"air_baseline":   map[string]any{"amp": airA, "phase": airP, "calibrated": airCal},
		"dry_baseline":   map[string]any{"amp": dryA, "phase": dryP, "calibrated": dryCal},
	}
	if nn, ok := s.inductive.NeuralReading(); ok {
		resp["neural"] = nn
	}
	return okJSON(resp), nil
}

func (s *Server) runInductiveSamplingLocked(mode string, calibrate bool, samples int) (nfcml.InductiveReading, bool, error) {
	fm, hasHW := s.backend.(interface{ MeasureField() (uint8, uint8, error) })
	hasHW = hasHW && s.backend != nil

	readADC := func() (uint8, uint8, error) {
		if hasHW {
			return fm.MeasureField()
		}
		if calibrate {
			return 180, 120, nil
		}
		if mode == "moisture" {
			return 176, 127, nil
		}
		return 140, 138, nil
	}

	var last nfcml.InductiveReading
	for i := range samples {
		amp, phase, err := readADC()
		if err != nil {
			return nfcml.InductiveReading{}, !hasHW, err
		}
		if calibrate && i == 0 {
			if mode == "moisture" {
				s.inductive.CalibrateDry(amp, phase)
			} else {
				s.inductive.CalibrateAir(amp, phase)
			}
		}
		last = s.inductive.Sample(amp, phase)
	}
	return last, !hasHW, nil
}

func (s *Server) toolEPaperPush(ctx context.Context, raw json.RawMessage) (toolResult, *rpcError) {
	var in struct {
		ConfirmRF bool   `json:"confirm_rf"`
		Model     string `json:"model"`
		Title     string `json:"title"`
		Body      string `json:"body"`
		SimMode   bool   `json:"sim_mode"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return toolResult{}, &rpcError{Code: errCodeInvalidParams, Message: "Invalid arguments: " + err.Error()}
	}
	if !in.ConfirmRF {
		return errResult("confirm_rf must be true to transmit RF energy and program NFC e-Paper"), nil
	}
	if strings.TrimSpace(in.Title) == "" {
		return errResult("title must not be empty"), nil
	}
	model, err := st25r3916.ParseEPaperModel(in.Model)
	if err != nil {
		return errResult(err.Error()), nil
	}
	w, h, _, err := st25r3916.EPaperDimensions(model)
	if err != nil {
		return errResult(err.Error()), nil
	}
	bmp := st25r3916.RenderTextBitmap(model, in.Title, in.Body)
	frames := st25r3916.BuildWaveshareFrames(model, bmp)

	if s.backend == nil || in.SimMode {
		return okJSON(map[string]any{
			"sim_mode":     true,
			"model":        string(model),
			"width":        w,
			"height":       h,
			"bitmap_bytes": len(bmp),
			"frames":       len(frames),
			"progress_pct": 100,
			"status":       "simulated",
		}), nil
	}

	prog, ok := s.backend.(EPaperProgrammer)
	if !ok {
		return errResult("active backend does not support Waveshare NFC e-Paper programming"), nil
	}
	lastPct := 0
	s.hwMu.Lock()
	err = prog.ProgramEPaper(ctx, model, bmp, func(pct int) { lastPct = pct })
	s.hwMu.Unlock()
	if err != nil {
		return errResult("e-paper programming failed: " + err.Error()), nil
	}
	return okJSON(map[string]any{
		"sim_mode":     false,
		"model":        string(model),
		"width":        w,
		"height":       h,
		"bitmap_bytes": len(bmp),
		"frames":       len(frames),
		"progress_pct": lastPct,
		"status":       "programmed",
	}), nil
}
