package mcpserver

import (
	"context"
	"errors"
	"testing"

	"nfc-studio/internal/config"
	"nfc-studio/internal/st25r3916"
)

type fakeSensorEPaperBackend struct {
	fakeBackend
	amp         uint8
	phase       uint8
	fieldErr    error
	epaperCalls int
	epaperModel st25r3916.EPaperModel
	epaperLen   int
	epaperErr   error
}

func (f *fakeSensorEPaperBackend) MeasureField() (uint8, uint8, error) {
	if f.fieldErr != nil {
		return 0, 0, f.fieldErr
	}
	return f.amp, f.phase, nil
}

func (f *fakeSensorEPaperBackend) ProgramEPaper(_ context.Context, model st25r3916.EPaperModel, bitmap []byte, onProgress func(int)) error {
	f.epaperCalls++
	f.epaperModel = model
	f.epaperLen = len(bitmap)
	if f.epaperErr != nil {
		return f.epaperErr
	}
	if onProgress != nil {
		onProgress(50)
		onProgress(100)
	}
	return nil
}

func TestMCPInductiveSenseTool(t *testing.T) {
	t.Run("sim_mode_calibrate_and_sense", func(t *testing.T) {
		srv := New(nil, t.TempDir(), config.Default())

		res, out := callUpgradeTool(t, srv, "nfc_inductive_sense", map[string]any{
			"mode":      "metal",
			"calibrate": true,
		})
		if res.IsError || out["simulated"] != true || out["calibrated"] != true {
			t.Fatalf("calibrate metal failed: isErr=%v out=%+v", res.IsError, out)
		}

		res, out = callUpgradeTool(t, srv, "nfc_inductive_sense", map[string]any{
			"mode":    "metal",
			"samples": 3,
		})
		if res.IsError {
			t.Fatalf("metal sense error: %+v", out)
		}
		reading := out["reading"].(map[string]any)
		if reading["material_label"] != "FERROUS" {
			t.Fatalf("expected FERROUS in sim metal mode, got %+v", reading)
		}

		res, out = callUpgradeTool(t, srv, "nfc_inductive_sense", map[string]any{
			"mode":      "moisture",
			"calibrate": true,
		})
		if res.IsError {
			t.Fatalf("moisture calibrate failed: %+v", out)
		}
		res, out = callUpgradeTool(t, srv, "nfc_inductive_sense", map[string]any{
			"mode": "moisture",
		})
		if res.IsError || out["moisture_label"] != "DAMP" {
			t.Fatalf("expected DAMP in sim moisture mode, got isErr=%v out=%+v", res.IsError, out)
		}
	})

	t.Run("hardware_backend_and_validation", func(t *testing.T) {
		be := &fakeSensorEPaperBackend{amp: 150, phase: 102}
		srv := New(be, t.TempDir(), config.Default())

		res, out := callUpgradeTool(t, srv, "nfc_inductive_sense", map[string]any{"mode": "metal"})
		if res.IsError || out["simulated"] != false {
			t.Fatalf("hw sense failed: isErr=%v out=%+v", res.IsError, out)
		}
		reading := out["reading"].(map[string]any)
		if reading["material_label"] != "NON-FERROUS" {
			t.Fatalf("expected NON-FERROUS, got %+v", reading)
		}

		res, _ = callUpgradeTool(t, srv, "nfc_inductive_sense", map[string]any{"mode": "invalid"})
		if !res.IsError {
			t.Fatal("expected error for invalid mode")
		}
		res, _ = callUpgradeTool(t, srv, "nfc_inductive_sense", map[string]any{"samples": 99})
		if !res.IsError {
			t.Fatal("expected error for samples > 32")
		}

		be.fieldErr = errors.New("spi bus timeout")
		res, _ = callUpgradeTool(t, srv, "nfc_inductive_sense", map[string]any{"mode": "metal"})
		if !res.IsError {
			t.Fatal("expected error propagation from MeasureField")
		}
	})
}

func TestMCPEPaperPushTool(t *testing.T) {
	t.Run("safety_gates_and_sim_mode", func(t *testing.T) {
		srv := New(nil, t.TempDir(), config.Default())

		// Missing confirm_rf must fail.
		res, _ := callUpgradeTool(t, srv, "nfc_epaper_push", map[string]any{
			"model": "2.13",
			"title": "Badge",
		})
		if !res.IsError {
			t.Fatal("expected error when confirm_rf is omitted")
		}

		// Empty title must fail.
		res, _ = callUpgradeTool(t, srv, "nfc_epaper_push", map[string]any{
			"confirm_rf": true,
			"title":      "   ",
		})
		if !res.IsError {
			t.Fatal("expected error when title is blank")
		}

		// Unsupported model must fail.
		res, _ = callUpgradeTool(t, srv, "nfc_epaper_push", map[string]any{
			"confirm_rf": true,
			"model":      "7.5",
			"title":      "Badge",
		})
		if !res.IsError {
			t.Fatal("expected error for unsupported model")
		}

		// Valid SIM push succeeds.
		res, out := callUpgradeTool(t, srv, "nfc_epaper_push", map[string]any{
			"confirm_rf": true,
			"model":      "1.54",
			"title":      "ZHENYA",
			"body":       "CardputerZero Lab",
		})
		if res.IsError || out["sim_mode"] != true || out["bitmap_bytes"] != float64(5000) || out["status"] != "simulated" {
			t.Fatalf("unexpected sim push result: isErr=%v out=%+v", res.IsError, out)
		}
	})

	t.Run("hardware_backend_and_sim_override", func(t *testing.T) {
		be := &fakeSensorEPaperBackend{}
		srv := New(be, t.TempDir(), config.Default())

		// sim_mode=true skips hardware even when backend is attached.
		res, out := callUpgradeTool(t, srv, "nfc_epaper_push", map[string]any{
			"confirm_rf": true,
			"model":      "2.90",
			"title":      "DRY RUN",
			"sim_mode":   true,
		})
		if res.IsError || out["sim_mode"] != true || be.epaperCalls != 0 {
			t.Fatalf("sim_mode override failed: isErr=%v out=%+v calls=%d", res.IsError, out, be.epaperCalls)
		}

		// Real hardware upload.
		res, out = callUpgradeTool(t, srv, "nfc_epaper_push", map[string]any{
			"confirm_rf": true,
			"model":      "2.13",
			"title":      "LIVE",
			"body":       "250x122",
		})
		if res.IsError || out["sim_mode"] != false || out["status"] != "programmed" || be.epaperCalls != 1 || be.epaperLen != 3904 {
			t.Fatalf("hw push failed: isErr=%v out=%+v calls=%d len=%d", res.IsError, out, be.epaperCalls, be.epaperLen)
		}

		be.epaperErr = st25r3916.ErrNotISODEP
		res, _ = callUpgradeTool(t, srv, "nfc_epaper_push", map[string]any{
			"confirm_rf": true,
			"title":      "FAIL",
		})
		if !res.IsError {
			t.Fatal("expected error when ProgramEPaper fails")
		}
	})
}
