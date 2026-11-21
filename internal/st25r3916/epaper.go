package st25r3916

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// EPaperModel identifies a supported battery-free Waveshare NFC e-Paper display.
type EPaperModel string

const (
	EPaper213 EPaperModel = "2.13" // 250x122 monochrome
	EPaper290 EPaperModel = "2.90" // 296x128 monochrome
	EPaper154 EPaperModel = "1.54" // 200x200 monochrome
)

var (
	// ErrInvalidEPaperModel is returned for an unsupported Waveshare e-Paper model.
	ErrInvalidEPaperModel = errors.New("st25r3916: unsupported e-paper model")
	// ErrInvalidBitmapSize is returned when a bitmap does not match model dimensions.
	ErrInvalidBitmapSize = errors.New("st25r3916: invalid e-paper bitmap size")
	// ErrEPaperStatus is returned when the e-Paper controller rejects an APDU frame.
	ErrEPaperStatus = errors.New("st25r3916: e-paper APDU non-9000 status")
)

const (
	epaperCLA        uint8  = 0xCD
	epaperStepInit   uint8  = 0x00
	epaperStepNormal uint8  = 0x01
	epaperStepConfig uint8  = 0x02
	epaperStepPwrOn  uint8  = 0x03
	epaperStepCfg2   uint8  = 0x04
	epaperStepData   uint8  = 0x05
	epaperStepChunk  uint8  = 0x06
	epaperStepRefr   uint8  = 0x18
	epaperChunkSize         = 100
	epaperTimeoutMs  uint32 = 120
	epaperWait              = 180 * time.Millisecond
)

type epaperSpec struct {
	width  int
	height int
	code   uint8
}

func lookupEPaperSpec(model EPaperModel) (epaperSpec, bool) {
	switch model {
	case EPaper213:
		return epaperSpec{width: 250, height: 122, code: 0x01}, true
	case EPaper290:
		return epaperSpec{width: 296, height: 128, code: 0x02}, true
	case EPaper154:
		return epaperSpec{width: 200, height: 200, code: 0x03}, true
	default:
		return epaperSpec{}, false
	}
}

// ParseEPaperModel normalizes a model string ("2.13", "2.90", "1.54") into EPaperModel.
func ParseEPaperModel(raw string) (EPaperModel, error) {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case "", "2.13", "213":
		return EPaper213, nil
	case "2.90", "2.9", "290":
		return EPaper290, nil
	case "1.54", "154":
		return EPaper154, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrInvalidEPaperModel, raw)
	}
}

// EPaperDimensions returns pixel width, height, and packed 1-bit byte length for model.
func EPaperDimensions(model EPaperModel) (width, height, byteLen int, err error) {
	spec, ok := lookupEPaperSpec(model)
	if !ok {
		return 0, 0, 0, fmt.Errorf("%w: %q", ErrInvalidEPaperModel, model)
	}
	return spec.width, spec.height, ((spec.width + 7) / 8) * spec.height, nil
}

// RenderTextBitmap renders a framed 1-bit monochrome badge (0xFF=white, 0=black ink)
// using the built-in 5x7 ASCII font. Returns nil for an unsupported model.
func RenderTextBitmap(model EPaperModel, title, body string) []byte {
	spec, ok := lookupEPaperSpec(model)
	if !ok {
		return nil
	}
	rowBytes := (spec.width + 7) / 8
	buf := make([]byte, rowBytes*spec.height)
	for i := range buf {
		buf[i] = 0xFF
	}
	drawRectBorder(buf, rowBytes, spec.width, spec.height)
	drawBitmapText(buf, rowBytes, spec.width, spec.height, 8, 8, 2, title)
	drawHLine(buf, rowBytes, spec.width, spec.height, 4, spec.width-5, 28)
	drawWrappedBody(buf, rowBytes, spec.width, spec.height, 8, 36, body)
	return buf
}

func setBlackPixel(buf []byte, rowBytes, w, h, x, y int) {
	if x < 0 || y < 0 || x >= w || y >= h {
		return
	}
	buf[y*rowBytes+x/8] &^= 1 << (7 - uint(x%8))
}

func drawHLine(buf []byte, rowBytes, w, h, x0, x1, y int) {
	for x := x0; x <= x1; x++ {
		setBlackPixel(buf, rowBytes, w, h, x, y)
	}
}

func drawRectBorder(buf []byte, rowBytes, w, h int) {
	for x := 2; x < w-2; x++ {
		setBlackPixel(buf, rowBytes, w, h, x, 2)
		setBlackPixel(buf, rowBytes, w, h, x, h-3)
	}
	for y := 2; y < h-2; y++ {
		setBlackPixel(buf, rowBytes, w, h, 2, y)
		setBlackPixel(buf, rowBytes, w, h, w-3, y)
	}
}

func drawBitmapText(buf []byte, rowBytes, w, h, x, y, scale int, text string) {
	cx := x
	step := 6 * scale
	for _, r := range text {
		if cx+5*scale >= w-4 {
			break
		}
		g := glyph5x7(r)
		for col := range 5 {
			bits := g[col]
			for row := range 7 {
				if (bits>>uint(row))&1 == 0 {
					continue
				}
				for sy := range scale {
					for sx := range scale {
						setBlackPixel(buf, rowBytes, w, h, cx+col*scale+sx, y+row*scale+sy)
					}
				}
			}
		}
		cx += step
	}
}

