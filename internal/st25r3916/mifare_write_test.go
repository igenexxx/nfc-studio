package st25r3916

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"testing"
)

func TestWriteMagicGen1ADump(t *testing.T) {
	uid := []byte{0xDE, 0xAD, 0xBE, 0xEF}
	bcc := uid[0] ^ uid[1] ^ uid[2] ^ uid[3]
	blk1 := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	readBack1 := append(append([]byte(nil), blk1[:]...), 0x00, 0x00)

	t.Run("writes Block 0 plus data block with progress and verify", func(t *testing.T) {
		evts := []rxEvent{
			{timerIRQ: irqTimerNRE},                   // HALT
			{mainIRQ: irqMainRXE, fifo: []byte{0x0A}}, // 0x40
			{mainIRQ: irqMainRXE, fifo: []byte{0x0A}}, // 0x43
			{mainIRQ: irqMainRXE, fifo: []byte{0x0A}}, // WRITE blk 0 cmd
			{mainIRQ: irqMainRXE, fifo: []byte{0x0A}}, // WRITE blk 0 data
			{mainIRQ: irqMainRXE, fifo: []byte{0x0A}}, // WRITE blk 1 cmd
			{mainIRQ: irqMainRXE, fifo: []byte{0x0A}}, // WRITE blk 1 data
			{mainIRQ: irqMainRXE, fifo: readBack1},    // READ blk 1 verify
			{mainIRQ: irqMainRXE, fifo: []byte{0x04, 0x00}},
			{mainIRQ: irqMainRXE, fifo: []byte{0xDE, 0xAD, 0xBE, 0xEF, bcc}},
			{mainIRQ: irqMainRXE, fifo: []byte{0x08, 0x00, 0x00}},
		}
		chip, _ := newTestChipWithEvents(t, evts)
		progressCalls := 0
		rep, err := chip.WriteMagicGen1ADump(
			context.Background(),
			uid, [2]byte{0x04, 0x00}, 0x08,
			[]MifareBlockWrite{{Block: 1, Data: blk1}},
			true,
			func(done, total int) {
				progressCalls++
				if total != 2 || done < 1 || done > 2 {
					t.Errorf("bad progress (%d/%d)", done, total)
				}
			},
		)
		if err != nil || !rep.Verified || rep.BlocksWritten != 2 || rep.ReadBackUID != "DE AD BE EF" || progressCalls != 2 {
			t.Fatalf("WriteMagicGen1ADump() = (%+v, %v), prog=%d", rep, err, progressCalls)
		}
	})

	t.Run("rejects non-4B UID and mid-dump NAK reports FailedBlock", func(t *testing.T) {
		chip, _ := newTestChipWithEvents(t, nil)
		if _, err := chip.WriteMagicGen1ADump(context.Background(), []byte{1, 2, 3}, [2]byte{4, 0}, 8, nil, true, nil); !errors.Is(err, ErrInvalidUIDLen) {
			t.Fatalf("got %v, want ErrInvalidUIDLen", err)
		}
		evts := []rxEvent{
			{timerIRQ: irqTimerNRE},
			{mainIRQ: irqMainRXE, fifo: []byte{0x0A}},
			{mainIRQ: irqMainRXE, fifo: []byte{0x0A}},
			{mainIRQ: irqMainRXE, fifo: []byte{0x0A}},
			{mainIRQ: irqMainRXE, fifo: []byte{0x0A}},
			{mainIRQ: irqMainRXE, fifo: []byte{0x00}}, // NAK on blk 5
		}
		chip2, _ := newTestChipWithEvents(t, evts)
		rep, err := chip2.WriteMagicGen1ADump(context.Background(), uid, [2]byte{4, 0}, 8, []MifareBlockWrite{{Block: 5, Data: blk1}}, true, nil)
		if !errors.Is(err, ErrWriteNAK) || rep.FailedBlock != 5 {
			t.Fatalf("got (%+v, %v), want ErrWriteNAK on block 5", rep, err)
		}
	})
}

// simCrypto1CardTransport simulates an ST25R3916 talking to a live MIFARE Classic / Gen2 CUID card.
type simCrypto1CardTransport struct {
	regs        [64]uint8
	fifoTx      []byte
	fifoRx      []byte
	rxReady     bool
	uid         [4]byte
	keyA        [6]byte
	mem         [64][16]byte
	nt          uint32
	cs          *Crypto1
	authPending bool
	writeBlk    int
	block0RO    bool
}

