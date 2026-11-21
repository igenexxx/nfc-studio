# Spec 04: ST25R3916 Inductive Physical Sensing & Battery-Free NFC e-Paper

- **Spec ID**: `NFC-SPEC-04`
- **Status**: Implemented
- **Bounded Context**: `nfc-studio` (`internal/nfcml`, `internal/st25r3916`, `internal/mcpserver`, `internal/ui`)
- **File Size Budget**: Every non-test `.go` file `<= 350` lines; every function `<= 40` lines.

---

## 1. Overview

Repurposes the ST25R3916's 13.56 MHz analog front-end (`MeasureAmplitude 0xD3`, `MeasurePhase 0xD5`, 8-bit ADC `0x25`) and energy-harvesting ISO-DEP initiator into three physical-world tools:
1. **Inductive Metal Detector & Material Classifier** (`US-NFC-04.1`): Classifies nearby objects (`Air`, `Ferrous/Steel`, `Non-Ferrous Copper/Al`, `Resonant 13.56 MHz Tag`, `Water/Dielectric`) from calibrated amplitude/phase deltas with a 0-alloc `InductiveSensor` and rolling sparkline.
2. **Contactless Water Level / Dielectric Moisture Sensor** (`US-NFC-04.2`): Calibrates a `"dry"` baseline against an empty vessel or dry soil, then quantifies dielectric shift (`DRY` / `DAMP` / `WET`, `MoisturePct 0..100%`).
3. **Battery-Free NFC e-Paper Programmer** (`US-NFC-04.3`): Powers and updates Waveshare NFC e-Ink displays (`2.13"`, `2.90"`, `1.54"`) via ISO 14443-A `FieldOn` + `RATS` + `0xCD` APDU command sequence using a pure-Go 5×7 text-to-1-bit bitmap renderer.

---

## 2. User Stories & Acceptance Criteria

### `US-NFC-04.1` — Inductive Metal Detector & Material Classifier
> **As a** hardware hacker, **I want** `nfc-studio` to turn the ST25R3916 antenna into a calibrated inductive metal detector and material classifier, **so that** I can distinguish ferrous steel, non-ferrous copper/aluminum, and resonant 13.56 MHz tags without extra probes.

**Acceptance Criteria (Given / When / Then):**
- **AC-1 (Air Baseline Calibration)**:
  - **Given** an unobstructed antenna in free air,
  - **When** `InductiveSensor.CalibrateAir(amp, phase)` is called,
  - **Then** subsequent `Sample(amp, phase)` calls compute `DeltaAmp = int16(amp) - int16(baseAmp)` and `DeltaPhase = int16(phase) - int16(basePhase)`, returning `MaterialAir` and `StrengthPct == 0` within the deadband (`|DeltaAmp| < 4 && |DeltaPhase| < 4`).
- **AC-2 (Material Classification)**:
  - **Given** a calibrated air baseline,
  - **When** a ferrous object shifts phase positively (`DeltaPhase >= +6`, `DeltaAmp <= -8`), a non-ferrous conductor shifts phase negatively (`DeltaPhase <= -6`), or a resonant 13.56 MHz tag loads amplitude (`DeltaAmp <= -6`, `|DeltaPhase| < 6`),
  - **Then** `Sample` classifies them deterministically as `MaterialFerrous`, `MaterialNonFerrous`, and `MaterialResonantTag` respectively with `StrengthPct` in `0..100`.
- **AC-3 (Zero-Alloc Hot Path & Rolling Sparkline)**:
  - **Given** `BenchmarkInductive` running `Sample` in `for b.Loop()`,
  - **Then** `0 allocs/op` is achieved, and the fixed 32-sample ring buffer exposes chronological `StrengthPct` history via `Sparkline(dst)`.

### `US-NFC-04.2` — Contactless Water Level / Dielectric Moisture Sensor
> **As an** IoT builder, **I want** to calibrate a dry reference against a tank wall or soil sample and read contactless moisture percentage, **so that** I can detect water presence through non-metallic enclosures.

**Acceptance Criteria (Given / When / Then):**
- **AC-1 (Dry Baseline Calibration)**:
  - **Given** an empty plastic/glass container or dry soil near the antenna,
  - **When** `InductiveSensor.CalibrateDry(amp, phase)` is called,
  - **Then** moisture scoring anchors to `(dryAmp, dryPhase)` and reports `MoistureDry` (`MoisturePct < 20`).
