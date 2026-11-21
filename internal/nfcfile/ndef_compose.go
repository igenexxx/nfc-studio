package nfcfile

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
)

// ErrNDEFOversize is returned when an encoded NDEF TLV exceeds target tag user memory.
var ErrNDEFOversize = errors.New("nfcfile: NDEF payload exceeds tag user memory")

// WiFiAuth represents the WPS/WSC Authentication Type attribute (0x1003).
type WiFiAuth uint16

const (
	WiFiAuthOpen WiFiAuth = 0x0001
	WiFiAuthWPA2 WiFiAuth = 0x0020
	WiFiAuthWPA3 WiFiAuth = 0x0040

	wscAttrCredential  uint16 = 0x100E
	wscAttrSSID        uint16 = 0x1045
	wscAttrAuthType    uint16 = 0x1003
	wscAttrEncType     uint16 = 0x100F
	wscAttrNetworkKey  uint16 = 0x1027
	wscEncNone         uint16 = 0x0001
	wscEncAES          uint16 = 0x0008
	wscMimeType               = "application/vnd.wfa.wsc"
	ndefFlagMBME       uint8  = 0xC0
	ndefTNFWellKnown   uint8  = 0x01
	ndefTNFMime        uint8  = 0x02
	maxShortPayloadLen        = 255
	maxShortTLVLen            = 254
	maxLangCodeLen            = 63
)

// WiFiConfig describes Wi-Fi credentials to encode into an NFC Forum WSC MIME record.
type WiFiConfig struct {
	SSID     string
	Password string
	Auth     WiFiAuth
}

// ComposeURL encodes a URL into a 4-byte-padded NDEF Type 2 TLV (0x03 ... 0xFE).
func ComposeURL(rawURL string) ([]byte, error) {
	return EncodeNDEFURI(rawURL)
}

// EncodeNDEFURI encodes a URL with NFC Forum URI prefix compression into a padded NDEF TLV.
func EncodeNDEFURI(rawURL string) ([]byte, error) {
	if rawURL == "" {
		return nil, errors.New("nfcfile: URL must not be empty")
	}
	code, suffix := matchURIPrefix(rawURL)
	payload := make([]byte, 1+len(suffix))
	payload[0] = code
	copy(payload[1:], suffix)

	rec := buildNDEFRecord(ndefTNFWellKnown, "U", payload)
	return wrapNDEFTLV(rec), nil
}

// ComposeText encodes a UTF-8 text string with optional ISO language code into a padded NDEF TLV.
func ComposeText(lang, text string) ([]byte, error) {
	return EncodeNDEFText(lang, text)
}

// EncodeNDEFText encodes a Text ('T') NDEF record wrapped in a 4-byte-padded Type 2 TLV.
func EncodeNDEFText(lang, text string) ([]byte, error) {
	if text == "" {
		return nil, errors.New("nfcfile: text must not be empty")
	}
	if lang == "" {
		lang = "en"
	}
	if len(lang) > maxLangCodeLen {
		return nil, fmt.Errorf("nfcfile: language code length %d exceeds %d", len(lang), maxLangCodeLen)
	}
	payload := make([]byte, 1+len(lang)+len(text))
	payload[0] = uint8(len(lang)) & ndefLangLenMask
	copy(payload[1:], lang)
	copy(payload[1+len(lang):], text)

	rec := buildNDEFRecord(ndefTNFWellKnown, "T", payload)
	return wrapNDEFTLV(rec), nil
}

// ComposeWiFi encodes Wi-Fi credentials into an NFC Forum WSC MIME NDEF TLV.
func ComposeWiFi(cfg WiFiConfig) ([]byte, error) {
	return EncodeNDEFWiFi(cfg)
}