func drawWrappedBody(buf []byte, rowBytes, w, h, startX, startY int, body string) {
	maxChars := max(1, (w-startX-8)/6)
	y := startY
	for line := range strings.SplitSeq(body, "\n") {
		runes := []rune(line)
		for len(runes) > 0 && y+7 < h-4 {
			n := min(len(runes), maxChars)
			drawBitmapText(buf, rowBytes, w, h, startX, y, 1, string(runes[:n]))
			runes = runes[n:]
			y += 10
		}
	}
}

// BuildWaveshareFrames builds the Waveshare 0xCD APDU command sequence for a packed bitmap.
// Returns nil when model is unsupported or bitmap is empty.
func BuildWaveshareFrames(model EPaperModel, bitmap []byte) [][]byte {
	spec, ok := lookupEPaperSpec(model)
	if !ok || len(bitmap) == 0 {
		return nil
	}
	numChunks := (len(bitmap) + epaperChunkSize - 1) / epaperChunkSize
	frames := make([][]byte, 0, 7+numChunks)
	frames = append(frames,
		[]byte{epaperCLA, epaperStepInit, 0x00, 0x00, 0x00},
		[]byte{epaperCLA, epaperStepNormal, 0x00, 0x00, 0x00},
		[]byte{epaperCLA, epaperStepConfig, 0x00, 0x00, 0x01, spec.code},
		[]byte{epaperCLA, epaperStepPwrOn, 0x00, 0x00, 0x00},
		[]byte{epaperCLA, epaperStepCfg2, 0x00, 0x00, 0x04,
			uint8(spec.width >> 8), uint8(spec.width), uint8(spec.height >> 8), uint8(spec.height)},
		[]byte{epaperCLA, epaperStepData, 0x00, 0x00, 0x00},
	)
	for i := range numChunks {
		start := i * epaperChunkSize
		end := min(len(bitmap), start+epaperChunkSize)
		chunk := bitmap[start:end]
		frame := make([]byte, 5+len(chunk))
		frame[0] = epaperCLA
		frame[1] = epaperStepChunk
		frame[2] = uint8(i >> 8)
		frame[3] = uint8(i & 0xFF)
		frame[4] = uint8(len(chunk))
		copy(frame[5:], chunk)
		frames = append(frames, frame)
	}
	frames = append(frames, []byte{epaperCLA, epaperStepRefr, 0x00, 0x00, 0x00})
	return frames
}

// ProgramEPaper powers an ISO 14443-A Waveshare NFC e-Paper tag, activates ISO-DEP via RATS,
// uploads all 0xCD bitmap frames with progress reporting, and always turns off the RF field.
func (c *Chip) ProgramEPaper(ctx context.Context, model EPaperModel, bitmap []byte, onProgress func(int)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, _, wantLen, err := EPaperDimensions(model)
	if err != nil {
		return err
	}
	if len(bitmap) != wantLen {
		return fmt.Errorf("%w: got %d bytes, want %d for model %s", ErrInvalidBitmapSize, len(bitmap), wantLen, model)
	}
	frames := BuildWaveshareFrames(model, bitmap)

	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.resetAndEnableField(); err != nil {
		return err
	}
	defer func() { _ = c.fieldOffLocked() }()

	if err := c.activateEPaperISODEPLocked(); err != nil {
		return err
	}
	return c.sendEPaperFramesLocked(ctx, frames, onProgress)
}

func (c *Chip) activateEPaperISODEPLocked() error {
	if _, err := c.sendWUPA(); err != nil {
		return err
	}
	_, sak, err := c.runAnticollisionCascade()
	if err != nil {
		return err
	}
	if (sak & sakISODEPBit) == 0 {
		return fmt.Errorf("%w: SAK=0x%02X", ErrNotISODEP, sak)
	}
	_, err = c.sendRATSLocked()
	return err
}

func (c *Chip) sendEPaperFramesLocked(ctx context.Context, frames [][]byte, onProgress func(int)) error {
	var blockNum uint8
	total := len(frames)
	for i, frame := range frames {
		if err := ctx.Err(); err != nil {
			return err
		}
		resp, err := c.transceiveRawISODEPLocked(ctx, frame, &blockNum)
		if err != nil {
			return fmt.Errorf("st25r3916: e-paper frame %d/%d: %w", i+1, total, err)
		}
		if !isSW9000(resp) {
			return fmt.Errorf("%w: frame %d/%d resp=%X", ErrEPaperStatus, i+1, total, resp)
		}
		if onProgress != nil {
			onProgress((i + 1) * 100 / total)
		}
	}
	return nil
}

func (c *Chip) transceiveRawISODEPLocked(ctx context.Context, apdu []byte, blockNum *uint8) ([]byte, error) {
	pcb := pcbIBlockBase | (*blockNum & 0x01)
	frame := make([]byte, 1+len(apdu))
	frame[0] = pcb
	copy(frame[1:], apdu)
	*blockNum ^= 1

	rx, err := c.txISO14443AWithCRCLocked(frame, epaperTimeoutMs, epaperWait)
	if err != nil {
		return nil, err
	}
	for range maxWTXExtensions {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		rx = stripTrailingCRC(rx)
		if len(rx) >= 2 && rx[0] == pcbWTX {
			rx, err = c.txISO14443AWithCRCLocked([]byte{pcbWTX, rx[1]}, epaperTimeoutMs, epaperWait)
			if err != nil {
				return nil, err
			}
			continue
		}
		if len(rx) < 1 {
			return nil, fmt.Errorf("%w: empty ISO-DEP response", ErrProtocol)
		}
		return rx[1:], nil
	}
	return nil, fmt.Errorf("%w: too many WTX frames", ErrProtocol)
}
