# NFC Studio — Swiss-Army Upgrade Specifications (`specs/`)

Authoritative Spec-Driven Development (SDD) specifications for the `nfc-studio` Swiss-Army upgrade on M5Stack CardputerZero (`320×170` RGB565 `/dev/fb0`, 46-key TCA8418 keyboard, ST25R3916 SPI Mode 1 transceiver, pure-Go ARM64 `CGO_ENABLED=0`).

All specs adhere strictly to [DOMAIN.md](../../DOMAIN.md) (Bounded Context B & C, Ubiquitous Language, Invariants) and [DESIGN.md](../../DESIGN.md) (4-layer clean architecture, `320×170` semantic palette, anti-god-file caps: `<= 350` lines/file, `<= 40` lines/function).

---

## 1. Specification Index & Execution Waves

| Wave | Spec File | User Stories | Scope | New MCP Tools |
| --- | --- | --- | --- | --- |
| **Wave 1** | [01-ntag-write-and-magic-clone.md](./01-ntag-write-and-magic-clone.md) | `US-NFC-01.1`–`01.3` | NTAG213/215/216 Type 2 page writer (`WRITE 0xA2`), Magic Card UID cloner (`Gen1A` backdoor & `Gen2/CUID`), On-device NDEF Composer (URL / Text / Wi-Fi WSC) | `nfc_write_ntag`, `nfc_clone_magic_uid`, `nfc_compose_ndef` |
| **Wave 2** | [02-mifare-dict-and-emv.md](./02-mifare-dict-and-emv.md) | `US-NFC-02.1`–`02.2` | MIFARE Classic 1K/4K dictionary reader (built-in + `~/.config/nfc-studio/mf_keys.dict`, Flipper `.nfc` v4 export), Read-only ISO-DEP / EMV APDU Inspector (`RATS` + `SELECT PPSE`, masked PAN) | `nfc_dict_scan`, `nfc_inspect_emv` |
| **Wave 3** | [03-multiprotocol-nfcb-f-v.md](./03-multiprotocol-nfcb-f-v.md) | `US-NFC-03.1`–`03.2` | ST25R3916 hardware multi-protocol discovery: NFC-B (`ISO 14443-B`), NFC-F (`FeliCa`), NFC-V (`ISO 15693`), adaptive radar glyphs, `.nfc` v4 serialization | `nfc_scan_multiprotocol` |

---

## 2. Package Architecture Alignment

Every new capability fits cleanly into the existing `nfc-studio/internal/` boundaries without creating catch-all packages or exceeding the `350`-line cap:

```text
nfc-studio/internal/
├── st25r3916/          # Hardware ACL (mutex-serialized SPI Mode 1)
│   ├── ntag_write.go   # [Spec 01] WriteType2Page (0xA2), WriteType2Range, verify
│   ├── magic_clone.go  # [Spec 01] Gen1A 7-bit 0x40/0x43 & Gen2/CUID block 0 writer
│   ├── mifare_dict.go  # [Spec 02] MIFARE Classic Auth + dictionary sector reader
│   ├── isodep_emv.go   # [Spec 02] RATS, I-Block transceive, read-only PPSE/EMV
│   ├── nfcb.go         # [Spec 03] ISO 14443-B SENSB_REQ / SENSB_RES
│   ├── nfcf.go         # [Spec 03] FeliCa SENSF_REQ / SENSF_RES
│   └── nfcv.go         # [Spec 03] ISO 15693 INVENTORY & READ_SINGLE_BLOCK
├── nfcfile/            # Pure Domain Codecs (zero hardware imports)
│   ├── ndef_encode.go  # [Spec 01] URL, Text, Wi-Fi WSC TLV encoder & page packer
│   ├── mifare_keys.go  # [Spec 02] Built-in keys + mf_keys.dict parser
│   ├── emv_tlv.go      # [Spec 02] Read-only BER-TLV parser + PAN masking
│   └── multiprotocol.go# [Spec 03] NFC-B / FeliCa / NFC-V .nfc v4 format & parse
├── ui/                 # 320x170 RGB565 Framebuffer Views (zero blocking SPI)
│   ├── view_write.go   # [Spec 01] NTAG Write / Magic Clone / NDEF Composer sub-view
│   ├── view_inspect.go # [Spec 02] MIFARE Dict progress & EMV Inspector sub-view
│   └── radar.go        # [Spec 03] RadarSourceNFCB, RadarSourceFeliCa, RadarSourceNFCV
└── mcpserver/          # Default-Deny Stdio JSON-RPC 2.0 Adapter
    ├── tools_write.go  # [Spec 01] nfc_write_ntag, nfc_clone_magic_uid, nfc_compose_ndef
    ├── tools_inspect.go# [Spec 02] nfc_dict_scan, nfc_inspect_emv
    └── tools_multi.go  # [Spec 03] nfc_scan_multiprotocol
```

---

## 3. Global Safety & Quality Invariants

1. **Explicit Write Gates**: Every physical RF write or clone operation MUST require `confirm_write: true` in MCP and a 2-step confirmation (`[ENTER] CONFIRM WRITE` within `5 s`) in the UI.
2. **Lock/OTP/Config Protection**: `WRITE (0xA2)` MUST refuse pages `0..3` (UID/BCC/Lock/CC) and dynamic lock/config tail pages unless `allow_system_pages: true` is explicitly supplied alongside `confirm_write: true`.
3. **Strict Read-Only EMV**: EMV inspection MUST NEVER issue `VERIFY PIN`, `GENERATE AC`, `PUT DATA`, or `EXTERNAL AUTHENTICATE` APDUs, and MUST ALWAYS mask PANs (`XXXXXX******XXXX`).
4. **No CRYPTO1 Cracking**: MIFARE Classic support is strictly a known-key dictionary reader (factory + user-owned `mf_keys.dict`); darkside/nested/hardnested key recovery remains out of scope per `DOMAIN.md`.
5. **Non-Blocking UI & Field-Off Cleanup**: Long-running writes, dictionary scans, and multi-protocol sweeps MUST run on background worker goroutines with `context.Context` cancellation and unconditionally call `FieldOff()` via `defer`.
6. **SIM Mode Parity**: Every UI flow and MCP tool MUST function deterministically when `Chip == nil` (`SimMode == true`).
7. **Verification & Docs**: Each implemented wave MUST pass `go test -race -cover ./...` (`>= 90%` on new packages), update `drawHelpModal`, `nfc-studio/README.md`, `learning-portal/content/nfc-studio/guide.md`, domain theory + interactive lab, `learning-portal/content/glossary.md`, and regenerate `320×170` screenshots via `go run ./cmd/shots`.
