# Spec 01: Physical NTAG Writer, Magic Card UID Cloner & On-Device NDEF Composer

- **Spec ID**: `NFC-SPEC-01`
- **Status**: Draft (Ready for Implementation)
- **Bounded Context**: `nfc-studio` (`internal/st25r3916`, `internal/nfcfile`, `internal/ui`, `internal/mcpserver`)
- **File Size Budget**: Every new or modified `.go` file `<= 350` lines; every function `<= 40` lines.

---

## 1. Overview

Extends `nfc-studio` from a read/emulate tool into a safe physical tag writer:
1. **NTAG213 / 215 / 216 Type 2 Page Writer**: Writes 4-byte pages using ISO 14443-A Type 2 `WRITE (0xA2)` with strict page-range guards and `READ (0x30)` read-back verification.
2. **Magic Card UID Cloner**: Clones 4-byte UIDs onto Gen1A (`0x40`/`0x43` 7-bit backdoor) and Gen2/CUID (direct block 0) magic cards with automatic BCC calculation and verification.
3. **On-Device NDEF Composer**: Builds standard NDEF TLV (`0x03`) payloads for **URL**, **Text**, and **Wi-Fi Credentials** (`application/vnd.wfa.wsc`) right on CardputerZero for immediate write, save, or emulation.

---

## 2. User Stories & Acceptance Criteria

### `US-NFC-01.1` — Physical NTAG213/215/216 Type 2 Page Writer
> **As a** hardware researcher, **I want** to write user-memory pages to a physical NTAG213/215/216 tag with safety guards and read-back verification, **so that** I can provision tags without accidentally bricking OTP, Lock, or Capability Container bytes.

**Acceptance Criteria (Given / When / Then):**
- **AC-1 (Safe Default Range)**:
  - **Given** a detected NTAG213 (user pages `4..39`), NTAG215 (`4..129`), or NTAG216 (`4..225`) inferred from CC byte `Page 3[2]`,
  - **When** `WriteType2Pages` is invoked for pages within user range,
  - **Then** each 4-byte page is written via `WRITE (0xA2, page, b0, b1, b2, b3)` with CRC, expecting 4-bit ACK `0x0A` (ST25R3916 FIFO 1 byte, `no_crc_rx = 1`), followed by `READ (0x30)` verification confirming written bytes match.
- **AC-2 (System Page Guard)**:
  - **Given** a write request targeting pages `0..3` (UID/BCC/Lock/CC) or dynamic lock/config tail pages (`>= userEnd+1`, e.g., `40..44` on NTAG213, `130..134` on NTAG215, `226..230` on NTAG216),
  - **When** `AllowSystemPages` is `false` (default),
  - **Then** the driver MUST reject the entire write before transmitting any RF frame, returning `ErrProtectedPage`.
- **AC-3 (Safety Confirmation Gate)**:
  - **Given** an MCP call to `nfc_write_ntag` with `confirm_write != true`,
  - **When** the server validates arguments,
  - **Then** it returns an `isError: true` JSON payload explaining `confirm_write must be true`, without touching ST25R3916.
- **AC-4 (SIM Mode Parity)**:
  - **Given** `SimMode == true` (`backend == nil`),
  - **When** `nfc_write_ntag` is called with valid pages and `confirm_write: true`,
  - **Then** it validates page ranges/lengths, updates simulated in-memory state (or returns a deterministic `sim_mode: true` report with `verified: true`), and never panics.

### `US-NFC-01.2` — Magic Card UID Cloner (`Gen1A` & `Gen2/CUID`)
> **As a** physical access tester, **I want** to clone a 4-byte UID (with ATQA/SAK and valid BCC) onto a Gen1A or Gen2/CUID Magic Card, **so that** I can replicate authorized test badges on physical silicon.

