package nfcfile

import (
	"fmt"
	"strconv"
	"strings"
)

// MultiMeta holds optional metadata for non-NTAG / multi-protocol .nfc v4 files.
type MultiMeta struct {
	Protocol     string  // "", "NFC-A", "NFC-B", "NFC-F", "NFC-V"
	MifareType   string  // "1K" or "4K" for Mifare Classic
	AppData      [4]byte // ISO14443-3B Application Data
	ProtocolInfo [3]byte // ISO14443-3B Protocol Info
	PMm          [8]byte // FeliCa Manufacturer (PMm)
	SystemCode   [2]byte // FeliCa System Code
	DSFID        uint8   // ISO15693-3 DSFID
	Unreadable   []bool  // Marks unreadable MIFARE Classic blocks formatted as "??"
}

func applyMultiField(tag *NfcTag, k, v string) (bool, error) {
	switch k {
	case "Mifare Classic type":
		tag.MifareType = v
		return true, nil
	case "Application Data":
		b, err := parseHexBytes(v)
		if err != nil || len(b) != 4 {
			return true, fmt.Errorf("nfcfile: invalid Application Data %q", v)
		}
		copy(tag.AppData[:], b)
		tag.Protocol = "NFC-B"
		return true, nil
	case "Protocol Info":
		b, err := parseHexBytes(v)
		if err != nil || len(b) != 3 {
			return true, fmt.Errorf("nfcfile: invalid Protocol Info %q", v)
		}
		copy(tag.ProtocolInfo[:], b)
		tag.Protocol = "NFC-B"
		return true, nil
	case "Manufacturer":
		b, err := parseHexBytes(v)
		if err != nil || len(b) != 8 {
			return true, fmt.Errorf("nfcfile: invalid Manufacturer %q", v)
		}
		copy(tag.PMm[:], b)
		tag.Protocol = "NFC-F"
		return true, nil
	case "System Code":
		b, err := parseHexBytes(v)
		if err != nil || len(b) != 2 {
			return true, fmt.Errorf("nfcfile: invalid System Code %q", v)
		}
		copy(tag.SystemCode[:], b)
		tag.Protocol = "NFC-F"
		return true, nil
	case "DSFID":
		b, err := parseHexBytes(v)
		if err != nil || len(b) != 1 {
			return true, fmt.Errorf("nfcfile: invalid DSFID %q", v)
		}
		tag.DSFID = b[0]
		tag.Protocol = "NFC-V"
		return true, nil
	case "Block Count":
		n, err := strconv.Atoi(v)
		if err != nil {
			return true, fmt.Errorf("nfcfile: invalid Block Count %q: %w", v, err)
		}
		tag.PagesTotal = n
		return true, nil
	default:
		return false, nil
	}
}

func inferProtocolFromDeviceType(dt string) string {
	upper := strings.ToUpper(dt)
	switch {
	case strings.Contains(upper, "14443-3B") || strings.Contains(upper, "NFC-B"):
		return "NFC-B"
	case strings.Contains(upper, "FELICA") || strings.Contains(upper, "NFC-F"):
		return "NFC-F"
	case strings.Contains(upper, "15693") || strings.Contains(upper, "NFC-V"):
		return "NFC-V"
	default:
		return ""
	}
}

func formatMultiProtocolBody(b *strings.Builder, t *NfcTag) bool {
	proto := t.Protocol
	if proto == "" {
		proto = inferProtocolFromDeviceType(t.DeviceType)
	}
	switch proto {
	case "NFC-B":
		fmt.Fprintf(b, "Application Data: %s\n", FormatHex(t.AppData[:]))
		fmt.Fprintf(b, "Protocol Info: %s\n", FormatHex(t.ProtocolInfo[:]))
		return true
	case "NFC-F":
		fmt.Fprintf(b, "Manufacturer: %s\n", FormatHex(t.PMm[:]))
		fmt.Fprintf(b, "System Code: %s\n", FormatHex(t.SystemCode[:]))
		return true
	case "NFC-V":
		fmt.Fprintf(b, "DSFID: %02X\n", t.DSFID)
		fmt.Fprintf(b, "Block Count: %d\n", len(t.Pages))
		for i, blk := range t.Pages {
			fmt.Fprintf(b, "Block %d: %s\n", i, FormatHex(blk))
		}
		return true
	}
	if t.MifareType != "" {
		fmt.Fprintf(b, "ATQA: %02X %02X\n", t.ATQA[0], t.ATQA[1])
		fmt.Fprintf(b, "SAK: %02X\n", t.SAK)
		fmt.Fprintf(b, "Mifare Classic type: %s\n", t.MifareType)
		fmt.Fprintf(b, "Data format version: 2\n")
		for i, blk := range t.Pages {
			if i < len(t.Unreadable) && t.Unreadable[i] {
				fmt.Fprintf(b, "Block %d: %s\n", i, unknownHexBlock(len(blk)))
			} else {
				fmt.Fprintf(b, "Block %d: %s\n", i, FormatHex(blk))
			}
		}
		return true
	}
	return false
}

func unknownHexBlock(n int) string {
	if n <= 0 {
		n = 16
	}
	parts := make([]string, n)
	for i := range n {
		parts[i] = "??"
	}
	return strings.Join(parts, " ")
}

func parseBlockHexBytes(s string) ([]byte, bool, error) {
	fields := strings.Fields(s)
	out := make([]byte, 0, len(fields))
	hasUnknown := false
	for _, f := range fields {
		if f == "??" {
			hasUnknown = true
			out = append(out, 0x00)
			continue
		}
		v, err := strconv.ParseUint(f, 16, 8)
		if err != nil {
			return nil, false, err
		}
		out = append(out, uint8(v))
	}
	return out, hasUnknown, nil
}
