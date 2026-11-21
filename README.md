# NFC Studio

Native 13.56 MHz HF NFC scanner, color-coded hex/NDEF memory inspector, hardware Passive Target UID emulator, and Flipper-compatible `.nfc` library for the **M5Stack CardputerZero** + **Cap CC1101 & NFC** (**ST25R3916**).

Built in pure Go (`CGO_ENABLED=0`, zero external runtime dependencies) rendering directly to `/dev/fb0` (320×170 RGB565) with `evdev` keyboard input.

> **Hardware Safety:** Never hot-swap the Cap CC1101 & NFC module while CardputerZero is powered on.

---

## Features

- **Live ISO 14443-A Scanner (`SCAN`):** Background 180 ms hardware poller performing full `WUPA` → Cascade Level 1–3 Anticollision (`0x93`/`0x95`/`0x97`) & BCC XOR → `SELECT` → `SAK` discovery with live **`DETECTED`** indicator and NDEF preview.
- **Color-Coded Memory & NDEF Inspector (`DUMP`):** Automatically dumps NFC Forum Type 2 (NTAG21x / MIFARE Ultralight) 4-byte pages via `READ (0x30)`:
  - **Cyan (`P00–P02`):** UID, BCC, Internal & Lock bytes
  - **Amber (`P03`):** Capability Container (`0xE1 ...`)
  - **Emerald (`P04+`):** User memory, NDEF TLV (`0x03`), and decoded URI/Text records
- **Hardware UID Card Emulation (`EMU`):** Uses ST25R3916 **Passive Target A (`0xA0`)** memory for autonomous hardware responses to external readers (`REQA`/`WUPA`/Anticollision/`SELECT`), live **`HITS`** counter, random UID generator (`RND`), and instant `4B/7B` (MIFARE Classic 1K ↔ NTAG215) switching.
- **Flipper-Compatible `.nfc` Library (`SAVED`):** Exports and loads standard Flipper Zero Version 4 `.nfc` files from `~/nfc-studio/tags/` and `~/zerosub/nfc/`.
- **Automatic Hardware Setup:** Enables the Cap 5 V rail (`ext_5v_out` / GPIO26) and automatically loads `spi0-spidev2-gpio22-overlay` to expose `/dev/spidev0.2` (SPI Mode 1, 5 MHz). Falls back cleanly to **`SIM`** mode if hardware is absent.

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

Exposed tools: `nfc_get_status`, `nfc_scan_tag`, `nfc_parse_ndef`, `nfc_emulate_tag` (`confirm_rf: true`), `nfc_list_saved`, `nfc_read_saved`, `nfc_save_tag`, `nfc_delete_saved` (`confirm: true`).
