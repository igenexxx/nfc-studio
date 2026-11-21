package nfcfile

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestNDEFComposeRoundTripAndLimits(t *testing.T) {
	t.Run("URI prefix compression and round-trip", func(t *testing.T) {
		tlv, err := ComposeURL("https://cardputer.cc/nfc")
		if err != nil {
			t.Fatalf("ComposeURL() error = %v", err)
		}
		pages, err := PackNDEFPages(tlv, 36)
		if err != nil {
			t.Fatalf("PackNDEFPages() error = %v", err)
		}
		all := make([][]byte, 4+len(pages))
		for i := range 4 {
			all[i] = make([]byte, 4)
		}
		for i, p := range pages {
			all[4+i] = append([]byte(nil), p[:]...)
		}
		rec, ok := ExtractNDEF(all)
		if !ok || rec.Type != "URI" || rec.Payload != "https://cardputer.cc/nfc" {
			t.Fatalf("ExtractNDEF() = (%+v, %v)", rec, ok)
		}
	})

	t.Run("Text long record (>255B) round-trip", func(t *testing.T) {
		longText := strings.Repeat("A", 270)
		tlv, err := ComposeText("en", longText)
		if err != nil {
			t.Fatalf("ComposeText() error = %v", err)
		}
		if tlv[0] != 0x03 || tlv[1] != 0xFF {
			t.Fatalf("expected 3-byte TLV length for >254B NDEF, got %X", tlv[:4])
		}
		pages, err := PackNDEFPages(tlv, 126)
		if err != nil {
			t.Fatalf("PackNDEFPages() error = %v", err)
		}
		all := make([][]byte, 4+len(pages))
		for i := range 4 {
			all[i] = make([]byte, 4)
		}
		for i, p := range pages {
			all[4+i] = append([]byte(nil), p[:]...)
		}
		rec, ok := ExtractNDEF(all)
		if !ok || rec.Type != "TEXT" || rec.Payload != longText {
			t.Fatalf("long Text round-trip failed: ok=%v type=%s len=%d", ok, rec.Type, len(rec.Payload))
		}
	})

	t.Run("WiFi WSC encoding and oversize guard", func(t *testing.T) {
		tlv, err := ComposeWiFi(WiFiConfig{SSID: "LabWiFi", Password: "secret123", Auth: WiFiAuthWPA2})
		if err != nil {
			t.Fatalf("ComposeWiFi() error = %v", err)
		}
		if !bytes.Contains(tlv, []byte("application/vnd.wfa.wsc")) || !bytes.Contains(tlv, []byte("LabWiFi")) {
			t.Fatalf("missing WSC MIME or SSID in %X", tlv)
		}
		if _, err := PackNDEFPages(make([]byte, 148), 36); !errors.Is(err, ErrNDEFOversize) {
			t.Fatalf("expected ErrNDEFOversize, got %v", err)
		}
		if _, err := ComposeURL(""); err == nil {
			t.Fatal("expected error for empty URL")
		}
		if _, err := ComposeWiFi(WiFiConfig{}); err == nil {
			t.Fatal("expected error for empty SSID")
		}
	})
}

func FuzzNDEFEncodeRoundTrip(f *testing.F) {
	f.Add("https://example.com")
	f.Add("http://www.test.org/a")
	f.Fuzz(func(t *testing.T, url string) {
		if url == "" {
			return
		}
		tlv, err := ComposeURL(url)
		if err != nil {
			return
		}
		pages, err := PackNDEFPages(tlv, 2000)
		if err != nil {
			t.Fatalf("PackNDEFPages error: %v", err)
		}
		all := make([][]byte, 4+len(pages))
		for i := range 4 {
			all[i] = make([]byte, 4)
		}
		for i, p := range pages {
			all[4+i] = append([]byte(nil), p[:]...)
		}
		rec, ok := ExtractNDEF(all)
		if !ok || rec.Payload != url {
			t.Fatalf("round-trip mismatch: got (%q, %v) want %q", rec.Payload, ok, url)
		}
	})
}

