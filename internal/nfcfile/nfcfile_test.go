package nfcfile

import (
	"strings"
	"testing"
)

func TestParseAndFormatNfc(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantErr   bool
		wantType  string
		wantPages int
	}{
		{
			name: "valid NTAG215",
			input: `Filetype: Flipper NFC device
Version: 4
Device type: NTAG215
UID: 04 A1 B2 C3 D4 E5 F6
ATQA: 44 00
SAK: 00
Pages total: 6
Page 0: 04 A1 B2 77
Page 1: C3 D4 E5 F6
Page 2: 11 48 00 00
Page 3: E1 10 3E 00
Page 4: 03 10 D1 01
Page 5: 0C 55 04 63
`,
			wantType:  "NTAG215",
			wantPages: 6,
		},
		{
			name:    "missing filetype",
			input:   "UID: 04 A1 B2 C3\n",
			wantErr: true,
		},
		{
			name:    "missing UID",
			input:   "Filetype: Flipper NFC device\nATQA: 44 00\n",
			wantErr: true,
		},
		{
			name:    "bad ATQA",
			input:   "Filetype: Flipper NFC device\nUID: 04 A1 B2 C3\nATQA: ZZ\n",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tag, err := Parse(tc.input)
			if (err != nil) != tc.wantErr {
				t.Fatalf("Parse err=%v wantErr=%v", err, tc.wantErr)
			}
			if tc.wantErr {
				return
			}
			if tag.DeviceType != tc.wantType || len(tag.Pages) != tc.wantPages {
				t.Fatalf("got type=%q pages=%d", tag.DeviceType, len(tag.Pages))
			}
			out := tag.Format()
			if !strings.Contains(out, "UID:") {
				t.Fatalf("Format missing UID: %s", out)
			}
			tag2, err := Parse(out)
			if err != nil || len(tag2.UID) != len(tag.UID) {
				t.Fatalf("round-trip failed: %v", err)
			}
		})
	}
}

func TestExtractNDEF(t *testing.T) {
	// NDEF URI record: https://cardputer.cc (prefix 0x04 = https://)
	pages := [][]byte{
		{0x04, 0xA1, 0xB2, 0x77},
		{0xC3, 0xD4, 0xE5, 0xF6},
		{0x11, 0x48, 0x00, 0x00},
		{0xE1, 0x10, 0x3E, 0x00},
		{0x03, 0x11, 0xD1, 0x01},
		{0x0D, 0x55, 0x04, 'c'},
		{'a', 'r', 'd', 'p'},
		{'u', 't', 'e', 'r'},
		{'.', 'c', 'c', 0xFE},
	}
	rec, ok := ExtractNDEF(pages)
	if !ok {
		t.Fatal("expected NDEF extraction to succeed")
	}
	if rec.Type != "URI" || rec.Payload != "https://cardputer.cc" {
		t.Fatalf("got %+v want URI https://cardputer.cc", rec)
	}
}

func FuzzParseNfcFile(f *testing.F) {
	f.Add("Filetype: Flipper NFC device\nVersion: 4\nUID: 04 11 22 33\nATQA: 44 00\nSAK: 00\n")
	f.Fuzz(func(t *testing.T, s string) {
		tag, err := Parse(s)
		if err == nil {
			_ = tag.Format()
		}
	})
}
