# Spec 03: ST25R3916 Multi-Protocol Discovery (NFC-B, FeliCa / NFC-F, NFC-V)

- **Spec ID**: `NFC-SPEC-03`
- **Status**: Draft (Ready for Implementation)
- **Bounded Context**: `nfc-studio` (`internal/st25r3916`, `internal/nfcfile`, `internal/ui`, `internal/mcpserver`)
- **File Size Budget**: Every new or modified `.go` file `<= 350` lines; every function `<= 40` lines.

---

## 1. Overview

Unlocks the ST25R3916's full 13.56 MHz multi-technology frontend beyond ISO 14443-A:
- **NFC-B (ISO 14443-B)**: `SENSB_REQ` -> `PUPI`, `AppData`, `ProtocolInfo` (ePassports, national eIDs, transit cards).
- **NFC-F (FeliCa / JIS X 6319-4)**: `SENSF_REQ` -> `IDm`, `PMm`, `SystemCode` (Suica, Pasmo, Sony FeliCa).
- **NFC-V (ISO 15693 Vicinity)**: `INVENTORY` + `READ_SINGLE_BLOCK` -> 8-byte `UID`, `DSFID`, memory blocks (ski passes, library tags, logistics).

Includes adaptive radar glyphs (`ui/radar.go`), Flipper `.nfc` v4 serialization (`internal/nfcfile/multiprotocol.go`), and MCP `nfc_scan_multiprotocol`.

---

## 2. User Stories & Acceptance Criteria

### `US-NFC-03.1` — ST25R3916 Hardware Multi-Protocol Polling
> **As an** RF/NFC engineer, **I want** `nfc-studio` to discover NFC-B, FeliCa (NFC-F), and ISO 15693 (NFC-V) tags alongside ISO 14443-A, **so that** I can inspect passports, transit cards, ski passes, and library tags from one scanner.

**Acceptance Criteria (Given / When / Then):**
- **AC-1 (NFC-B / ISO 14443-B Discovery)**:
  - **Given** ST25R3916 configured for NFC-B initiator (`regMode = 0x10`, `regBitrate = 0x00` 106 kbps, `regISO14443B = 0x00`, `T_guard >= 5 ms`),
  - **When** `PollISO14443B()` transmits `SENSB_REQ` (`0x05, 0x00, 0x00`, `AFI=0x00`, `PARAM=0x00`) with CRC,
  - **Then** on a valid 12-byte (+ 2 CRC) `SENSB_RES` starting with `0x50`, it extracts 4-byte `PUPI` (`rx[1:5]`), 4-byte `AppData` (`rx[5:9]`), and 3-byte `ProtocolInfo` (`rx[9:12]`).
- **AC-2 (NFC-F / FeliCa Discovery)**:
  - **Given** ST25R3916 configured for NFC-F initiator (`regMode = 0x18`, `regBitrate = 0x11` 212 kbps, `T_guard >= 20 ms`),
  - **When** `PollFeliCa()` transmits `SENSF_REQ` (`0x06, 0x00, 0xFF, 0xFF, 0x01, 0x00` — wildcard System Code `0xFFFF`, `RC=0x01` request system code) with CRC,
  - **Then** on a valid `SENSF_RES` (`len >= 18`, `rx[1] == 0x01`), it extracts 8-byte `IDm` (`rx[2:10]`), 8-byte `PMm` (`rx[10:18]`), and optional 2-byte `SystemCode` (`rx[18:20]`).
- **AC-3 (NFC-V / ISO 15693 Discovery & Block Read)**:
  - **Given** ST25R3916 configured for ISO 15693 (`regMode = 0x30`, 1-out-of-4 coding, `T_guard >= 5 ms`),
  - **When** `PollISO15693()` transmits `INVENTORY` (`0x26, 0x01, 0x00` — high data rate, inventory flag, 1 slot, maskLen=0) with CRC,
  - **Then** on a valid 10-byte (+ 2 CRC) reply (`rx[0] & 0x01 == 0`), it extracts `DSFID = rx[1]` and 8-byte `UID = rx[2:10]` (reversed to MSB-first display order for Flipper compatibility), then reads up to `32` 4-byte blocks via `READ_SINGLE_BLOCK` (`0x22, 0x20, uid_lsb[0..7], blk`) stopping cleanly on error flag `rx[0]&0x01 != 0`.
- **AC-4 (Unified Sequential Multi-Protocol Sweep)**:
  - **Given** `PollMultiProtocol(protocols []Protocol)` with any subset of `{ProtoA, ProtoB, ProtoF, ProtoV}`,
  - **When** called,
  - **Then** it polls each requested protocol sequentially with clean `FieldOff()` between modulation changes, returns the first discovered `MultiTagInfo` (or `ErrNoTag` if none reply), and ALWAYS calls `FieldOff()` via `defer`.

