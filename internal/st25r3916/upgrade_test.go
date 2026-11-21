package st25r3916

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func newTestChipWithEvents(t *testing.T, events []rxEvent) (*Chip, *mockTransport) {
	t.Helper()
	tr := &mockTransport{id: 0x2D, adc: 141, events: events}
	chip, err := Open(tr)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	chip.pollStep = 0
	chip.guardT = 0
	return chip, tr
}

func TestIdentifyNTAGModel(t *testing.T) {
	tests := []struct {
		name      string
		cc        [4]byte
		wantName  string
		wantEnd   uint8
		wantError bool
	}{
		{name: "NTAG213", cc: [4]byte{0xE1, 0x10, 0x12, 0x00}, wantName: "NTAG213", wantEnd: 39},
		{name: "NTAG215", cc: [4]byte{0xE1, 0x10, 0x3E, 0x00}, wantName: "NTAG215", wantEnd: 129},
		{name: "NTAG216", cc: [4]byte{0xE1, 0x10, 0x6D, 0x00}, wantName: "NTAG216", wantEnd: 225},
		{name: "bad magic", cc: [4]byte{0x00, 0x10, 0x3E, 0x00}, wantError: true},
		{name: "unknown size", cc: [4]byte{0xE1, 0x10, 0xFF, 0x00}, wantError: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m, err := IdentifyNTAGModel(tc.cc)
			if tc.wantError {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil || m.Name != tc.wantName || m.UserEndPage != tc.wantEnd {
				t.Fatalf("IdentifyNTAGModel() = (%+v, %v), want (%s, end=%d)", m, err, tc.wantName, tc.wantEnd)
			}
		})
	}
}

func TestBuildBlock0(t *testing.T) {
	blk, err := BuildBlock0([]byte{0xDE, 0xAD, 0xBE, 0xEF}, 0x08, [2]byte{0x04, 0x00})
	if err != nil {
		t.Fatalf("BuildBlock0() error = %v", err)
	}
	wantBCC := byte(0xDE ^ 0xAD ^ 0xBE ^ 0xEF)
	if blk[4] != wantBCC || blk[5] != 0x08 || blk[6] != 0x04 || blk[7] != 0x00 {
		t.Fatalf("unexpected block0: %X", blk)
	}
	for _, bad := range [][]byte{nil, {0x01, 0x02, 0x03}, {0x04, 1, 2, 3, 4, 5, 6}} {
		if _, err := BuildBlock0(bad, 0x08, [2]byte{0x04, 0x00}); !errors.Is(err, ErrInvalidUIDLen) {
			t.Fatalf("BuildBlock0(len=%d) err = %v, want ErrInvalidUIDLen", len(bad), err)
		}
	}
}

func FuzzBuildBlock0(f *testing.F) {
	f.Add([]byte{0x01, 0x02, 0x03, 0x04}, uint8(0x08), uint8(0x04), uint8(0x00))
	f.Fuzz(func(t *testing.T, uid []byte, sak, atqa0, atqa1 uint8) {
		blk, err := BuildBlock0(uid, sak, [2]byte{atqa0, atqa1})
		if len(uid) != 4 {
			if !errors.Is(err, ErrInvalidUIDLen) {
				t.Fatalf("len=%d: got %v, want ErrInvalidUIDLen", len(uid), err)
			}
			return
		}
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if blk[4] != (uid[0] ^ uid[1] ^ uid[2] ^ uid[3]) {
			t.Fatalf("BCC mismatch: got %02X", blk[4])
		}
	})
}

func selectAndCCEvents(ccSize uint8) []rxEvent {
	// WUPA -> ATQA, anticollision CL1 (4B UID), SELECT -> SAK 0x00, READ(0) -> 16B + 2B CRC
	u0, u1, u2, u3 := byte(0x11), byte(0x22), byte(0x33), byte(0x44)
	bcc := u0 ^ u1 ^ u2 ^ u3
	p0to3 := []byte{
		u0, u1, u2, bcc,
		0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
		0xE1, 0x10, ccSize, 0x00,
		0xAA, 0xBB,
	}
	return []rxEvent{
		{mainIRQ: irqMainRXE, fifo: []byte{0x44, 0x00}},
		{mainIRQ: irqMainRXE, fifo: []byte{u0, u1, u2, u3, bcc}},
		{mainIRQ: irqMainRXE, fifo: []byte{0x00, 0xFE, 0x51}},
		{mainIRQ: irqMainRXE, fifo: p0to3},
	}
}

