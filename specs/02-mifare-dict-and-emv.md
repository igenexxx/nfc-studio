# Spec 02: MIFARE Classic Dictionary Reader & Read-Only EMV APDU Inspector

- **Spec ID**: `NFC-SPEC-02`
- **Status**: Draft (Ready for Implementation)
- **Bounded Context**: `nfc-studio` (`internal/st25r3916`, `internal/nfcfile`, `internal/ui`, `internal/mcpserver`)
- **File Size Budget**: Every new or modified `.go` file `<= 350` lines; every function `<= 40` lines.

---

## 1. Overview

Adds two high-value ISO 14443-A inspection capabilities while preserving `DOMAIN.md` boundaries:
1. **MIFARE Classic 1K / 4K Dictionary Reader**: Authenticates sectors using a built-in default key set plus user-supplied `~/.config/nfc-studio/mf_keys.dict` (strictly known-key authentication — **NO** darkside/nested/hardnested CRYPTO1 cracking), displays live sector recovery progress, and exports Flipper `.nfc` v4 dumps with recovered Key A/B.
2. **Read-Only ISO-DEP / EMV APDU Inspector**: For `SAK 0x20` (ISO 14443-4) contactless payment cards, performs `RATS` -> `SELECT PPSE (2PAY.SYS.DDF01)` -> `SELECT AID` -> read-only `READ RECORD` to inspect AID, Application Label, masked PAN, and Expiry Date with **zero** destructive or state-mutating APDUs.

---

## 2. User Stories & Acceptance Criteria

### `US-NFC-02.1` — MIFARE Classic 1K/4K Dictionary Reader
> **As an** access-control auditor, **I want** to test a MIFARE Classic 1K/4K card against factory default keys and my own `mf_keys.dict`, **so that** I can audit weak/default sector keys and export a Flipper-compatible `.nfc` v4 dump.

**Acceptance Criteria (Given / When / Then):**
- **AC-1 (Built-in + User Dictionary Loading)**:
  - **Given** built-in default keys (`FFFFFFFFFFFF`, `A0A1A2A3A4A5`, `D3F7D3F7D3F7`, `000000000000`, `B0B1B2B3B4B5`, `4D3A99C351DD`, `1A982C7E459A`, `AABBCCDDEEFF`, `714C5C886E97`, `587EE5F9350F`, `A0478CC39091`, `533CB6C723F6`, `8FD0A4F256E9`) and optional `$HOME/.config/nfc-studio/mf_keys.dict`,
  - **When** `LoadMifareKeyDict(path)` runs,
  - **Then** it ignores blank lines and `#` comments, accepts 12-hex-char (6-byte) keys with optional spaces/colons, deduplicates keys while preserving order (built-in first, then user), and silently skips missing user file without error.
- **AC-2 (Sector Count & Key A/B Recovery)**:
  - **Given** a MIFARE Classic 1K (`SAK 0x08`, `16` sectors / `64` blocks) or 4K (`SAK 0x18`, `40` sectors / `256` blocks),
  - **When** `ReadMifareClassicDict(ctx, keys, onProgress)` runs,
  - **Then** for each sector it tests Key A (`0x60`) and Key B (`0x61`) against `keys`, re-selecting via `WUPA` after any failed auth (MIFARE HALT behavior), reads accessible 16-byte blocks via `READ (0x30)`, emits `DictProgress` updates per sector, and respects `ctx.Done()` immediately.
- **AC-3 (Flipper `.nfc` v4 Export)**:
  - **Given** a completed or partial `MifareDump`,
  - **When** converted via `ToNfcTag()` and formatted with `NfcTag.Format()`,
  - **Then** it writes `Device type: Mifare Classic`, `Mifare Classic type: 1K` (or `4K`), `Data format version: 2`, and `Block 0..N` lines where recovered Key A/B are populated into sector trailer bytes `0..5` and `10..15`, and unreadable blocks/keys are formatted as `??` bytes.
- **AC-4 (Non-Blocking UI & Field-Off)**:
  - **Given** a dictionary scan triggered from `SCAN` or `DUMP`,
  - **When** scanning runs or is cancelled by user (`4 STOP` / `Esc`),
  - **Then** SPI traffic executes in a background goroutine (never blocking the `55 ms` UI loop) and unconditionally calls `FieldOff()` on exit.

### `US-NFC-02.2` — Read-Only ISO-DEP / EMV APDU Inspector
> **As a** payment-card researcher, **I want** to inspect public contactless EMV metadata on `SAK 0x20` cards via read-only APDUs, **so that** I can view AID, card brand label, masked PAN, and expiry date without risking card lock or PIN counter decrement.

