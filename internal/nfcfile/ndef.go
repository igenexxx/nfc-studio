package nfcfile

import "fmt"

const (
	userMemStartPage = 4
	tlvNull          = 0x00
	tlvNDEF          = 0x03
	tlvTerminator    = 0xFE
	ndefFlagSR       = 0x10
	ndefFlagIL       = 0x08
	ndefLangLenMask  = 0x3F
)

var uriPrefixes = []string{
	"",
	"http://www.",
	"https://www.",
	"http://",
	"https://",
	"tel:",
	"mailto:",
	"ftp://anonymous:anonymous@",
	"ftp://ftp.",
	"ftps://",
	"sftp://",
	"smb://",
	"nfs://",
	"ftp://",
	"dav://",
	"news:",
	"telnet://",
	"imap:",
	"rtsp://",
	"urn:",
}

// NdefRecord represents a parsed NDEF URI or Text record.
type NdefRecord struct {
	Type    string // "URI" or "TEXT"
	Payload string
}

// ExtractNDEF scans NTAG user pages (starting at page 4) for an NDEF TLV (0x03)
// and decodes the first URI ('U') or Text ('T') record.
func ExtractNDEF(pages [][]byte) (NdefRecord, bool) {
	if len(pages) <= userMemStartPage {
		return NdefRecord{}, false
	}
	var raw []byte
	for _, pg := range pages[userMemStartPage:] {
		raw = append(raw, pg...)
	}
	// Find TLV type 0x03 (NDEF Message).
	for i := 0; i+2 < len(raw); {
		t := raw[i]
		if t == tlvNull {
			i++
			continue
		}
		if t == tlvTerminator {
			break
		}
		l := int(raw[i+1])
		i += 2
		if l == 0xFF {
			if i+2 > len(raw) {
				break
			}
			l = int(raw[i])<<8 | int(raw[i+1])
			i += 2
		}
		if t == tlvNDEF && i+l <= len(raw) {
			return parseNdefMessage(raw[i : i+l])
		}
		i += l
	}
	return NdefRecord{}, false
}

func parseNdefMessage(msg []byte) (NdefRecord, bool) {
	recType, payload, ok := parseNdefHeader(msg)
	if !ok {
		return NdefRecord{}, false
	}
	return decodeNdefPayload(recType, payload)
}

func parseNdefHeader(msg []byte) (string, []byte, bool) {
	if len(msg) < 4 {
		return "", nil, false
	}
	hdr := msg[0]
	typeLen := int(msg[1])
	idx := 2
	var payloadLen int
	if (hdr & ndefFlagSR) != 0 {
		if idx >= len(msg) {
			return "", nil, false
		}
		payloadLen = int(msg[idx])
		idx++
	} else {
		if idx+4 > len(msg) {
			return "", nil, false
		}
		payloadLen = int(msg[idx])<<24 | int(msg[idx+1])<<16 | int(msg[idx+2])<<8 | int(msg[idx+3])
		idx += 4
	}
	idLen := 0
	if (hdr & ndefFlagIL) != 0 {
		if idx >= len(msg) {
			return "", nil, false
		}
		idLen = int(msg[idx])
		idx++
	}
	if idx+typeLen+idLen+payloadLen > len(msg) || typeLen == 0 || payloadLen == 0 {
		return "", nil, false
	}
	recType := string(msg[idx : idx+typeLen])
	payload := msg[idx+typeLen+idLen : idx+typeLen+idLen+payloadLen]
	return recType, payload, true
}

func decodeNdefPayload(recType string, payload []byte) (NdefRecord, bool) {
	switch recType {
	case "U":
		prefixIdx := int(payload[0])
		prefix := ""
		if prefixIdx < len(uriPrefixes) {
			prefix = uriPrefixes[prefixIdx]
		}
		return NdefRecord{Type: "URI", Payload: prefix + string(payload[1:])}, true
	case "T":
		langLen := int(payload[0] & ndefLangLenMask)
		if 1+langLen <= len(payload) {
			return NdefRecord{Type: "TEXT", Payload: string(payload[1+langLen:])}, true
		}
		return NdefRecord{}, false
	default:
		return NdefRecord{Type: recType, Payload: fmt.Sprintf("%d bytes", len(payload))}, true
	}
}
