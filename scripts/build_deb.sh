#!/usr/bin/env bash
set -euo pipefail

PKG="nfc-studio"
VER="0.1.0"
REV="1"
ARCH="arm64"
DEB_NAME="${PKG}_${VER}-${REV}_${ARCH}.deb"

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

echo "[1/4] Running tests..."
go test ./...

echo "[2/4] Cross-compiling ${ARCH} binary..."
mkdir -p dist
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags="-s -w" -o "dist/${PKG}" ./cmd/nfc-studio

echo "[3/4] Staging Debian package..."
STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT

mkdir -p "$STAGE/DEBIAN"
mkdir -p "$STAGE/etc/sudoers.d"
mkdir -p "$STAGE/usr/share/APPLaunch/bin"
mkdir -p "$STAGE/usr/share/APPLaunch/applications"
mkdir -p "$STAGE/usr/share/APPLaunch/share/images"

cat > "$STAGE/etc/sudoers.d/${PKG}" <<EOF
%spi ALL=(root) NOPASSWD: /usr/bin/dtoverlay spi0-spidev2-gpio22-overlay
%gpio ALL=(root) NOPASSWD: /usr/bin/dtoverlay spi0-spidev2-gpio22-overlay
EOF
chmod 0440 "$STAGE/etc/sudoers.d/${PKG}"

install -m 0755 "dist/${PKG}" "$STAGE/usr/share/APPLaunch/bin/${PKG}"
install -m 0644 "assets/images/nfc-studio-icon.png" "$STAGE/usr/share/APPLaunch/share/images/nfc-studio-icon.png"

cat > "$STAGE/usr/share/APPLaunch/applications/${PKG}.desktop" <<EOF
[Desktop Entry]
Type=Application
Name=NFC Studio
Comment=ST25R3916 NFC scanner, hex/NDEF inspector & card emulator
Exec=/usr/share/APPLaunch/bin/${PKG}
Terminal=false
Sysplause=true
Icon=share/images/nfc-studio-icon.png
Categories=Utility;
StartupNotify=false
EOF

SIZE_KB="$(du -sk "$STAGE/usr" | awk '{print $1}')"
cat > "$STAGE/DEBIAN/control" <<EOF
Package: ${PKG}
Version: ${VER}-${REV}
Section: utils
Priority: optional
Architecture: ${ARCH}
Installed-Size: ${SIZE_KB}
Maintainer: Zhenya <zhenya@cardputer.cc>
Description: ST25R3916 NFC scanner, hex/NDEF inspector and card emulator
 NFC Studio unlocks the ST25R3916 13.56 MHz transceiver on CardputerZero
 Cap CC1101 & NFC with scanning, hex/NDEF inspection, hardware UID
 emulation, and Flipper-compatible .nfc file management.
EOF

echo "[4/4] Building dist/${DEB_NAME}..."
dpkg-deb --root-owner-group --build "$STAGE" "dist/${DEB_NAME}"
ls -lh "dist/${DEB_NAME}"