func TestWriteType2PagesAndGuards(t *testing.T) {
	t.Run("rejects protected pages before write", func(t *testing.T) {
		for _, badPage := range []uint8{0, 2, 3, 40} {
			chip, _ := newTestChipWithEvents(t, selectAndCCEvents(ntag213CCSize))
			_, err := chip.WriteType2Pages(context.Background(), []WritePageOp{{Page: badPage, Data: [4]byte{1, 2, 3, 4}}}, WriteOptions{VerifyReadBack: true})
			if !errors.Is(err, ErrProtectedPage) {
				t.Fatalf("page %d: err = %v, want ErrProtectedPage", badPage, err)
			}
		}
	})

	t.Run("writes and verifies user page", func(t *testing.T) {
		wantData := [4]byte{0x03, 0x05, 0xD1, 0x01}
		readBack := make([]byte, 18)
		copy(readBack[0:4], wantData[:])
		evts := append(selectAndCCEvents(ntag215CCSize),
			rxEvent{mainIRQ: irqMainRXE, fifo: []byte{0x0A}},
			rxEvent{mainIRQ: irqMainRXE, fifo: readBack},
		)
		chip, _ := newTestChipWithEvents(t, evts)
		if err := chip.WriteNTAGPage(4, wantData); err != nil {
			t.Fatalf("WriteNTAGPage() error = %v", err)
		}
	})

	t.Run("NAK returns ErrWriteNAK with FailedPage", func(t *testing.T) {
		evts := append(selectAndCCEvents(ntag215CCSize),
			rxEvent{mainIRQ: irqMainRXE, fifo: []byte{0x00}},
		)
		chip, _ := newTestChipWithEvents(t, evts)
		rep, err := chip.WriteNTAGPages(context.Background(), []WritePageOp{{Page: 6, Data: [4]byte{1, 2, 3, 4}}}, WriteOptions{VerifyReadBack: true})
		if !errors.Is(err, ErrWriteNAK) || rep.FailedPage != 6 {
			t.Fatalf("got (%+v, %v), want ErrWriteNAK on page 6", rep, err)
		}
	})

	t.Run("read-back mismatch returns ErrVerifyFailed", func(t *testing.T) {
		evts := append(selectAndCCEvents(ntag215CCSize),
			rxEvent{mainIRQ: irqMainRXE, fifo: []byte{0x0A}},
			rxEvent{mainIRQ: irqMainRXE, fifo: make([]byte, 18)},
		)
		chip, _ := newTestChipWithEvents(t, evts)
		rep, err := chip.WriteType2Pages(context.Background(), []WritePageOp{{Page: 5, Data: [4]byte{9, 9, 9, 9}}}, WriteOptions{VerifyReadBack: true})
		if !errors.Is(err, ErrVerifyFailed) || rep.FailedPage != 5 {
			t.Fatalf("got (%+v, %v), want ErrVerifyFailed on page 5", rep, err)
		}
	})
}

func TestCloneMagicUID(t *testing.T) {
	uid := []byte{0xDE, 0xAD, 0xBE, 0xEF}
	bcc := uid[0] ^ uid[1] ^ uid[2] ^ uid[3]

	t.Run("Gen1A happy path and re-poll verify", func(t *testing.T) {
		evts := []rxEvent{
			{timerIRQ: irqTimerNRE},                               // HALT (no reply)
			{mainIRQ: irqMainRXE, fifo: []byte{0x0A}},             // 0x40 7-bit ACK
			{mainIRQ: irqMainRXE, fifo: []byte{0x0A}},             // 0x43 8-bit ACK
			{mainIRQ: irqMainRXE, fifo: []byte{0x0A}},             // 0xA0 0x00 ACK
			{mainIRQ: irqMainRXE, fifo: []byte{0x0A}},             // Block 0 ACK
			{mainIRQ: irqMainRXE, fifo: []byte{0x04, 0x00}},       // Re-poll WUPA
			{mainIRQ: irqMainRXE, fifo: []byte{0xDE, 0xAD, 0xBE, 0xEF, bcc}},
			{mainIRQ: irqMainRXE, fifo: []byte{0x08, 0x00, 0x00}}, // SELECT SAK
		}
		chip, _ := newTestChipWithEvents(t, evts)
		rep, err := chip.CloneMagicUID(context.Background(), MagicGen1A, uid, [2]byte{0x04, 0x00}, 0x08, [6]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF})
		if err != nil || !rep.Verified || rep.ReadBackID != "DE AD BE EF" {
			t.Fatalf("CloneMagicUID(Gen1A) = (%+v, %v)", rep, err)
		}
	})

	t.Run("Gen1A NAK aborts", func(t *testing.T) {
		evts := []rxEvent{
			{timerIRQ: irqTimerNRE},
			{mainIRQ: irqMainRXE, fifo: []byte{0x00}}, // NAK on 0x40
		}
		chip, _ := newTestChipWithEvents(t, evts)
		if _, err := chip.CloneMagicUID(context.Background(), MagicGen1A, uid, [2]byte{0x04, 0x00}, 0x08, [6]byte{}); !errors.Is(err, ErrWriteNAK) {
			t.Fatalf("got %v, want ErrWriteNAK", err)
		}
	})
}