- **AC-2 (Dielectric Shift Quantification)**:
  - **Given** a calibrated dry baseline,
  - **When** water (`ε_r ≈ 80`) introduces parasitic capacitance (`+C`) shifting phase/amplitude moderately,
  - **Then** `Sample` classifies `MaterialWaterDielectric`, computes `MoisturePct` clamped to `0..100`, and maps `< 20%` → `MoistureDry`, `20..59%` → `MoistureDamp`, `>= 60%` → `MoistureWet`.

### `US-NFC-04.3` — Battery-Free NFC e-Paper (Waveshare) Programmer
> **As a** CardputerZero user, **I want** to render a custom text badge and push it wirelessly to a battery-free Waveshare NFC e-Paper display, **so that** I can update desk badges or asset tags directly from the handheld or over MCP.

**Acceptance Criteria (Given / When / Then):**
- **AC-1 (Pure-Go 1-Bit Canvas Renderer)**:
  - **Given** a supported `EPaperModel` (`EPaper213` 250×122, `EPaper290` 296×128, `EPaper154` 200×200), `title`, and `body`,
  - **When** `RenderTextBitmap(model, title, body)` runs,
  - **Then** it produces a packed 1-bit monochrome slice of exact length `((width+7)/8) * height` (`0xFF` white background, cleared `0` bits for black border and 5×7 glyphs).
- **AC-2 (Waveshare `0xCD` APDU Framing)**:
  - **Given** a valid model and non-empty bitmap,
  - **When** `BuildWaveshareFrames(model, bitmap)` runs,
  - **Then** it emits the ordered sequence: `0x00` init, `0x01` normal mode, `0x02` model config, `0x03` power on, `0x04` resolution config, `0x05` data start, `N` chunked `0x06` row packets (`<= 100` bytes payload each), and `0x18` refresh.
- **AC-3 (ISO-DEP Upload, Safety & SIM Mode)**:
  - **Given** `Chip.ProgramEPaper(ctx, model, bitmap, onProgress)`,
  - **When** executed against an ISO 14443-4 (`SAK & 0x20 != 0`) e-Paper tag,
  - **Then** it enables `FieldOn`, runs `WUPA` → anticollision → `RATS`, transmits every `0xCD` APDU inside ISO-DEP I-Blocks (handling `S(WTX)`), reports monotonic `0..100` progress, honors `ctx` cancellation, and unconditionally calls `FieldOff()` via `defer`.

---

## 3. Physics & Signal Math

- **Inductive Delta**: $\Delta A = A_{\text{live}} - A_{\text{base}}$, $\Delta \Phi = \Phi_{\text{live}} - \Phi_{\text{base}}$.
- **Ferrous (`+L`)**: High permeability ($\mu_r \gg 1$) increases antenna inductance $L$, shifting tank phase positively ($\Delta\Phi \ge +6$) with hysteresis/eddy loading ($\Delta A \le -8$).
- **Non-Ferrous (`-L`)**: Induced eddy currents in Cu/Al oppose the H-field, reducing effective $L$ and shifting phase negatively ($\Delta\Phi \le -6$).
- **Resonant Tag**: Matched 13.56 MHz LC load absorbs real power ($\Delta A \le -6$) near zero phase shift ($|\Delta\Phi| < 6$).
- **Water Dielectric (`+C`)**: High permittivity ($\varepsilon_r \approx 80$) adds stray capacitance across the coil; moisture score $S = 3|\Delta\Phi_{\text{dry}}| + 2\max(0, -\Delta A_{\text{dry}})$, $\text{MoisturePct} = \min(100, \lfloor 100 S / 60 \rfloor)$.

---

## 4. Exact Go Types & Interfaces

