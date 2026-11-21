package nfcfile

import (
	"errors"
	"fmt"
	"strings"
)

const (
	emvConstructedMask uint8 = 0x20
	emvMultiByteTag    uint8 = 0x1F
	maxTLVDepth              = 8
)

// EMVApp represents public read-only metadata for a contactless EMV payment application.
type EMVApp struct {
	AID       string `json:"aid"`
	Label     string `json:"label"`
	MaskedPAN string `json:"masked_pan,omitempty"`
	Expiry    string `json:"expiry,omitempty"` // "YY/MM"
}

// EMVInfo summarizes a read-only ISO-DEP / EMV contactless inspection.
type EMVInfo struct {
	UIDHex string   `json:"uid_hex"`
	ATSHex string   `json:"ats_hex"`
	Apps   []EMVApp `json:"apps"`
}

// ParseEMVTLV parses EMV BER-TLV structures recursively into a map keyed by 1- or 2-byte tag.
func ParseEMVTLV(data []byte) (map[uint16][]byte, error) {
	out := make(map[uint16][]byte)
	if len(data) == 0 {
		return out, nil
	}
	if err := parseTLVDepth(data, out, 0); err != nil {
		return out, err
	}
	return out, nil
}

func parseTLVDepth(data []byte, out map[uint16][]byte, depth int) error {
	if depth > maxTLVDepth {
		return errors.New("nfcfile: EMV TLV nesting depth exceeded")
	}
	for i := 0; i < len(data); {
		if data[i] == 0x00 || data[i] == 0xFF {
			i++
			continue
		}
		tag, firstByte, next, err := readTLVTag(data, i)
		if err != nil {
			return err
		}
		length, valStart, err := readTLVLength(data, next)
		if err != nil {
			return err
		}
		if valStart+length > len(data) {
			return fmt.Errorf("nfcfile: truncated TLV value for tag 0x%X", tag)
		}
		val := data[valStart : valStart+length]
		if _, exists := out[tag]; !exists {
			out[tag] = append([]byte(nil), val...)
		}
		if (firstByte & emvConstructedMask) != 0 {
			_ = parseTLVDepth(val, out, depth+1)
		}
		i = valStart + length
	}
	return nil
}

func readTLVTag(data []byte, i int) (tag uint16, first uint8, next int, err error) {
	first = data[i]
	i++
	if (first & emvMultiByteTag) != emvMultiByteTag {
		return uint16(first), first, i, nil
	}
	if i >= len(data) {
		return 0, 0, i, errors.New("nfcfile: truncated multi-byte TLV tag")
	}
	second := data[i]
	i++
	// Skip any 3rd+ tag bytes if continuation bit is set.
	for (second&0x80) != 0 && i < len(data) {
		second = data[i]
		i++
	}
	return (uint16(first) << 8) | uint16(second), first, i, nil
}

func readTLVLength(data []byte, i int) (length int, next int, err error) {
	if i >= len(data) {
		return 0, i, errors.New("nfcfile: truncated TLV length")
	}
	b := data[i]
	i++
	if (b & 0x80) == 0 {
		return int(b), i, nil
	}
	numBytes := int(b & 0x7F)
	if numBytes == 0 || numBytes > 2 || i+numBytes > len(data) {
		return 0, i, fmt.Errorf("nfcfile: unsupported TLV length encoding 0x%02X", b)
	}
	for range numBytes {
		length = (length << 8) | int(data[i])
		i++
	}
	return length, i, nil
}

// MaskPAN masks a numeric PAN to BIN(6) + "*"*(len-10) + Last4.
// PANs shorter than 10 digits are fully masked as "****".
func MaskPAN(digits string) string {
	var clean strings.Builder
	for i := range len(digits) {
		c := digits[i]
		if c >= '0' && c <= '9' {
			clean.WriteByte(c)
		}
	}
	s := clean.String()
	if len(s) < 10 {
		return "****"
	}
	return s[:6] + strings.Repeat("*", len(s)-10) + s[len(s)-4:]
}

// DecodeBCDDigits unpacks BCD nibbles (stopping at 'F' or 'D' separator) into ASCII digits.
func DecodeBCDDigits(b []byte) string {
	var sb strings.Builder
	for _, v := range b {
		hi := v >> 4
		lo := v & 0x0F
		if hi > 9 {
			break
		}
		sb.WriteByte('0' + hi)
		if lo > 9 {
			break
		}
		sb.WriteByte('0' + lo)
	}
	return sb.String()
}

// ExtractTrack2Masked extracts a masked PAN and YY/MM expiry from EMV Track 2 Equivalent Data (tag 0x57).
func ExtractTrack2Masked(track2 []byte) (maskedPAN string, expiry string) {
	hexStr := strings.ToUpper(fmt.Sprintf("%x", track2))
	panPart, rest, ok := strings.Cut(hexStr, "D")
	if !ok {
		return MaskPAN(panPart), ""
	}
	maskedPAN = MaskPAN(panPart)
	if len(rest) >= 4 {
		expiry = rest[0:2] + "/" + rest[2:4]
	}
	return maskedPAN, expiry
}

// FormatEMVExpiry converts EMV tag 0x5F24 (YYMMDD BCD) into "YY/MM".
func FormatEMVExpiry(raw []byte) string {
	digits := DecodeBCDDigits(raw)
	if len(digits) < 4 {
		return ""
	}
	return digits[0:2] + "/" + digits[2:4]
}