func TestMifareDictAndToNfcTag(t *testing.T) {
	t.Run("non-MIFARE card rejected", func(t *testing.T) {
		evts := selectAndCCEvents(ntag215CCSize)[:3]
		chip, _ := newTestChipWithEvents(t, evts)
		if _, err := chip.ReadMifareClassicDict(context.Background(), [][6]byte{{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}}, nil); !errors.Is(err, ErrNotMifareClassic) {
			t.Fatalf("got %v, want ErrNotMifareClassic", err)
		}
	})

	t.Run("MifareDump ToNfcTag formats 1K and 4K with unknown blocks", func(t *testing.T) {
		dump := &MifareDump{
			UID:      []byte{0xDE, 0xAD, 0xBE, 0xEF},
			ATQA:     [2]byte{0x04, 0x00},
			SAK:      0x08,
			CardSize: "1K",
			Sectors: []SectorResult{
				{
					Sector:   0,
					HasKeyA:  true,
					KeyA:     [6]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF},
					Blocks:   make([][16]byte, 4),
					Readable: []bool{true, true, true, true},
				},
			},
		}
		tag := dump.ToNfcTag()
		formatted := tag.Format()
		if !strings.Contains(formatted, "Mifare Classic type: 1K") || !strings.Contains(formatted, "Block 4: ?? ??") {
			t.Fatalf("unexpected .nfc format:\n%s", formatted)
		}
		dump4k := &MifareDump{UID: []byte{1, 2, 3, 4}, SAK: 0x18, CardSize: "4K"}
		if got := len(dump4k.ToNfcTag().Pages); got != 256 {
			t.Fatalf("4K blocks = %d, want 256", got)
		}
	})
}

func TestEMVAllowlistAndInspect(t *testing.T) {
	t.Run("forbidden APDUs blocked before SPI", func(t *testing.T) {
		forbidden := [][]byte{
			{0x00, 0x20, 0x00, 0x80, 0x08, 1, 2, 3, 4, 5, 6, 7, 8}, // VERIFY PIN
			{0x80, 0xAE, 0x80, 0x00, 0x00},                         // GENERATE AC
			{0x00, 0xDA, 0x9F, 0x10, 0x01, 0x00},                   // PUT DATA
			{0x00, 0x82, 0x00, 0x00, 0x08},                         // EXTERNAL AUTH
			{0x00, 0xA4},                                           // Too short
		}
		chip, tr := newTestChipWithEvents(t, nil)
		for _, apdu := range forbidden {
			_, err := chip.TransceiveISODEP(context.Background(), apdu)
			if !errors.Is(err, ErrForbiddenAPDU) {
				t.Fatalf("APDU %X: got %v, want ErrForbiddenAPDU", apdu, err)
			}
			if tr.eventIdx != 0 {
				t.Fatalf("forbidden APDU %X touched SPI transport!", apdu)
			}
		}
	})

	t.Run("InspectEMV extracts masked PAN and never exposes raw PAN", func(t *testing.T) {
		u0, u1, u2, u3 := byte(0x01), byte(0x02), byte(0x03), byte(0x04)
		bcc := u0 ^ u1 ^ u2 ^ u3
		// PPSE TLV: 6F (FCI) -> 4F (AID A0000000031010) + 50 (VISA)
		ppsePayload := []byte{
			0x02, // PCB
			0x4F, 0x07, 0xA0, 0x00, 0x00, 0x00, 0x03, 0x10, 0x10,
			0x50, 0x04, 'V', 'I', 'S', 'A',
			0x90, 0x00, // SW
			0x00, 0x00, // CRC
		}
		selAIDResp := []byte{0x03, 0x90, 0x00, 0x00, 0x00}
		// Record 1: 5A (PAN 4111111111111111) + 5F24 (Expiry 281231)
		recResp := []byte{
			0x02,
			0x5A, 0x08, 0x41, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11,
			0x5F, 0x24, 0x03, 0x28, 0x12, 0x31,
			0x90, 0x00,
			0x00, 0x00,
		}
		evts := []rxEvent{
			{mainIRQ: irqMainRXE, fifo: []byte{0x04, 0x00}},
			{mainIRQ: irqMainRXE, fifo: []byte{u0, u1, u2, u3, bcc}},
			{mainIRQ: irqMainRXE, fifo: []byte{0x20, 0x00, 0x00}},             // SAK 0x20 (ISO-DEP)
			{mainIRQ: irqMainRXE, fifo: []byte{0x05, 0x78, 0x80, 0x70, 0x02, 0x00, 0x00}}, // ATS
			{mainIRQ: irqMainRXE, fifo: []byte{0xF2, 0x01, 0x00, 0x00}},       // WTX extension
			{mainIRQ: irqMainRXE, fifo: ppsePayload},
			{mainIRQ: irqMainRXE, fifo: selAIDResp},
			{mainIRQ: irqMainRXE, fifo: recResp},
		}
		chip, _ := newTestChipWithEvents(t, evts)
		info, err := chip.InspectEMV(context.Background())
		if err != nil {
			t.Fatalf("InspectEMV() error = %v", err)
		}
		if len(info.Apps) != 1 || info.Apps[0].AID != "A0000000031010" || info.Apps[0].MaskedPAN != "411111******1111" || info.Apps[0].Expiry != "28/12" {
			t.Fatalf("unexpected EMVInfo: %+v", info)
		}
	})
}