### `internal/nfcml/inductive.go`
```go
type MaterialClass int
const (
	MaterialAir MaterialClass = iota
	MaterialFerrous
	MaterialNonFerrous
	MaterialResonantTag
	MaterialWaterDielectric
)

type MoistureLevel int
const (
	MoistureDry MoistureLevel = iota
	MoistureDamp
	MoistureWet
)

type InductiveReading struct {
	Amp           uint8         `json:"amp"`
	Phase         uint8         `json:"phase"`
	DeltaAmp      int16         `json:"delta_amp"`
	DeltaPhase    int16         `json:"delta_phase"`
	StrengthPct   int           `json:"strength_pct"`
	Material      MaterialClass `json:"material"`
	MaterialLabel string        `json:"material_label"`
	MoistureLevel MoistureLevel `json:"moisture_level"`
	MoisturePct   int           `json:"moisture_pct"`
}

type InductiveSensor struct { /* fixed-size 0-alloc struct */ }
func NewInductiveSensor() *InductiveSensor
func (s *InductiveSensor) CalibrateAir(amp, phase uint8)
func (s *InductiveSensor) CalibrateDry(amp, phase uint8)
func (s *InductiveSensor) Sample(amp, phase uint8) InductiveReading
func (s *InductiveSensor) Sparkline(dst []int16) int
```

### `internal/st25r3916/epaper.go`
```go
type EPaperModel string
const (
	EPaper213 EPaperModel = "2.13" // 250x122
	EPaper290 EPaperModel = "2.90" // 296x128
	EPaper154 EPaperModel = "1.54" // 200x200
)

func RenderTextBitmap(model EPaperModel, title, body string) []byte
func BuildWaveshareFrames(model EPaperModel, bitmap []byte) [][]byte
func (c *Chip) ProgramEPaper(ctx context.Context, model EPaperModel, bitmap []byte, onProgress func(int)) error
```

---

## 5. Safety Gates, UI Sub-View & MCP Tools

- **Safety Gates**:
  - `nfc_epaper_push` MUST reject requests unless `confirm_rf: true` is set.
  - `ProgramEPaper` MUST run `defer c.fieldOffLocked()` and abort immediately on `ctx.Done()`.
- **CardputerZero `320×170` UI Sub-View**:
  - **Inductive Sense**: Live material pill (Emerald `AIR`, Red `FERROUS`, Cyan `NON-FERROUS`, Magenta `RESONANT-TAG`, Amber `WATER`), `ΔA`/`ΔΦ` readout, 32-bar rolling sparkline, and `4 CAL` / `5 MODE (METAL/MOIST)` softkeys.
  - **e-Paper Badge**: Model selector (`2.13`/`2.90`/`1.54`), title/body preview, 2-step `ENTER` RF confirm, and `0–100%` transfer progress bar.
- **MCP Tools (`internal/mcpserver/tools_sensor_epaper.go`)**:
  - `nfc_inductive_sense`: `{mode: "metal"|"moisture", calibrate?: bool, samples?: int}` — samples `MeasureField` (or SIM), persists sensor calibration across calls.
  - `nfc_epaper_push`: `{confirm_rf: true, model: "2.13"|"2.90"|"1.54", title: string, body?: string, sim_mode?: bool}` — renders 1-bit bitmap and uploads via `ProgramEPaper` (or simulates deterministically).

---

## 6. Adversarial Test Plan

| Target | Adversarial & Boundary Cases (`go test -race -cover`) |
| --- | --- |
| `InductiveSensor` | Zero-value sensor without calibration; `uint8` extremes (`0`, `255`); deadband boundary (`±3` vs `±4`); all 5 `MaterialClass` & 3 `MoistureLevel` transitions; sparkline wrap-around past 32 samples; `BenchmarkInductive` verifying `0 allocs/op`. |
| `RenderTextBitmap` | All 3 models exact byte sizes; empty title/body; long wrapping text & non-ASCII runes; unknown model returns `nil`; black pixels verified inside border and glyph regions. |
| `BuildWaveshareFrames` | Verifies exact `0xCD` step order (`0x00..0x05`, chunked `0x06`, `0x18`); exact payload reassembly equals original bitmap; invalid model or empty bitmap returns `nil`. |
| `Chip.ProgramEPaper` | Full mocked ISO 14443-A + `RATS` + `S(WTX)` + `0xCD` APDU exchange; progress callback monotonicity; non-ISO-DEP `SAK=0x00` -> `ErrNotISODEP`; cancelled `ctx` mid-transfer; bad bitmap length; verifies `FieldOff()` always called. |
| MCP Tools | Tool count updated cleanly to `27`; `nfc_inductive_sense` calibrate + metal/moisture modes + invalid mode; `nfc_epaper_push` missing `confirm_rf` rejected, invalid model rejected, empty title rejected, SIM & mock hardware backend paths. |