### `US-NFC-03.2` — Adaptive Radar Glyphs, `.nfc` v4 Serialization & MCP Tool
> **As a** CardputerZero user and MCP client, **I want** distinct radar icons, protocol-specific summary fields, Flipper `.nfc` v4 save/load support, and a unified `nfc_scan_multiprotocol` MCP tool, **so that** every 13.56 MHz technology is first-class across UI, storage, and automation.

**Acceptance Criteria (Given / When / Then):**
- **AC-1 (Adaptive Radar Glyphs)**:
  - **Given** `ui.ClassifyRadarSource(tag, profile, field)`,
  - **When** `tag.Protocol` is `NFC-B`, `NFC-F`, or `NFC-V`,
  - **Then** it returns `RadarSourceNFCB` (passport/booklet glyph), `RadarSourceFeliCa` (transit wave glyph), or `RadarSourceNFCV` (vicinity antenna glyph), and `drawTagSummary` renders protocol-specific fields (`PUPI / AppData`, `IDm / PMm / Sys`, or `UID8 / DSFID / Blks`).
- **AC-2 (Flipper `.nfc` v4 Serialization & Round-Trip)**:
  - **Given** an `nfcfile.NfcTag` with `Protocol` set to `NFC-B`, `NFC-F`, or `NFC-V`,
  - **When** serialized via `Format()` and parsed back via `Parse()`,
  - **Then** it emits and parses Flipper-compatible fields:
    - `ISO14443-3B`: `Device type: ISO14443-3B`, `UID: <4B PUPI>`, `Application Data: <4B>`, `Protocol Info: <3B>`.
    - `FeliCa`: `Device type: FeliCa`, `UID: <8B IDm>`, `Manufacturer: <8B PMm>`, `System Code: <2B>`.
    - `ISO15693-3`: `Device type: ISO15693-3`, `UID: <8B>`, `DSFID: <1B>`, `Block Count: <N>`, `Data Content: ...` (or `Block i:`).
- **AC-3 (MCP `nfc_scan_multiprotocol`)**:
  - **Given** MCP enabled and `nfc_scan_multiprotocol` called with `protocols: ["A","B","F","V"]` and `attempts: 2`,
  - **When** a tag is present (or in `SIM` mode cycling/returning requested protocol),
  - **Then** it returns structured JSON containing `found`, `protocol`, `device_type`, `uid_hex`, and protocol-specific metadata (`pupi_hex`/`app_data_hex`/`protocol_info_hex`, `idm_hex`/`pmm_hex`/`system_code_hex`, or `dsfid_hex`/`blocks_hex`).

---

## 3. Exact Go Interfaces & Domain Types

### `internal/st25r3916/registers.go` (additions)
```go
const (
	regISO14443B uint8 = 0x06
	regISO14443B2 uint8 = 0x07

	modeNFCA uint8 = 0x08
	modeNFCB uint8 = 0x10
	modeNFCF uint8 = 0x18
	modeNFCV uint8 = 0x30

	bitrate106 uint8 = 0x00
	bitrate212 uint8 = 0x11
)

type Protocol string

const (
	ProtoNFCA Protocol = "NFC-A"
	ProtoNFCB Protocol = "NFC-B"
	ProtoNFCF Protocol = "NFC-F"
	ProtoNFCV Protocol = "NFC-V"
)
```

### `internal/st25r3916/nfcb.go`, `nfcf.go`, `nfcv.go`
```go
type NFCBInfo struct {
	PUPI         [4]byte
	AppData      [4]byte
	ProtocolInfo [3]byte
}

type FeliCaInfo struct {
	IDm        [8]byte
	PMm        [8]byte
	SystemCode [2]byte
	HasSysCode bool
}

type NFCVInfo struct {
	UID    [8]byte  // MSB-first display order
	DSFID  uint8
	Blocks [][]byte // 4-byte blocks
}

type MultiTagInfo struct {
	Protocol Protocol
	NFCA     *TagInfo
	NFCB     *NFCBInfo
	NFCF     *FeliCaInfo
	NFCV     *NFCVInfo
}

func (c *Chip) PollISO14443B() (*NFCBInfo, error)
func (c *Chip) PollFeliCa() (*FeliCaInfo, error)
func (c *Chip) PollISO15693(maxBlocks int) (*NFCVInfo, error)
func (c *Chip) PollMultiProtocol(ctx context.Context, protos []Protocol) (*MultiTagInfo, error)
```

