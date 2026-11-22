# NFC Studio

Native 13.56 MHz HF NFC scanner, color-coded hex/NDEF memory inspector, hardware Passive Target UID emulator, and Flipper-compatible `.nfc` library for the **M5Stack CardputerZero** + **Cap CC1101 & NFC** (**ST25R3916**).

Built in pure Go (`CGO_ENABLED=0`, zero external runtime dependencies) rendering directly to `/dev/fb0` (320×170 RGB565) with `evdev` keyboard input.

> **Hardware Safety:** Never hot-swap the Cap CC1101 & NFC module while CardputerZero is powered on.

> [!CAUTION]
> **Legal & Responsible Use Disclaimer:** This software is provided strictly for **educational, research, diagnostics, and authorized security testing** on NFC tags, cards, and readers that you own or have explicit written permission to test. Unauthorized cloning, emulation, reading, modification, or bypassing of third-party access-control systems, payment cards, or identity documents is illegal and punishable by law. You are solely responsible for complying with all applicable local laws and RF regulations. Provided **"AS IS"** without warranty of any kind; use entirely at your own risk.

---

## Features

- **Live ISO 14443-A Scanner & Adaptive ADC Radar (`SCAN`):** Background 180 ms hardware poller performing full `WUPA` → Cascade Level 1–3 Anticollision (`0x93`/`0x95`/`0x97`) & BCC XOR → `SELECT` → `SAK` discovery alongside a live **ST25R3916 ADC radar** showing real-time coupling strength (`0–100%` bar + `LOCK`/`SIG`/`METAL!` indicator) and source-specific center icons (NTAG chip, MIFARE/DESFire card, Encrypted padlock, Metal detune alert).
- **Color-Coded Memory & NDEF Inspector (`DUMP`):** Automatically dumps NFC Forum Type 2 (NTAG21x / MIFARE Ultralight) 4-byte pages via `READ (0x30)`:
  - **Cyan (`P00–P02`):** UID, BCC, Internal & Lock bytes
  - **Amber (`P03`):** Capability Container (`0xE1 ...`)
  - **Emerald (`P04+`):** User memory, NDEF TLV (`0x03`), and decoded URI/Text records