**Acceptance Criteria (Given / When / Then):**
- **AC-1 (Block 0 Construction & BCC Invariant)**:
  - **Given** a 4-byte target `UID` (`u0..u3`), 1-byte `SAK`, and 2-byte `ATQA`,
  - **When** `BuildBlock0(uid, sak, atqa)` runs,
  - **Then** it produces a 16-byte block `[u0, u1, u2, u3, u0^u1^u2^u3, sak, atqa[0], atqa[1], 0x44, 0x00, ...]` where byte 4 is strictly the XOR BCC. Non-4-byte UIDs return `ErrInvalidUIDLen`.
- **AC-2 (Gen1A Backdoor Sequence)**:
  - **Given** `MagicGen1A` mode and `confirm_write: true`,
  - **When** `CloneMagicUID` executes,
  - **Then** ST25R3916 sends `HALT (0x50 0x00)` with CRC -> 7-bit short frame `0x40` (`regTxBytes2 = 0x07`, `cmdTransmitNoCRC`) expecting `0x0A` -> 8-bit `0x43` (`cmdTransmitNoCRC`) expecting `0x0A` -> `WRITE_BLOCK (0xA0 0x00)` expecting `0x0A` -> 16-byte Block 0 with CRC expecting `0x0A` -> re-polls `PollISO14443A()` to verify the new UID matches.
- **AC-3 (Gen2 / CUID Direct Write)**:
  - **Given** `MagicGen2CUID` mode and `confirm_write: true`,
  - **When** `CloneMagicUID` executes,
  - **Then** ST25R3916 selects the card, authenticates Sector 0 with default key (`FFFFFFFFFFFF`, or caller-supplied `key_a_hex`), issues `WRITE_BLOCK (0xA0 0x00)` + 16-byte Block 0, and verifies via `PollISO14443A()`.
- **AC-4 (Field-Off Cleanup)**:
  - **Given** any failure (NAK, timeout, wrong card type, `ctx.Done()`),
  - **When** `CloneMagicUID` exits,
  - **Then** `FieldOff()` is unconditionally called via `defer`.

### `US-NFC-01.3` — On-Device NDEF Composer (URL, Text, Wi-Fi WSC)
> **As a** CardputerZero user, **I want** to compose URL, Text, or Wi-Fi credentials NDEF payloads on-device, **so that** I can immediately write them to an NTAG21x tag or save/emulate them without a PC.

**Acceptance Criteria (Given / When / Then):**
- **AC-1 (URI Encoding with Prefix Compression)**:
  - **Given** a URL string (e.g. `https://cardputer.cc`),
  - **When** `EncodeNDEFURI(url)` runs,
  - **Then** it selects the longest matching NFC Forum URI identifier code from `uriPrefixes` (`0x04` for `https://`), wraps it in an NDEF Short Record (`0xD1, 0x01, len, 'U', code, suffix...`), wraps in TLV `0x03 <len> <msg> 0xFE`, and pads to 4-byte page boundaries.
- **AC-2 (Text Encoding)**:
  - **Given** language code (default `"en"`) and UTF-8 text,
  - **When** `EncodeNDEFText(lang, text)` runs,
  - **Then** it emits NDEF `'T'` record (`status = len(lang) & 0x3F`) wrapped in TLV `0x03 ... 0xFE` and round-trips cleanly through `ExtractNDEF()`.
- **AC-3 (Wi-Fi WSC Encoding)**:
  - **Given** `SSID`, `Password`, and `AuthType` (`OPEN`, `WPA2`, `WPA3`),
  - **When** `EncodeNDEFWiFi(cfg)` runs,
  - **Then** it emits an NFC Forum MIME record (`TNF=0x02`, type `"application/vnd.wfa.wsc"`) containing Credential TLV `0x100E` with SSID (`0x1045`), Auth Type (`0x1003`), Encryption (`0x100F`), and Network Key (`0x1027`), wrapped in TLV `0x03 ... 0xFE`.
- **AC-4 (Capacity Enforcement)**:
  - **Given** a composed TLV exceeding target tag user memory (e.g. `> 144` bytes for NTAG213, `> 504` bytes for NTAG215, `> 888` bytes for NTAG216),
  - **When** packing into pages via `PackNDEFPages(tlv, maxUserPages)`,
  - **Then** it returns `ErrNDEFOversize` before any RF write occurs.

---