**Acceptance Criteria (Given / When / Then):**
- **AC-1 (Strict Read-Only APDU Allowlist)**:
  - **Given** the EMV inspector,
  - **When** transmitting ISO 7816-4 APDUs over ISO-DEP I-Blocks (`PCB 0x02`/`0x03`),
  - **Then** ONLY `SELECT` (`CLA=0x00, INS=0xA4`) and `READ RECORD` (`CLA=0x00, INS=0xB2`) (plus optional zero-amount read-only `GET PROCESSING OPTIONS` `CLA=0x80, INS=0xA8` only when AFL is absent) are permitted. `VERIFY (0x20)`, `GENERATE AC (0xAE)`, `PUT DATA (0xDA)`, and `EXTERNAL AUTH (0x82)` MUST be statically disallowed (`ErrForbiddenAPDU`).
- **AC-2 (RATS + PPSE + BER-TLV Extraction)**:
  - **Given** a selected ISO 14443-4 card (`SAK & 0x20 != 0`),
  - **When** `InspectEMV(ctx)` runs,
  - **Then** it sends `RATS (0xE0 0x80)` to obtain `ATS`, selects `2PAY.SYS.DDF01` (`00 A4 04 00 0E 32 50 41 59 2E 53 59 53 2E 44 44 46 30 31 00`), parses BER-TLV tag `0x4F` (AID) and `0x50` (Application Label), selects the first AID, reads SFI 1..2 records (`READ RECORD`), and extracts `0x5A` (PAN) / `0x57` (Track 2) and `0x5F24` (Expiry `YYMM`).
- **AC-3 (Mandatory PAN Masking Invariant)**:
  - **Given** any extracted PAN (12–19 digits),
  - **When** building `EMVInfo`, rendering UI, or returning MCP JSON,
  - **Then** the PAN MUST be masked to `BIN(6) + "*"*(len-10) + Last4` (e.g. `411111******1111`), and the unmasked PAN MUST NEVER be stored in `EMVInfo`, logs, `.nfc` files, or MCP responses.
- **AC-4 (SIM Mode Parity)**:
  - **Given** `SimMode == true`,
  - **When** `nfc_dict_scan` or `nfc_inspect_emv` is invoked,
  - **Then** deterministic synthetic results (e.g., 1K with 4/16 default keys found; synthetic Visa test AID `A0000000031010`, masked `400000******0002`, exp `28/12`) are returned with `sim_mode: true`.

---

## 3. Exact Go Interfaces & Domain Types

### `internal/nfcfile/mifare_keys.go` & `emv_tlv.go`
```go
type MifareKey [6]byte

func DefaultMifareKeys() []MifareKey
func DefaultKeyDictPath() string // $HOME/.config/nfc-studio/mf_keys.dict
func LoadMifareKeyDict(path string) ([]MifareKey, error)
func ParseMifareKeyDict(r io.Reader) ([]MifareKey, error)

type EMVApp struct {
	AID       string `json:"aid"`
	Label     string `json:"label"`
	MaskedPAN string `json:"masked_pan,omitempty"`
	Expiry    string `json:"expiry,omitempty"` // "YY/MM"
}

type EMVInfo struct {
	UIDHex string   `json:"uid_hex"`
	ATSHex string   `json:"ats_hex"`
	Apps   []EMVApp `json:"apps"`
}

func ParseEMVTLV(data []byte) (map[uint16][]byte, error)
func MaskPAN(digits string) string
```

### `internal/st25r3916/mifare_dict.go` & `isodep_emv.go`
```go
var (
	ErrNotMifareClassic = errors.New("st25r3916: tag is not MIFARE Classic")
	ErrNotISODEP        = errors.New("st25r3916: tag does not support ISO-DEP (SAK 0x20)")
	ErrForbiddenAPDU    = errors.New("st25r3916: non-read-only APDU blocked")
)

type SectorResult struct {
	Sector   int        `json:"sector"`
	HasKeyA  bool       `json:"has_key_a"`
	KeyA     [6]byte    `json:"-"`
	HasKeyB  bool       `json:"has_key_b"`
	KeyB     [6]byte    `json:"-"`
	Blocks   [][16]byte `json:"-"`
	Readable []bool     `json:"readable"`
}

type DictProgress struct {
	Sector       int
	TotalSectors int
	KeysFoundA   int
	KeysFoundB   int
	BlocksRead   int
}

type MifareDump struct {
	UID          []byte
	ATQA         [2]byte
	SAK          uint8
	CardSize     string // "1K" or "4K"
	Sectors      []SectorResult
	KeysTested   int
	ElapsedMs    int64
}

func (c *Chip) ReadMifareClassicDict(ctx context.Context, keys [][6]byte, onProgress func(DictProgress)) (*MifareDump, error)
func (c *Chip) TransceiveISODEP(ctx context.Context, apdu []byte) ([]byte, error)
func (c *Chip) InspectEMV(ctx context.Context) (*nfcfile.EMVInfo, error)
```