- **Hardware Card Emulation (`EMU`):** Uses ST25R3916 **Passive Target A (`0xA0`)** and **Passive Target F (`0xA8`, FeliCa 212/424 kbps)** memory for autonomous hardware responses (`REQA`/`WUPA`/`SENSF_REQ`), live **`HITS`** counter (`WUA`/`SDDC`/`WUF`), external reader RF field bar, random ID generator (`RND`), and instant `TYPE` switching (`NTAG215 7B` ↔ `MIFARE Classic 1K 4B` ↔ `FeliCa 8B IDm`). *(NFC-B and NFC-V are read-only per ST25R3916 silicon.)*
- **User `.nfc` Library (`SAVED`):** Manages your personal Flipper Zero Version 4 `.nfc` files strictly inside `~/nfc-studio/tags/` (with `[x]` multi-select via `Space`/`1`/`D` and single/batch delete).
- **Auto-Emu & External Databases (`PROBE`):** Step automatically through read-only external `.nfc` packs (`~/nfc-studio/databases/<pack>/` + `~/zerosub/nfc/`) or your `USER` library at a configurable dwell interval (`0.5s–8.0s`). Stop instantly (`4 STOP` / `Space` / `Enter`), backtrack with `F/X` using `>` / `*` sent markers, single-test (`5 TRY`), promote winners to `USER` (`6 SAVE`), or delete unwanted DB files (`1`/`D` + `Del`, recorded in `.sync-state.json` `ignored`).
- **Remote DB Sync & Source Manager (`SET` / `--sync`):** Configure GitHub or HTTPS JSON manifest `.nfc` sources in `~/.config/nfc-studio/sources.json`, run incremental `ETag`/`SHA` delta sync (`SET → Sync DB Sources` or `nfc-studio --sync`), and toggle or batch-delete pack folders & sources in `SET → Manage DB Sources`.
- **Automatic Hardware Setup:** Enables the Cap 5 V rail (`ext_5v_out` / GPIO26) and automatically loads `spi0-spidev2-gpio22-overlay` to expose `/dev/spidev0.2` (SPI Mode 1, 5 MHz). Falls back cleanly to **`SIM`** mode if hardware is absent.
- **NTAG21x Writer, Magic UID Cloner & NDEF Composer:** Write 4-byte user pages (`WRITE 0xA2`) with read-back verification and `ErrProtectedPage` lock/OTP/CC guards, clone 4-byte UIDs onto Gen1A (`0x40`/`0x43`) and Gen2/CUID Magic Cards with automatic BCC calculation, and compose URL, Text, or Wi-Fi WSC (`application/vnd.wfa.wsc`) NDEF TLVs.
- **MIFARE Classic Dictionary & Read-Only EMV Inspector:** Audit MIFARE Classic 1K/4K sectors against built-in default keys + `~/.config/nfc-studio/mf_keys.dict` (known-key dictionary only; no CRYPTO1 cracking) with `.nfc` v4 export, and inspect ISO-DEP (`SAK 0x20`) EMV cards via strictly read-only APDUs (`RATS` + `SELECT PPSE` + `READ RECORD`) with mandatory PAN masking.
- **Multi-Protocol Discovery (NFC-B / FeliCa / NFC-V):** Sequential ST25R3916 polling across ISO 14443-A, ISO 14443-B (`SENSB_REQ`), FeliCa (`SENSF_REQ`), and ISO 15693 (`INVENTORY` + `READ_SINGLE_BLOCK`) with dedicated radar glyphs and Flipper `.nfc` v4 serialization.
- **Inductive Metal Detector & Contactless Moisture Sensor:** Repurposes ST25R3916 `MeasureAmplitude (0xD3)` and `MeasurePhase (0xD5)` through a zero-allocation `InductiveSensor` to classify `AIR`, `FERROUS` (+L), `NON-FERROUS` (-L eddy current), `RESONANT-TAG`, and `WATER/DIELECTRIC` (`DRY`/`DAMP`/`WET`, `0–100%` moisture) with a 32-sample rolling sparkline.
- **Battery-Free NFC e-Paper Programmer (Waveshare e-Ink):** Renders custom 1-bit monochrome text badges (`2.13"` 250×122, `2.90"` 296×128, `1.54"` 200×200) using a built-in 5×7 font and uploads them over ISO-DEP (`RATS` + Waveshare `0xCD` APDU sequence) with progress reporting and `SIM` mode support.

---

## Keyboard Controls

| Key | Global / Context Action |
| :--- | :--- |
| **`Tab`** or **`8`** | Cycle tabs: `SCAN` → `DUMP` → `EMU` → `SAVED` |
| **`4` `5` `6` `7` `8`** | Bottom softkey bar actions (context-sensitive per tab) |
| **`F` / `X`** (or `↑` / `↓`) | Scroll hex pages (`DUMP`) or saved `.nfc` files (`SAVED`) |
| **`Z` / `C`** (or `←` / `→`) | Flip pages inside the Help overlay |
| **`Enter`** | Quick-emulate (`SCAN`), Start/Stop (`EMU`), or Emulate selected (`SAVED`) |
| **`Space`** | Pause/Resume scan (`SCAN`), Toggle HEX/NDEF (`DUMP`), Start/Stop (`EMU`) |
| **`H`** | Toggle 4-page built-in Help overlay |
| **`Q`** or **`Esc`** | Close Help or quit cleanly (turns off RF field) |

---

## CLI Flags & `--probe` Diagnostic Mode

```bash
# Probe ST25R3916 silicon, supply voltage, and poll 5x for an ISO 14443-A tag on stdout
nfc-studio --probe

# Run UI in offline simulator mode (preloads sample NTAG215 tag)
nfc-studio --sim

# Override device paths or saved tags directory
nfc-studio --spi /dev/spidev0.2 --fb /dev/fb0 --tags ~/nfc-studio/tags
```

Example `--probe` output:

```text
ST25R3916 OK: id=0x2D rev=5 supply=3346mV
TAG FOUND: UID=04A1B2C3D4E5F6 ATQA=4400 SAK=00 Pages=135
```

### Automatic SPI Overlay

On CardputerZero, the ST25R3916 sits on SPI0 Chip Select 2 (**GPIO22** → `/dev/spidev0.2`). If `/dev/spidev0.2` is not yet present at startup, `nfc-studio` invokes `dtoverlay spi0-spidev2-gpio22-overlay` (and falls back to `sudo -n dtoverlay spi0-spidev2-gpio22-overlay`). The Debian package installs `/etc/sudoers.d/nfc-studio` granting passwordless overlay loading to members of the `spi` and `gpio` groups.

---

## Build, Test & Packaging

```bash
# Run full unit test suite (driver, ISO 14443-A state machine, NDEF/.nfc parser, UI)
go test -v ./...

# Build arm64 binary and APPLaunch .deb package in dist/
./scripts/build_deb.sh
```

Deploy and install on CardputerZero:

```bash
scp dist/nfc-studio_0.1.0-1_arm64.deb cardputer@<CARDPUTER_IP>:~
ssh cardputer@<CARDPUTER_IP> "sudo dpkg -i ~/nfc-studio_0.1.0-1_arm64.deb"
```

After installation, **NFC Studio** appears automatically in **APPLaunch** (`/usr/share/APPLaunch/bin/nfc-studio`).

---

## Documentation & Interactive Learning Portal

Deep-dive guides, screenshots, protocol theory, and an interactive **ISO 14443-A & NDEF Inspector** lab are available in the live Learning Portal:

- [Learning Portal Home](https://igenexxx.github.io/cardputer-zero-learning-portal/#/home)
- [NFC Studio App Guide & Controls](https://igenexxx.github.io/cardputer-zero-learning-portal/#/nfc-guide)
- [NFC & ISO 14443-A Deep-Dive Theory](https://igenexxx.github.io/cardputer-zero-learning-portal/#/nfc-domain)
- [RF, ML & NFC Domain Glossary](https://igenexxx.github.io/cardputer-zero-learning-portal/#/glossary)

## Built-In MCP Server (SSH Opt-In)

The pure-Go stdio **MCP Server** is disabled by default (`~/.config/nfc-studio/config.json` -> `"mcp_enabled": false`). Enable **MCP Server (SSH)** in the **`SET`** tab, then connect from an AI agent over SSH:

```bash
ssh <cardputer-host> /usr/share/APPLaunch/bin/nfc-studio --mcp
```

Exposed tools (28): `nfc_get_status`, `nfc_scan_tag`, `nfc_scan_multiprotocol`, `nfc_write_nfcv` (`confirm_write: true`), `nfc_parse_ndef`, `nfc_compose_ndef` (`write_to_tag` requires `confirm_write: true`), `nfc_write_ntag` (`confirm_write: true`), `nfc_clone_magic_uid` (`confirm_write: true`), `nfc_dict_scan`, `nfc_inspect_emv` (strictly read-only, masked PAN), `nfc_emulate_tag` (`confirm_rf: true`), `nfc_inductive_sense` (calibrated ST25R3916 + **`NFC-InductiveNet`** 7-class material & moisture neural classifier), `nfc_epaper_push` (`confirm_rf: true`, Waveshare NFC e-Paper programmer), `nfc_measure_field`, `nfc_analyze_tag` (Shannon entropy + **`NFC-DumpNet`** 10-ecosystem neural classifier & 32D embedding), `nfc_match_fingerprint` (32D learned SupCon k-NN), `nfc_model_info` (inspect embedded/override `.czml` models in `~/.config/nfc-studio/models/`), `nfc_list_saved`, `nfc_read_saved`, `nfc_save_tag`, `nfc_delete_saved` (`confirm: true`), `nfc_list_databases`, `nfc_promote_to_user`, `nfc_list_sources`, `nfc_add_source`, `nfc_remove_source` (`confirm: true`, single/batch `ids[]`), `nfc_sync_sources`, `nfc_delete_pack_files` (`confirm: true`).