## 3. Exact Go Interfaces & Domain Types

### `internal/st25r3916/ntag_write.go` & `magic_clone.go`
```go
var (
	ErrProtectedPage = errors.New("st25r3916: protected system/lock/config page")
	ErrWriteNAK      = errors.New("st25r3916: tag returned NAK on write")
	ErrVerifyFailed  = errors.New("st25r3916: read-back verification mismatch")
	ErrInvalidUIDLen = errors.New("st25r3916: magic clone requires 4-byte UID")
)

type NTAGModel struct {
	Name          string // "NTAG213", "NTAG215", "NTAG216"
	UserStartPage uint8  // 4
	UserEndPage   uint8  // 39, 129, or 225
	TotalPages    uint8  // 45, 135, or 231
}

type WritePageOp struct {
	Page uint8
	Data [4]byte
}

type WriteOptions struct {
	AllowSystemPages bool
	VerifyReadBack   bool // default true
}

type WriteReport struct {
	Model        string `json:"model"`
	PagesWritten int    `json:"pages_written"`
	Verified     bool   `json:"verified"`
	FailedPage   int    `json:"failed_page,omitempty"` // -1 when none
}

type MagicCardGen string

const (
	MagicGen1A    MagicCardGen = "gen1a"
	MagicGen2CUID MagicCardGen = "gen2"
)

type CloneReport struct {
	Gen        MagicCardGen `json:"gen"`
	TargetUID  string       `json:"target_uid"`
	Verified   bool         `json:"verified"`
	ReadBackID string       `json:"read_back_uid,omitempty"`
}

func IdentifyNTAGModel(ccPage [4]byte) (NTAGModel, error)
func BuildBlock0(uid []byte, sak uint8, atqa [2]byte) ([16]byte, error)
func (c *Chip) WriteType2Pages(ctx context.Context, ops []WritePageOp, opts WriteOptions) (WriteReport, error)
func (c *Chip) CloneMagicUID(ctx context.Context, gen MagicCardGen, uid []byte, atqa [2]byte, sak uint8, keyA [6]byte) (CloneReport, error)
```

### `internal/nfcfile/ndef_encode.go`
```go
var ErrNDEFOversize = errors.New("nfcfile: NDEF payload exceeds tag user memory")

type WiFiAuth uint16

const (
	WiFiAuthOpen WiFiAuth = 0x0001
	WiFiAuthWPA2 WiFiAuth = 0x0020
)

type WiFiConfig struct {
	SSID     string
	Password string
	Auth     WiFiAuth
}

func EncodeNDEFURI(rawURL string) ([]byte, error)
func EncodeNDEFText(lang, text string) ([]byte, error)
func EncodeNDEFWiFi(cfg WiFiConfig) ([]byte, error)
func PackNDEFPages(tlv []byte, maxUserPages int) ([][4]byte, error)
```

---

## 4. MCP Tool Contracts (`internal/mcpserver/tools_write.go`)

Extend `NFCBackend` via optional capability interface `NFCWriter` (Interface Segregation — existing mocks keep compiling):
```go
type NFCWriter interface {
	WriteType2Pages(ctx context.Context, ops []st25r3916.WritePageOp, opts st25r3916.WriteOptions) (st25r3916.WriteReport, error)
	CloneMagicUID(ctx context.Context, gen st25r3916.MagicCardGen, uid []byte, atqa [2]byte, sak uint8, keyA [6]byte) (st25r3916.CloneReport, error)
}
```

| Tool Name | Required Args | Optional Args | Safety Gate |
| --- | --- | --- | --- |
| `nfc_write_ntag` | `start_page` (int `>= 0`), `pages_hex` (`[]string`, 4B each), `confirm_write: true` | `allow_system_pages` (bool, default `false`), `verify` (bool, default `true`) | Rejects unless `confirm_write == true`; rejects pages `< 4` or `> userEnd` unless `allow_system_pages == true` |
| `nfc_clone_magic_uid` | `gen` (`"gen1a"` \| `"gen2"`), `uid_hex` (4B), `confirm_write: true` | `atqa_hex` (`"04 00"`), `sak_hex` (`"08"`), `key_a_hex` (`"FF FF FF FF FF FF"`) | Rejects unless `confirm_write == true`; enforces 4-byte UID + BCC |
| `nfc_compose_ndef` | `kind` (`"uri"` \| `"text"` \| `"wifi"`), payload fields (`url` / `text`+`lang` / `ssid`+`password`+`auth`) | `write_to_tag` (bool), `confirm_write` (bool), `save_name` (string) | Pure encode works without `confirm_write`; `write_to_tag: true` requires `confirm_write: true` |

