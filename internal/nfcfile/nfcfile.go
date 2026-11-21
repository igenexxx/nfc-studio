package nfcfile

import (
	"fmt"
	"strconv"
	"strings"
)

// NfcTag represents a Flipper Zero compatible .nfc dump.
type NfcTag struct {
	FileType   string
	Version    int
	DeviceType string // e.g. "NTAG215", "Mifare Classic 1K", "ISO15693", "FeliCa"
	UID        []byte
	ATQA       [2]byte
	SAK        uint8
	PagesTotal int
	Pages      [][]byte // 4 bytes per page (NTAG/Ultralight) or 16-byte blocks
	MultiMeta
}

// IdentifyCard infers a human-readable card type from ATQA, SAK, and UID length.
func IdentifyCard(atqa [2]byte, sak uint8, uidLen int) string {
	switch {
	case atqa == [2]byte{0x44, 0x00} && sak == 0x00 && uidLen == 7:
		return "NTAG21x / MFU"
	case atqa == [2]byte{0x04, 0x00} && sak == 0x08:
		return "MIFARE Classic 1K"
	case atqa == [2]byte{0x02, 0x00} && sak == 0x18:
		return "MIFARE Classic 4K"
	case sak == 0x20:
		return "ISO14443-4 / DESFire"
	default:
		return "ISO14443-3A"
	}
}

// Parse parses a Flipper Zero .nfc text document.
func Parse(data string) (*NfcTag, error) {
	tag := &NfcTag{Version: 4}
	for rawLine := range strings.SplitSeq(data, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		if err := applyField(tag, strings.TrimSpace(k), strings.TrimSpace(v)); err != nil {
			return nil, err
		}
	}
	if tag.FileType == "" {
		return nil, fmt.Errorf("nfcfile: missing Filetype header")
	}
	if len(tag.UID) == 0 {
		return nil, fmt.Errorf("nfcfile: missing UID")
	}
	if tag.DeviceType == "" {
		tag.DeviceType = IdentifyCard(tag.ATQA, tag.SAK, len(tag.UID))
	}
	if tag.Protocol == "" {
		tag.Protocol = inferProtocolFromDeviceType(tag.DeviceType)
	}
	if tag.PagesTotal == 0 {
		tag.PagesTotal = len(tag.Pages)
	}
	return tag, nil
}

func applyField(tag *NfcTag, k, v string) error {
	if handled, err := applyMultiField(tag, k, v); handled {
		return err
	}
	switch {
	case k == "Filetype":
		tag.FileType = v
	case k == "Version":
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("nfcfile: invalid Version %q: %w", v, err)
		}
		tag.Version = n
	case k == "Device type":
		tag.DeviceType = v
	case k == "UID":
		b, err := parseHexBytes(v)
		if err != nil || len(b) == 0 {
			return fmt.Errorf("nfcfile: invalid UID %q", v)
		}
		tag.UID = b
	case k == "ATQA":
		b, err := parseHexBytes(v)
		if err != nil || len(b) != 2 {
			return fmt.Errorf("nfcfile: invalid ATQA %q", v)
		}
		tag.ATQA = [2]byte{b[0], b[1]}
	case k == "SAK":
		b, err := parseHexBytes(v)
		if err != nil || len(b) != 1 {
			return fmt.Errorf("nfcfile: invalid SAK %q", v)
		}
		tag.SAK = b[0]
	case k == "Pages total":
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("nfcfile: invalid Pages total %q: %w", v, err)
		}
		tag.PagesTotal = n
	case strings.HasPrefix(k, "Page ") || strings.HasPrefix(k, "Block "):
		b, hasUnknown, err := parseBlockHexBytes(v)
		if err != nil {
			return fmt.Errorf("nfcfile: invalid %s: %w", k, err)
		}
		tag.Pages = append(tag.Pages, b)
		if hasUnknown || len(tag.Unreadable) > 0 {
			for len(tag.Unreadable) < len(tag.Pages)-1 {
				tag.Unreadable = append(tag.Unreadable, false)
			}
			tag.Unreadable = append(tag.Unreadable, hasUnknown)
		}
	}
	return nil
}

// Format serializes an NfcTag into Flipper Zero .nfc format.
func (t *NfcTag) Format() string {
	var b strings.Builder
	ft := t.FileType
	if ft == "" {
		ft = "Flipper NFC device"
	}
	ver := t.Version
	if ver == 0 {
		ver = 4
	}
	dt := t.DeviceType
	if dt == "" {
		dt = IdentifyCard(t.ATQA, t.SAK, len(t.UID))
	}
	fmt.Fprintf(&b, "Filetype: %s\n", ft)
	fmt.Fprintf(&b, "Version: %d\n", ver)
	fmt.Fprintf(&b, "Device type: %s\n", dt)
	fmt.Fprintf(&b, "UID: %s\n", FormatHex(t.UID))
	if formatMultiProtocolBody(&b, t) {
		return b.String()
	}
	fmt.Fprintf(&b, "ATQA: %02X %02X\n", t.ATQA[0], t.ATQA[1])
	fmt.Fprintf(&b, "SAK: %02X\n", t.SAK)
	if len(t.Pages) > 0 {
		fmt.Fprintf(&b, "Pages total: %d\n", len(t.Pages))
		for i, pg := range t.Pages {
			fmt.Fprintf(&b, "Page %d: %s\n", i, FormatHex(pg))
		}
	}
	return b.String()
}

// FormatHex formats a byte slice as space-separated uppercase hex.
func FormatHex(data []byte) string {
	parts := make([]string, len(data))
	for i, v := range data {
		parts[i] = fmt.Sprintf("%02X", v)
	}
	return strings.Join(parts, " ")
}

func parseHexBytes(s string) ([]byte, error) {
	fields := strings.Fields(s)
	out := make([]byte, 0, len(fields))
	for _, f := range fields {
		v, err := strconv.ParseUint(f, 16, 8)
		if err != nil {
			return nil, err
		}
		out = append(out, uint8(v))
	}
	return out, nil
}
