package st25r3916

import (
	"context"
	"errors"
	"testing"
)

func TestProbeBlankTargetAllCardTypes(t *testing.T) {
	t.Run("detects NTAG213/215/216 from CC page", func(t *testing.T) {
		cases := []struct {
			ccSize uint8
			want   BlankKind
			pages  int
		}{
			{ntag213CCSize, BlankNTAG213, 36},
			{ntag215CCSize, BlankNTAG215, 126},
			{ntag216CCSize, BlankNTAG216, 222},
		}
		for _, tc := range cases {
			chip, _ := newTestChipWithEvents(t, selectAndCCEvents(tc.ccSize))
			got, err := chip.ProbeBlankTarget(context.Background())
			if err != nil || got.Kind != tc.want || got.MaxUserPages != tc.pages {
				t.Fatalf("cc=0x%02X: got (%+v, %v), want (%s, %d pg)", tc.ccSize, got, err, tc.want, tc.pages)
			}
		}
	})

	t.Run("detects Gen1A Magic Card non-destructively", func(t *testing.T) {
		u0, u1, u2, u3 := byte(0xAA), byte(0xBB), byte(0xCC), byte(0xDD)
		bcc := u0 ^ u1 ^ u2 ^ u3
		evts := []rxEvent{
			{mainIRQ: irqMainRXE, fifo: []byte{0x04, 0x00}},
			{mainIRQ: irqMainRXE, fifo: []byte{u0, u1, u2, u3, bcc}},
			{mainIRQ: irqMainRXE, fifo: []byte{0x08, 0x00, 0x00}}, // SAK 0x08
			{timerIRQ: irqTimerNRE},                               // HALT
			{mainIRQ: irqMainRXE, fifo: []byte{0x0A}},             // 0x40 ACK
			{mainIRQ: irqMainRXE, fifo: []byte{0x0A}},             // 0x43 ACK
		}
		chip, _ := newTestChipWithEvents(t, evts)
		got, err := chip.ProbeBlankTarget(context.Background())
		if err != nil || got.Kind != BlankMagicGen1A || !got.Gen1ABackdoor || got.MaxBlocks != 64 {
			t.Fatalf("Gen1A probe = (%+v, %v)", got, err)
		}
	})

	t.Run("detects non-Gen1A MIFARE Classic 1K", func(t *testing.T) {
		u0, u1, u2, u3 := byte(0xA3), byte(0x7F), byte(0x7D), byte(0x0A)
		bcc := u0 ^ u1 ^ u2 ^ u3
		evts := []rxEvent{
			{mainIRQ: irqMainRXE, fifo: []byte{0x04, 0x00}},
			{mainIRQ: irqMainRXE, fifo: []byte{u0, u1, u2, u3, bcc}},
			{mainIRQ: irqMainRXE, fifo: []byte{0x08, 0x00, 0x00}}, // SAK 0x08
			{timerIRQ: irqTimerNRE},                               // HALT
			{timerIRQ: irqTimerNRE},                               // 0x40 timeout -> not Gen1A
		}
		chip, _ := newTestChipWithEvents(t, evts)
		got, err := chip.ProbeBlankTarget(context.Background())
		if err != nil || got.Kind != BlankMifareClassic1K || got.Gen1ABackdoor {
			t.Fatalf("MIFARE 1K probe = (%+v, %v)", got, err)
		}
	})

	t.Run("detects DESFire as BlankUnsupported and falls through to NFC-V", func(t *testing.T) {
		u0, u1, u2, u3 := byte(1), byte(2), byte(3), byte(4)
		bcc := u0 ^ u1 ^ u2 ^ u3
		evts := []rxEvent{
			{mainIRQ: irqMainRXE, fifo: []byte{0x44, 0x03}},
			{mainIRQ: irqMainRXE, fifo: []byte{u0, u1, u2, u3, bcc}},
			{mainIRQ: irqMainRXE, fifo: []byte{0x20, 0x00, 0x00}},
		}
		chip, _ := newTestChipWithEvents(t, evts)
		got, err := chip.ProbeBlankTarget(context.Background())
		if err != nil || got.Kind != BlankUnsupported {
			t.Fatalf("DESFire probe = (%+v, %v)", got, err)
		}

		// NFC-A no tag -> NFC-V INVENTORY succeeds.
		inv := []byte{0x00, 0x0A, 0x78, 0x56, 0x34, 0x12, 0x50, 0x01, 0x04, 0xE0, 0x00, 0x00}
		chipV, _ := newTestChipWithEvents(t, []rxEvent{
			{timerIRQ: irqTimerNRE},
			{mainIRQ: irqMainRXE, fifo: inv},
		})
		gotV, err := chipV.ProbeBlankTarget(context.Background())
		if err != nil || gotV.Kind != BlankNFCV {
			t.Fatalf("NFC-V probe = (%+v, %v)", gotV, err)
		}
	})
}

func TestWriteMifareCrypto1AutoBlock0LockedFallback(t *testing.T) {
	tr := newSimCrypto1Transport([4]byte{0xA3, 0x7F, 0x7D, 0x0A}, defaultMifareKey)
	tr.block0RO = true
	chip, err := Open(tr)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	chip.pollStep = 0
	chip.guardT = 0

	blk1 := [16]byte{0xCA, 0xFE, 0xBA, 0xBE, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}

	// 1. With allowRegularFallback=true and data blocks -> falls back cleanly & writes Block 1!
	rep, err := chip.WriteMifareCrypto1Auto(
		context.Background(),
		[]byte{0xF3, 0xCC, 0x8C, 0x97}, [2]byte{0x04, 0x00}, 0x08,
		[]MifareBlockWrite{{Block: 1, Data: blk1}},
		nil, true, true, nil,
	)
	if err != nil || !rep.Block0LockedFallback || rep.BlocksWritten != 1 || !rep.Verified {
		t.Fatalf("Auto fallback = (%+v, %v)", rep, err)
	}
	if tr.mem[1] != blk1 {
		t.Fatal("Block 1 not written during regular fallback")
	}

	// 2. With 0 data blocks (UID-only) -> returns ErrBlock0Locked!
	_, err = chip.WriteMifareCrypto1Auto(
		context.Background(),
		[]byte{0xF3, 0xCC, 0x8C, 0x97}, [2]byte{0x04, 0x00}, 0x08,
		nil, nil, true, true, nil,
	)
	if !errors.Is(err, ErrBlock0Locked) {
		t.Fatalf("UID-only on RO Block 0 err = %v, want ErrBlock0Locked", err)
	}
}