// EncodeNDEFWiFi encodes a Wi-Fi Simple Configuration (application/vnd.wfa.wsc) NDEF TLV.
func EncodeNDEFWiFi(cfg WiFiConfig) ([]byte, error) {
	if cfg.SSID == "" {
		return nil, errors.New("nfcfile: Wi-Fi SSID must not be empty")
	}
	auth := cfg.Auth
	if auth == 0 {
		auth = WiFiAuthWPA2
	}
	enc := wscEncAES
	if auth == WiFiAuthOpen {
		enc = wscEncNone
	}

	var inner []byte
	inner = appendWSCAttr(inner, wscAttrSSID, []byte(cfg.SSID))
	inner = appendWSCUint16(inner, wscAttrAuthType, uint16(auth))
	inner = appendWSCUint16(inner, wscAttrEncType, enc)
	if cfg.Password != "" {
		inner = appendWSCAttr(inner, wscAttrNetworkKey, []byte(cfg.Password))
	}
	cred := appendWSCAttr(nil, wscAttrCredential, inner)

	rec := buildNDEFRecord(ndefTNFMime, wscMimeType, cred)
	return wrapNDEFTLV(rec), nil
}

// PackNDEFPages chunks a TLV byte slice into 4-byte NTAG user pages, enforcing capacity.
func PackNDEFPages(tlv []byte, maxUserPages int) ([][4]byte, error) {
	if len(tlv) == 0 {
		return nil, errors.New("nfcfile: empty NDEF TLV")
	}
	if maxUserPages <= 0 {
		return nil, ErrNDEFOversize
	}
	numPages := (len(tlv) + 3) / 4
	if numPages > maxUserPages {
		return nil, fmt.Errorf("%w: need %d pages, max %d", ErrNDEFOversize, numPages, maxUserPages)
	}
	pages := make([][4]byte, numPages)
	for i := range numPages {
		start := i * 4
		end := min(start+4, len(tlv))
		copy(pages[i][:], tlv[start:end])
	}
	return pages, nil
}

func matchURIPrefix(rawURL string) (uint8, string) {
	bestCode := uint8(0)
	bestLen := 0
	for i := 1; i < len(uriPrefixes); i++ {
		p := uriPrefixes[i]
		if len(p) > bestLen && strings.HasPrefix(rawURL, p) {
			bestCode = uint8(i)
			bestLen = len(p)
		}
	}
	return bestCode, rawURL[bestLen:]
}

func buildNDEFRecord(tnf uint8, recType string, payload []byte) []byte {
	hdr := ndefFlagMBME | (tnf & 0x07)
	short := len(payload) <= maxShortPayloadLen
	if short {
		hdr |= ndefFlagSR
	}
	out := make([]byte, 0, 6+len(recType)+len(payload))
	out = append(out, hdr, uint8(len(recType)))
	if short {
		out = append(out, uint8(len(payload)))
	} else {
		var lenBuf [4]byte
		binary.BigEndian.PutUint32(lenBuf[:], uint32(len(payload)))
		out = append(out, lenBuf[:]...)
	}
	out = append(out, recType...)
	out = append(out, payload...)
	return out
}

func wrapNDEFTLV(msg []byte) []byte {
	var tlv []byte
	if len(msg) <= maxShortTLVLen {
		tlv = make([]byte, 0, 2+len(msg)+1+3)
		tlv = append(tlv, tlvNDEF, uint8(len(msg)))
	} else {
		tlv = make([]byte, 0, 4+len(msg)+1+3)
		tlv = append(tlv, tlvNDEF, 0xFF, uint8(len(msg)>>8), uint8(len(msg)&0xFF))
	}
	tlv = append(tlv, msg...)
	tlv = append(tlv, tlvTerminator)
	for len(tlv)%4 != 0 {
		tlv = append(tlv, tlvNull)
	}
	return tlv
}

func appendWSCAttr(dst []byte, attr uint16, val []byte) []byte {
	var hdr [4]byte
	binary.BigEndian.PutUint16(hdr[0:2], attr)
	binary.BigEndian.PutUint16(hdr[2:4], uint16(len(val)))
	dst = append(dst, hdr[:]...)
	return append(dst, val...)
}

func appendWSCUint16(dst []byte, attr, val uint16) []byte {
	var v [2]byte
	binary.BigEndian.PutUint16(v[:], val)
	return appendWSCAttr(dst, attr, v[:])
}