---

## 5. CardputerZero `320×170` UI & Keybindings (`internal/ui/view_write.go`)

Accessible as a sub-view from `DUMP` (softkey `5` when viewing an NTAG/loaded tag) or `SAVED` (`W` / sub-action):
- **Header**: Reuses `drawHeader` with `WRITE` amber mode pill.
- **Workspace (`y = 18..142`)**:
  - Top row (`y = 21..35`): Sub-tab bar `[NTAG WR] [MAGIC] [NDEF]` switched via `Z`/`C` (`KeyLeft`/`KeyRight`).
  - Body (`y = 38..138`):
    - `NTAG WR`: Displays target model, page range (`P04..Pxx`), source tag UID, and 2-step confirmation banner (`Press 4/ENTER again within 5s to WRITE`).
    - `MAGIC`: Displays `Gen1A (0x40/0x43)` vs `Gen2 (CUID)` toggle, target 4-byte UID + auto-computed `BCC: XX`, `ATQA`, `SAK`.
    - `NDEF`: Preset selector (`URL`, `TEXT`, `WIFI`) with editable/preset templates and byte/page counter (`24B / 6 pg`).
- **Bottom Nav Bar (`y = 143..169`)**:
  - `4 (ARM/WR)`: First press arms 5-second confirmation window (amber border); second press executes async write worker.
  - `5 (GEN/KIND)`: Cycles `Gen1A`/`Gen2` or `URL`/`TEXT`/`WIFI`.
  - `6 (SAVE)`: Saves composed NDEF tag to `~/nfc-studio/tags/`.
  - `7 (EMU)`: Loads composed NDEF tag into `ModeEmu`.
  - `8 (BACK)`: Exits sub-view and cancels any armed prompt.

---

## 6. Edge Cases & Adversarial Test Plan

| Test Target | Adversarial Cases (`go test -race -cover`) |
| --- | --- |
| `IdentifyNTAGModel` | Zero/corrupt CC magic (`!= 0xE1`), unknown size byte (`0x00`, `0xFF`), exact NTAG213 (`0x12`), NTAG215 (`0x3E`), NTAG216 (`0x6D`). |
| `WriteType2Pages` | Attempt write to page `0`, `2` (static lock), `3` (CC), `40` (NTAG213 dynamic lock) with `AllowSystemPages=false` -> `errors.Is(err, ErrProtectedPage)`; NAK `0x00` on page 6 -> `ErrWriteNAK` + `FailedPage=6`; read-back mismatch -> `ErrVerifyFailed`; `ctx` cancelled mid-write -> `FieldOff()` called. |
| `BuildBlock0` / `CloneMagicUID` | 0-byte, 3-byte, 7-byte UID -> `ErrInvalidUIDLen`; BCC XOR verification across 100 fuzz inputs (`FuzzBuildBlock0`); Gen1A NAK on `0x40` -> aborts before `0x43` and calls `FieldOff()`. |
| `ndef_encode.go` | Empty URL/SSID, max short-record boundary (`255` bytes) vs long-record (`>= 256` bytes), TLV 3-byte length (`>= 255` bytes), UTF-8 multi-byte text, `ErrNDEFOversize` on NTAG213 (`> 36` user pages), round-trip `Encode -> Pack -> ExtractNDEF` fuzz test (`FuzzNDEFEncodeRoundTrip`). |
| `tools_write.go` | Missing `confirm_write`, `confirm_write: false`, malformed hex, path traversal in `save_name` (`../evil.nfc`), SIM mode determinism. |
