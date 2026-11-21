package st25r3916

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

func TestParseEPaperModelAndDimensions(t *testing.T) {
	tests := []struct {
		raw        string
		wantModel  EPaperModel
		wantW      int
		wantH      int
		wantBytes  int
		wantErr    bool
	}{
		{raw: "", wantModel: EPaper213, wantW: 250, wantH: 122, wantBytes: 3904},
		{raw: "2.13", wantModel: EPaper213, wantW: 250, wantH: 122, wantBytes: 3904},
		{raw: "2.90", wantModel: EPaper290, wantW: 296, wantH: 128, wantBytes: 4736},
		{raw: "1.54", wantModel: EPaper154, wantW: 200, wantH: 200, wantBytes: 5000},
		{raw: "7.5", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.raw, func(t *testing.T) {
			m, err := ParseEPaperModel(tc.raw)
			if tc.wantErr {
				if !errors.Is(err, ErrInvalidEPaperModel) {
					t.Fatalf("ParseEPaperModel(%q) err = %v, want ErrInvalidEPaperModel", tc.raw, err)
				}
				return
			}
			if err != nil || m != tc.wantModel {
				t.Fatalf("ParseEPaperModel(%q) = (%q, %v), want %q", tc.raw, m, err, tc.wantModel)
			}
			w, h, n, err := EPaperDimensions(m)
			if err != nil || w != tc.wantW || h != tc.wantH || n != tc.wantBytes {
				t.Fatalf("EPaperDimensions(%q) = (%d,%d,%d,%v)", m, w, h, n, err)
			}
		})
	}
}

func TestRenderTextBitmap(t *testing.T) {
	for _, m := range []EPaperModel{EPaper213, EPaper290, EPaper154} {
		_, _, wantLen, err := EPaperDimensions(m)
		if err != nil {
			t.Fatalf("EPaperDimensions(%q) err = %v", m, err)
		}
		bmp := RenderTextBitmap(m, "CardputerZero", "NFC e-Paper\n13.56 MHz Badge!")
		if len(bmp) != wantLen {
			t.Fatalf("RenderTextBitmap(%q) len = %d, want %d", m, len(bmp), wantLen)
		}
		blackCount := 0
		for _, b := range bmp {
			if b != 0xFF {
				blackCount++
			}
		}
		if blackCount == 0 {
			t.Fatalf("RenderTextBitmap(%q) produced an all-white bitmap", m)
		}
	}
	if got := RenderTextBitmap(EPaperModel("9.99"), "hi", "there"); got != nil {
		t.Fatalf("expected nil bitmap for unsupported model, got len=%d", len(got))
	}
}

func TestBuildWaveshareFramesAndReassembly(t *testing.T) {
	bmp := RenderTextBitmap(EPaper154, "TEST", "LINE1\nLINE2")
	frames := BuildWaveshareFrames(EPaper154, bmp)
	if len(frames) < 8 {
		t.Fatalf("expected >= 8 frames, got %d", len(frames))
	}
	wantPrefix := []uint8{0x00, 0x01, 0x02, 0x03, 0x04, 0x05}
	for i, step := range wantPrefix {
		if frames[i][0] != epaperCLA || frames[i][1] != step {
			t.Fatalf("frame[%d] = %X, want CLA=0xCD step=0x%02X", i, frames[i], step)
		}
	}
	last := frames[len(frames)-1]
	if last[0] != epaperCLA || last[1] != epaperStepRefr {
		t.Fatalf("last frame = %X, want refresh 0x18", last)
	}
	var reassembled []byte
	for _, f := range frames[6 : len(frames)-1] {
		if f[0] != epaperCLA || f[1] != epaperStepChunk {
			t.Fatalf("unexpected chunk frame header: %X", f[:5])
		}
		reassembled = append(reassembled, f[5:]...)
	}
	if !bytes.Equal(reassembled, bmp) {
		t.Fatalf("reassembled bitmap (%dB) != original (%dB)", len(reassembled), len(bmp))
	}
	if got := BuildWaveshareFrames(EPaper213, nil); got != nil {
		t.Fatalf("expected nil for empty bitmap, got %d frames", len(got))
	}
	if got := BuildWaveshareFrames(EPaperModel("bad"), []byte{1}); got != nil {
		t.Fatalf("expected nil for bad model, got %d frames", len(got))
	}
}