func TestMultiProtocolPolling(t *testing.T) {
	t.Run("NFC-B SENSB_RES parsing and errors", func(t *testing.T) {
		sensB := []byte{0x50, 0x11, 0x22, 0x33, 0x44, 0xAA, 0xBB, 0xCC, 0xDD, 0x00, 0x81, 0x71, 0x00, 0x00}
		chip, _ := newTestChipWithEvents(t, []rxEvent{{mainIRQ: irqMainRXE, fifo: sensB}})
		info, err := chip.PollNFCB()
		if err != nil || !bytes.Equal(info.PUPI[:], []byte{0x11, 0x22, 0x33, 0x44}) {
			t.Fatalf("PollNFCB() = (%+v, %v)", info, err)
		}
		badChip, _ := newTestChipWithEvents(t, []rxEvent{{mainIRQ: irqMainRXE, fifo: []byte{0x51, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}}})
		if _, err := badChip.PollNFCB(); !errors.Is(err, ErrProtocol) {
			t.Fatalf("bad prefix err = %v, want ErrProtocol", err)
		}
	})

	t.Run("NFC-F SENSF_RES with and without SystemCode", func(t *testing.T) {
		sensF20 := make([]byte, 20)
		sensF20[0], sensF20[1] = 20, 0x01
		copy(sensF20[2:10], []byte{0x01, 0x2E, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF})
		sensF20[18], sensF20[19] = 0x88, 0xB4
		chip, _ := newTestChipWithEvents(t, []rxEvent{{mainIRQ: irqMainRXE, fifo: sensF20}})
		info, err := chip.PollNFCF()
		if err != nil || !info.HasSysCode || info.SystemCode != [2]byte{0x88, 0xB4} {
			t.Fatalf("PollNFCF(20B) = (%+v, %v)", info, err)
		}
	})

	t.Run("NFC-V INVENTORY UID reversal and partial block read", func(t *testing.T) {
		inv := []byte{0x00, 0x0A, 0x78, 0x56, 0x34, 0x12, 0x50, 0x01, 0x04, 0xE0, 0x00, 0x00}
		blk0 := []byte{0x00, 0xDE, 0xAD, 0xBE, 0xEF, 0x00, 0x00}
		blkErr := []byte{0x01, 0x0F}
		chip, _ := newTestChipWithEvents(t, []rxEvent{
			{mainIRQ: irqMainRXE, fifo: inv},
			{mainIRQ: irqMainRXE, fifo: blk0},
			{mainIRQ: irqMainRXE, fifo: blkErr},
		})
		info, err := chip.PollNFCV(4)
		if err != nil {
			t.Fatalf("PollNFCV() error = %v", err)
		}
		wantUID := [8]byte{0xE0, 0x04, 0x01, 0x50, 0x12, 0x34, 0x56, 0x78}
		if info.UID != wantUID || info.DSFID != 0x0A || len(info.Blocks) != 1 {
			t.Fatalf("unexpected NFCVInfo: %+v", info)
		}
	})

	t.Run("PollMultiProtocol falls through A to B", func(t *testing.T) {
		sensB := []byte{0x50, 0x12, 0x34, 0x56, 0x78, 0, 0, 0, 0, 0, 0x81, 0x71, 0, 0}
		chip, _ := newTestChipWithEvents(t, []rxEvent{
			{timerIRQ: irqTimerNRE},               // NFC-A WUPA -> ErrNoTag
			{mainIRQ: irqMainRXE, fifo: sensB},    // NFC-B SENSB_RES
		})
		res, err := chip.PollMultiProtocol(context.Background(), nil)
		if err != nil || res.Protocol != ProtoNFCB || res.NFCB == nil {
			t.Fatalf("PollMultiProtocol() = (%+v, %v)", res, err)
		}
	})
}