func newSimCrypto1Transport(uid [4]byte, keyA [6]byte) *simCrypto1CardTransport {
	tr := &simCrypto1CardTransport{
		uid:      uid,
		keyA:     keyA,
		nt:       0x13579BDF,
		writeBlk: -1,
	}
	tr.regs[regIdentity] = 0x2D
	tr.regs[regADConverterOut] = 141
	return tr
}

func (s *simCrypto1CardTransport) Close() error { return nil }

func (s *simCrypto1CardTransport) Transfer(tx []byte) ([]byte, error) {
	rx := make([]byte, len(tx))
	if len(tx) == 0 {
		return rx, nil
	}
	op := tx[0]
	switch {
	case op == opLoadFIFO:
		s.fifoTx = append(s.fifoTx, tx[1:]...)
	case op == opReadFIFO:
		copy(rx[1:], s.fifoRx)
	case op == cmdClearFIFO:
		s.fifoTx = nil
		s.fifoRx = nil
		s.rxReady = false
	case op == cmdTransmitWUPA:
		s.cs = nil
		s.authPending = false
		s.writeBlk = -1
		s.fifoRx = []byte{0x04, 0x00}
		s.rxReady = true
	case op == cmdTransmitNoCRC || op == cmdTransmitWithCRC:
		s.handleRFTransmit()
	case op >= 0xC0:
		// other direct command
	case op&opReadReg != 0:
		start := op & 0x3F
		for i := 1; i < len(tx); i++ {
			reg := (start + uint8(i-1)) & 0x3F
			switch reg {
			case regInterrupt:
				if s.rxReady {
					rx[i] = irqMainRXE
					s.rxReady = false
				}
			case regFIFOStatus1:
				rx[i] = uint8(len(s.fifoRx) & 0xFF)
			case regFIFOStatus2:
				rx[i] = uint8((len(s.fifoRx) >> 8) << 6)
			default:
				rx[i] = s.regs[reg]
			}
		}
	default:
		s.regs[op&0x3F] = tx[1]
	}
	return rx, nil
}

func (s *simCrypto1CardTransport) handleRFTransmit() {
	rawPar := (s.regs[regISO14443A] & (iso14443ANoTxPar | iso14443ANoRxPar)) != 0
	if !rawPar {
		s.handlePlainRF()
		return
	}
	if s.authPending {
		s.handleCrypto1AuthStep2()
		return
	}
	if s.cs == nil {
		return
	}
	s.handleCrypto1EncryptedCmd()
}

func (s *simCrypto1CardTransport) handlePlainRF() {
	if len(s.fifoTx) == 2 && s.fifoTx[0] == 0x93 && s.fifoTx[1] == 0x20 {
		bcc := s.uid[0] ^ s.uid[1] ^ s.uid[2] ^ s.uid[3]
		s.fifoRx = []byte{s.uid[0], s.uid[1], s.uid[2], s.uid[3], bcc}
		s.rxReady = true
		return
	}
	if len(s.fifoTx) == 7 && s.fifoTx[0] == 0x93 && s.fifoTx[1] == 0x70 {
		s.fifoRx = []byte{0x08, 0x00, 0x00}
		s.rxReady = true
		return
	}
	if len(s.fifoTx) == 2 && (s.fifoTx[0] == cmdMifareAuthKeyA || s.fifoTx[0] == cmdMifareAuthKeyB) {
		var ntBuf [4]byte
		binary.BigEndian.PutUint32(ntBuf[:], s.nt)
		s.fifoRx = ntBuf[:]
		s.rxReady = true
		s.authPending = true
		return
	}
}

func (s *simCrypto1CardTransport) handleCrypto1AuthStep2() {
	s.authPending = false
	d, p, ok := UnpackParityBits(s.fifoTx, 8)
	if !ok {
		return
	}
	uid32 := binary.BigEndian.Uint32(s.uid[:])
	cs := NewCrypto1(s.keyA)
	cs.Word(uid32^s.nt, false)
	for i := range 4 {
		ks := cs.Byte(d[i], true)
		plainNR := d[i] ^ ks
		if p[i] != (OddParity(plainNR) ^ cs.FilterBit()) {
			return
		}
	}
	arPlain, arOK := cs.DecryptWithParity(d[4:8], p[4:8])
	if !arOK || binary.BigEndian.Uint32(arPlain) != PRNGSuccessor(s.nt, 64) {
		return
	}
	s.cs = cs
	var atBuf [4]byte
	binary.BigEndian.PutUint32(atBuf[:], PRNGSuccessor(s.nt, 96))
	atEnc, atPar := s.cs.EncryptWithParity(atBuf[:])
	packed, _ := PackParityBits(atEnc, atPar)
	s.fifoRx = packed
	s.rxReady = true
}