func TestMifareKeyDict(t *testing.T) {
	if len(DefaultMifareKeys()) != 13 {
		t.Fatalf("DefaultMifareKeys() len = %d, want 13", len(DefaultMifareKeys()))
	}
	raw := "# comment\r\n  FF FF FF FF FF FF  \r\nAA:BB:CC:DD:EE:01 # inline\nINVALID\n1234567890\nAA-BB-CC-DD-EE-01\n"
	keys, err := ParseMifareKeyDict(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("ParseMifareKeyDict() error = %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("got %d unique keys, want 2", len(keys))
	}
	loaded, err := LoadMifareKeyDict("/nonexistent/path/mf_keys.dict")
	if err != nil || len(loaded) != 13 {
		t.Fatalf("missing dict file should return 13 defaults without error, got (%d, %v)", len(loaded), err)
	}
}

func TestEMVTLVAndPANMasking(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"4111111111111111", "411111******1111"},
		{"4111 1122 3344 5566", "411111******5566"},
		{"123456789", "****"},
		{"", "****"},
	}
	for _, tc := range tests {
		if got := MaskPAN(tc.in); got != tc.want {
			t.Fatalf("MaskPAN(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}

	tlv := []byte{
		0x6F, 0x10,
		0x4F, 0x07, 0xA0, 0x00, 0x00, 0x00, 0x03, 0x10, 0x10,
		0x5F, 0x24, 0x03, 0x28, 0x12, 0x31, 0x00,
	}
	m, err := ParseEMVTLV(tlv)
	if err != nil {
		t.Fatalf("ParseEMVTLV() error = %v", err)
	}
	if FormatEMVExpiry(m[0x5F24]) != "28/12" || len(m[0x4F]) != 7 {
		t.Fatalf("unexpected parsed EMV TLV: %+v", m)
	}
}

func FuzzParseEMVTLV(f *testing.F) {
	f.Add([]byte{0x6F, 0x04, 0x50, 0x02, 'O', 'K'})
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = ParseEMVTLV(data)
	})
}

func TestMultiProtocolNFCFormatRoundTrip(t *testing.T) {
	cases := []*NfcTag{
		{
			DeviceType: "ISO14443-3B",
			UID:        []byte{0x12, 0x34, 0x56, 0x78},
			MultiMeta:  MultiMeta{Protocol: "NFC-B", AppData: [4]byte{1, 2, 3, 4}, ProtocolInfo: [3]byte{0x00, 0x81, 0x71}},
		},
		{
			DeviceType: "FeliCa",
			UID:        []byte{1, 2, 3, 4, 5, 6, 7, 8},
			MultiMeta:  MultiMeta{Protocol: "NFC-F", PMm: [8]byte{0, 0xF1, 0, 0, 0, 1, 0x43, 0}, SystemCode: [2]byte{0x88, 0xB4}},
		},
		{
			DeviceType: "ISO15693-3",
			UID:        []byte{0xE0, 0x04, 0x01, 0x50, 0x12, 0x34, 0x56, 0x78},
			Pages:      [][]byte{{0x01, 0x02, 0x03, 0x04}},
			MultiMeta:  MultiMeta{Protocol: "NFC-V", DSFID: 0x0A},
		},
	}
	for _, tc := range cases {
		raw := tc.Format()
		parsed, err := Parse(raw)
		if err != nil {
			t.Fatalf("%s Parse() error = %v\n%s", tc.DeviceType, err, raw)
		}
		if parsed.Protocol != tc.Protocol || !bytes.Equal(parsed.UID, tc.UID) {
			t.Fatalf("%s round-trip mismatch: got proto=%q uid=%X", tc.DeviceType, parsed.Protocol, parsed.UID)
		}
	}
}