func makeEPaperDiscoveryEvents(sak uint8, extra []rxEvent) []rxEvent {
	u0, u1, u2, u3 := byte(0xAA), byte(0xBB), byte(0xCC), byte(0xDD)
	bcc := u0 ^ u1 ^ u2 ^ u3
	evts := []rxEvent{
		{mainIRQ: irqMainRXE, fifo: []byte{0x04, 0x00}},                // WUPA -> ATQA
		{mainIRQ: irqMainRXE, fifo: []byte{u0, u1, u2, u3, bcc}},       // CL1 anticollision
		{mainIRQ: irqMainRXE, fifo: []byte{sak, 0xAA, 0xBB}},           // SELECT -> SAK + CRC
	}
	return append(evts, extra...)
}

func TestProgramEPaperSuccessAndErrors(t *testing.T) {
	t.Run("full_upload_with_wtx_and_progress", func(t *testing.T) {
		bmp := RenderTextBitmap(EPaper213, "HI", "OK")
		frames := BuildWaveshareFrames(EPaper213, bmp)
		extra := make([]rxEvent, 0, len(frames)+2)
		extra = append(extra, rxEvent{mainIRQ: irqMainRXE, fifo: []byte{0x05, 0x78, 0x80, 0x70, 0x02, 0xAA, 0xBB}}) // RATS -> ATS
		// Inject one S(WTX) on first APDU to verify WTX handling.
		extra = append(extra, rxEvent{mainIRQ: irqMainRXE, fifo: []byte{pcbWTX, 0x01, 0xAA, 0xBB}})
		for i := range frames {
			pcb := pcbIBlockBase | uint8(i&1)
			extra = append(extra, rxEvent{mainIRQ: irqMainRXE, fifo: []byte{pcb, 0x90, 0x00, 0xAA, 0xBB}})
		}
		chip, _ := newTestChipWithEvents(t, makeEPaperDiscoveryEvents(0x20, extra))
		var progress []int
		err := chip.ProgramEPaper(context.Background(), EPaper213, bmp, func(pct int) {
			progress = append(progress, pct)
		})
		if err != nil {
			t.Fatalf("ProgramEPaper() error = %v", err)
		}
		if len(progress) != len(frames) || progress[len(progress)-1] != 100 {
			t.Fatalf("unexpected progress callbacks: len=%d last=%v", len(progress), progress)
		}
	})

	t.Run("rejects_non_isodep_sak", func(t *testing.T) {
		bmp := RenderTextBitmap(EPaper213, "HI", "")
		chip, _ := newTestChipWithEvents(t, makeEPaperDiscoveryEvents(0x00, nil))
		err := chip.ProgramEPaper(context.Background(), EPaper213, bmp, nil)
		if !errors.Is(err, ErrNotISODEP) {
			t.Fatalf("err = %v, want ErrNotISODEP", err)
		}
	})

	t.Run("rejects_bad_bitmap_and_model", func(t *testing.T) {
		chip, _ := newTestChipWithEvents(t, nil)
		if err := chip.ProgramEPaper(context.Background(), EPaperModel("bad"), []byte{1}, nil); !errors.Is(err, ErrInvalidEPaperModel) {
			t.Fatalf("err = %v, want ErrInvalidEPaperModel", err)
		}
		if err := chip.ProgramEPaper(context.Background(), EPaper213, []byte{1, 2, 3}, nil); !errors.Is(err, ErrInvalidBitmapSize) {
			t.Fatalf("err = %v, want ErrInvalidBitmapSize", err)
		}
	})

	t.Run("rejects_non_9000_status_and_cancelled_ctx", func(t *testing.T) {
		bmp := RenderTextBitmap(EPaper213, "HI", "")
		extra := []rxEvent{
			{mainIRQ: irqMainRXE, fifo: []byte{0x05, 0x78, 0x80, 0x70, 0x02, 0xAA, 0xBB}}, // RATS
			{mainIRQ: irqMainRXE, fifo: []byte{pcbIBlockBase, 0x6A, 0x82, 0xAA, 0xBB}},    // SW=6A82 error
		}
		chip, _ := newTestChipWithEvents(t, makeEPaperDiscoveryEvents(0x20, extra))
		if err := chip.ProgramEPaper(context.Background(), EPaper213, bmp, nil); !errors.Is(err, ErrEPaperStatus) {
			t.Fatalf("err = %v, want ErrEPaperStatus", err)
		}

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := chip.ProgramEPaper(ctx, EPaper213, bmp, nil); !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
	})
}

func FuzzRenderTextBitmap(f *testing.F) {
	f.Add("Cardputer", "Line 1\nLine 2\x00\xff")
	f.Fuzz(func(t *testing.T, title, body string) {
		bmp := RenderTextBitmap(EPaper154, title, body)
		if len(bmp) != 5000 {
			t.Fatalf("unexpected bitmap len=%d", len(bmp))
		}
	})
}