### `internal/nfcfile/nfcfile.go` & `multiprotocol.go` (backward-compatible struct extension)
```go
// Optional protocol metadata fields added to NfcTag (zero-valued for ISO14443-A):
type NfcTag struct {
	// ... existing fields preserved verbatim ...
	Protocol     string  // "", "NFC-A", "NFC-B", "NFC-F", "NFC-V"
	AppData      [4]byte // ISO14443-3B
	ProtocolInfo [3]byte // ISO14443-3B
	PMm          [8]byte // FeliCa Manufacturer
	SystemCode   [2]byte // FeliCa System Code
	DSFID        uint8   // ISO15693-3
}
```

---

## 4. MCP Tool Contract (`internal/mcpserver/tools_multi.go`)

Extend via optional capability interface `MultiProtocolReader`:
```go
type MultiProtocolReader interface {
	PollMultiProtocol(ctx context.Context, protos []st25r3916.Protocol) (*st25r3916.MultiTagInfo, error)
}
```

- **Tool Name**: `nfc_scan_multiprotocol`
- **Input Schema**:
  - `protocols`: `[]string` (subset of `["A", "B", "F", "V"]`, default `["A", "B", "F", "V"]`)
  - `attempts`: `integer` (`1..10`, default `2`)
  - `max_v_blocks`: `integer` (`0..64`, default `16`, for ISO 15693 block reads)
- **Safety Gate**: Read-only; honors `ctx` cancellation; `FieldOff()` guaranteed on exit; `SIM` mode returns a deterministic tag matching the first requested protocol.

---

## 5. CardputerZero `320×170` UI & Keybindings (`ui/radar.go`, `ui/view_scan.go`, `ui/poller.go`)

- **Protocol Filter Pill** in `ModeScan` header (`x = 196..268`): Displays active scan set (`ALL`, `A`, `B`, `F`, `V`), cycled via `Z`/`C` (`KeyLeft`/`KeyRight`) while in `ModeScan`.
- **Poller (`ui/poller.go`)**: When `MultiProtocolReader` is supported by `a.Chip`, `pollOnce()` calls `PollMultiProtocol` using the active protocol filter (atomic value), preserving the `180 ms` non-blocking ticker.
- **Radar Glyphs (`ui/radar.go`)**:
  - `RadarSourceNFCB`: Compact passport booklet with e-chip symbol (cyan/emerald).
  - `RadarSourceFeliCa`: Twin transit RF chevrons (magenta/cyan).
  - `RadarSourceNFCV`: Long-range vicinity antenna bars (emerald).
- **Summary Panel (`x = 84..314, y = 21..95`)**:
  - NFC-B: `ISO14443-3B`, `PUPI: XX XX XX XX`, `APP : XX XX XX XX`, `INFO: XX XX XX`.
  - NFC-F: `FeliCa`, `IDm : XX .. XX`, `PMm : XX .. XX`, `SYS : XXXX`.
  - NFC-V: `ISO15693-3`, `UID : XX .. XX`, `DSFID: XX   BLK: N`.
- **Emulation Guard**: Pressing `5 (EMU)` on a non-NFC-A tag shows toast `"EMU supports NFC-A only"` instead of corrupting `PT_A` memory.

---

## 6. Edge Cases & Adversarial Test Plan

| Test Target | Adversarial Cases (`go test -race -cover`) |
| --- | --- |
| `PollISO14443B` | Short `SENSB_RES` (`< 12` bytes) -> `ErrProtocol`; wrong prefix (`rx[0] != 0x50`) -> `ErrProtocol`; CRC error IRQ -> `ErrCRC`; `ErrNoTag` on NRE timer. |
| `PollFeliCa` | 18-byte reply (no SystemCode -> `HasSysCode=false`) vs 20-byte reply (`HasSysCode=true`); wrong response code (`rx[1] != 0x01`) -> `ErrProtocol`; short reply -> `ErrProtocol`. |
| `PollISO15693` | Error flag set (`rx[0]&0x01 != 0`) -> `ErrProtocol`; UID byte-order reversal verified (`LSB-first` on wire -> `MSB-first` in `NFCVInfo.UID`); `READ_SINGLE_BLOCK` partial read stops cleanly without discarding discovered UID; `maxBlocks=0` skips block reads. |
| `PollMultiProtocol` | Empty `protos` slice -> defaults to `[A, B, F, V]`; invalid protocol string rejected at MCP layer; `ctx` cancelled between B and F -> stops immediately and calls `FieldOff()`. |
| `multiprotocol.go` | Full round-trip `Format() -> Parse()` for `ISO14443-3B`, `FeliCa`, `ISO15693-3`, plus existing `NTAG215` / `MIFARE Classic` regression tests; fuzz test (`FuzzParseMultiProtocolNFC`). |
| `ui/radar.go` | `ClassifyRadarSource` table test covering `NFC-B`, `NFC-F`, `NFC-V`, `FieldMetalDetune` precedence over protocol glyph, and `EMU` guard toast on non-NFC-A tags. |