---

## 4. MCP Tool Contracts (`internal/mcpserver/tools_inspect.go`)

Extend via optional capability interface `NFCInspector`:
```go
type NFCInspector interface {
	ReadMifareClassicDict(ctx context.Context, keys [][6]byte, onProgress func(st25r3916.DictProgress)) (*st25r3916.MifareDump, error)
	InspectEMV(ctx context.Context) (*nfcfile.EMVInfo, error)
}
```

| Tool Name | Arguments | Response Fields | Safety Gate |
| --- | --- | --- | --- |
| `nfc_dict_scan` | `max_keys` (int `1..256`, default `64`), `dict_path` (optional override rejected if outside `~/.config/nfc-studio`), `save_name` (optional `.nfc` export) | `uid_hex`, `card_size`, `sectors_total`, `keys_found_a`, `keys_found_b`, `blocks_read`, `blocks_total`, `sectors` (key hex + readable flags), `saved_path` | Read-only RF; enforces `ctx` timeout (`<= 30 s`); path-sandboxed `save_name`. |
| `nfc_inspect_emv` | `{}` (no required args) | `uid_hex`, `ats_hex`, `apps: [{aid, label, masked_pan, expiry}]`, `read_only: true` | Strictly read-only APDUs; PAN always masked. |

---

## 5. CardputerZero `320×170` UI & Keybindings (`internal/ui/view_inspect.go`)

Triggered from `SCAN` or `DUMP` when `SAK == 0x08 / 0x18` (MIFARE) or `SAK & 0x20 != 0` (ISO-DEP):
- **Header**: Reuses `drawHeader` with `DICT` (cyan) or `EMV` (emerald) pill.
- **MIFARE Dict View (`y = 18..142`)**:
  - Top bar: `MIFARE 1K  Keys: 13+U  Sec: 08/16`.
  - Sector grid (`16` compact `14×10 px` cells for 1K, scrollable `20/page` for 4K): Emerald = Key A+B, Cyan = Key A or B, Muted = locked.
  - Footer: `A:12/16  B:10/16  Blk:48/64`.
- **EMV Inspector View (`y = 18..142`)**:
  - `APP : VISA CREDIT (A0000000031010)`
  - `PAN : 4111 11** **** 1111` (amber masked badge)
  - `EXP : 28/12   ATS: 05 78 ...`
  - `MODE: READ-ONLY (SAFE)` (emerald badge)
- **Bottom Nav Bar (`y = 143..169`)**:
  - `4 (RUN/STOP)`: Starts/cancels async dict scan or EMV read.
  - `5 (DICT/EMV)`: Toggles between MIFARE Dict and EMV Inspector views.
  - `6 (SAVE)`: Exports recovered MIFARE `.nfc` v4 dump to `~/nfc-studio/tags/` (disabled for EMV to avoid persisting card data).
  - `7 (DUMP)`: Opens recovered blocks in `ModeDump`.
  - `8 (BACK)`: Returns to `ModeScan`.

---

## 6. Edge Cases & Adversarial Test Plan

| Test Target | Adversarial Cases (`go test -race -cover`) |
| --- | --- |
| `ParseMifareKeyDict` | Empty file, comments-only, CRLF line endings, mixed case, odd-length hex, `5`-byte or `7`-byte keys (skipped cleanly), duplicate keys, >10,000-line file cap. |
| `ReadMifareClassicDict` | Non-MIFARE card (`SAK 0x00`) -> `ErrNotMifareClassic`; partial sector auth (Key A succeeds, Key B fails); card removed mid-scan (`ErrNoTag`) -> returns partial dump safely + `FieldOff()`; `ctx` cancel after sector 2. |
| `MifareDump.ToNfcTag` | 1K (`64` blocks) and 4K (`256` blocks, 32×4 + 8×16 sector layout) formatting; unreadable blocks emit `?? ?? ... ??`; round-trip parse safety. |
| `ParseEMVTLV` / `MaskPAN` | Truncated length, multi-byte tag (`0x5F24`, `0x9F38`), indefinite/0x81/0x82 length encoding, zero-length value, recursive constructed tags (`0x6F`, `0xA5`, `0xBF0C`, `0x70`), PAN length `< 10` (fully masked `****`), `16` digits (`411111******1111`), fuzz test (`FuzzParseEMVTLV`). |
| `TransceiveISODEP` | Forbidden APDU (`INS=0x20`, `0xAE`, `0xDA`, `0x82`) -> `ErrForbiddenAPDU` before SPI TX; WTX (`0xF2`) extension frame handling; I-Block PCB toggling (`0x02`/`0x03`). |