func (s *simCrypto1CardTransport) handleCrypto1EncryptedCmd() {
	if s.writeBlk >= 0 {
		d, p, ok := UnpackParityBits(s.fifoTx, 18)
		if !ok {
			return
		}
		plain, parOK := s.cs.DecryptWithParity(d, p)
		if !parOK || !VerifyCRC16A(plain) {
			return
		}
		copy(s.mem[s.writeBlk][:], plain[:16])
		if s.writeBlk == 0 {
			copy(s.uid[:], plain[0:4])
		}
		s.writeBlk = -1
		s.fifoRx = []byte{s.encryptAck4(type2ACKNibble)}
		s.rxReady = true
		return
	}
	d, p, ok := UnpackParityBits(s.fifoTx, 4)
	if !ok {
		return
	}
	plain, parOK := s.cs.DecryptWithParity(d, p)
	if !parOK || !VerifyCRC16A(plain) {
		return
	}
	switch plain[0] {
	case cmdMifareWriteBlk:
		if plain[1] == 0 && s.block0RO {
			s.fifoRx = []byte{s.encryptAck4(0x04)} // NAK on locked Block 0
			s.cs = nil
			s.rxReady = true
			return
		}
		s.writeBlk = int(plain[1])
		s.fifoRx = []byte{s.encryptAck4(type2ACKNibble)}
		s.rxReady = true
	case cmdType2Read:
		blk := int(plain[1])
		frame := AppendCRC16A(s.mem[blk][:])
		enc, par := s.cs.EncryptWithParity(frame)
		packed, _ := PackParityBits(enc, par)
		s.fifoRx = packed
		s.rxReady = true
	}
}

func (s *simCrypto1CardTransport) encryptAck4(nib uint8) uint8 {
	var ks uint8
	for i := range 4 {
		ks |= s.cs.Bit(0, false) << i
	}
	return (nib ^ ks) & 0x0F
}

func TestWriteMifareCrypto1Gen2AndRegularEndToEnd(t *testing.T) {
	tr := newSimCrypto1Transport([4]byte{0x11, 0x22, 0x33, 0x44}, defaultMifareKey)
	chip, err := Open(tr)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	chip.pollStep = 0
	chip.guardT = 0

	targetUID := []byte{0xCA, 0xFE, 0xBA, 0xBE}
	blk1 := [16]byte{0xDE, 0xAD, 0xC0, 0xDE, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
	blk4 := [16]byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9}

	prog := 0
	rep, err := chip.WriteMifareCrypto1Dump(
		context.Background(),
		MagicGen2CUID,
		targetUID, [2]byte{0x04, 0x00}, 0x08,
		true,
		[]MifareBlockWrite{{Block: 1, Data: blk1}, {Block: 4, Data: blk4}},
		nil,
		true,
		func(done, total int) { prog++ },
	)
	if err != nil {
		t.Fatalf("WriteMifareCrypto1Dump(Gen2) error = %v rep=%+v", err, rep)
	}
	if !rep.Verified || rep.BlocksWritten != 3 || rep.ReadBackUID != "CA FE BA BE" || prog != 3 {
		t.Fatalf("unexpected Gen2 report: %+v prog=%d", rep, prog)
	}
	if !bytes.Equal(tr.mem[1][:], blk1[:]) || !bytes.Equal(tr.mem[4][:], blk4[:]) {
		t.Fatal("simulated card memory mismatch after Crypto1 write")
	}

	// Regular MIFARE Classic sector write (no Block 0) + CloneMagicUID(Gen2) also pass!
	blk2 := [16]byte{9, 8, 7, 6, 5, 4, 3, 2, 1, 0, 1, 2, 3, 4, 5, 6}
	repReg, err := chip.WriteMifareCrypto1Dump(
		context.Background(),
		MagicRegular,
		targetUID, [2]byte{0x04, 0x00}, 0x08,
		false,
		[]MifareBlockWrite{{Block: 2, Data: blk2}},
		nil,
		true,
		nil,
	)
	if err != nil || !repReg.Verified || repReg.BlocksWritten != 1 {
		t.Fatalf("WriteMifareCrypto1Dump(Regular) = (%+v, %v)", repReg, err)
	}

	cloneRep, err := chip.CloneMagicUID(context.Background(), MagicGen2CUID, []byte{0x12, 0x34, 0x56, 0x78}, [2]byte{0x04, 0x00}, 0x08, defaultMifareKey)
	if err != nil || !cloneRep.Verified || cloneRep.ReadBackID != "12 34 56 78" {
		t.Fatalf("CloneMagicUID(Gen2) = (%+v, %v)", cloneRep, err)
	}
}
