package st25r3916

import (
	"bytes"
	"errors"
	"sync"
	"testing"
)

func TestIsST25R3916(t *testing.T) {
	tests := []struct {
		id   uint8
		want bool
	}{
		{0x28, true},
		{0x2F, true},
		{0x00, false},
		{0xFF, false},
		{0x30, false},
	}
	for _, tc := range tests {
		if got := IsST25R3916(tc.id); got != tc.want {
			t.Fatalf("IsST25R3916(0x%02X)=%v want %v", tc.id, got, tc.want)
		}
	}
}

func TestADCToSupplyMVAndTicks(t *testing.T) {
	if got := ADCToSupplyMV(141); got != 3299 {
		t.Fatalf("ADCToSupplyMV(141)=%d want 3299", got)
	}
	if got := NoResponseTicks(0); got != 212 {
		t.Fatalf("NoResponseTicks(0)=%d want 212", got)
	}
	if got := NoResponseTicks(1000); got != 0xFFFF {
		t.Fatalf("NoResponseTicks(1000)=%d want 65535", got)
	}
}

func TestBuildPTMemoryA(t *testing.T) {
	mem4, err := BuildPTMemoryA([]byte{0xDE, 0xAD, 0xBE, 0xEF}, [2]byte{0x04, 0x00}, 0x08)
	if err != nil || len(mem4) != 15 || mem4[6] != 0x08 {
		t.Fatalf("4-byte UID PT mem failed: len=%d err=%v", len(mem4), err)
	}
	mem7, err := BuildPTMemoryA([]byte{0x04, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66}, [2]byte{0x44, 0x00}, 0x00)
	if err != nil || len(mem7) != 15 || mem7[2] != 0x04 || mem7[8] != 0x66 {
		t.Fatalf("7-byte UID PT mem failed: len=%d err=%v", len(mem7), err)
	}
	if _, err := BuildPTMemoryA([]byte{0x01, 0x02}, [2]byte{0x04, 0x00}, 0x00); err == nil {
		t.Fatal("expected error for invalid UID length")
	}
}

type rxEvent struct {
	mainIRQ  uint8
	timerIRQ uint8
	errIRQ   uint8
	fifo     []byte
}

// mockTransport simulates ST25R3916 SPI registers, FIFO, and scripted RX events.
type mockTransport struct {
	mu        sync.Mutex
	id        uint8
	adc       uint8
	targetIRQ uint8
	ptMem     []byte
	failErr   error

	// Scripted command responses consumed on WUPA / TX_NO_CRC / TX_WITH_CRC.
	events    []rxEvent
	eventIdx  int
	activeEvt *rxEvent
	lastTx    []byte
}

func (m *mockTransport) Transfer(tx []byte) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failErr != nil {
		return nil, m.failErr
	}
	rx := make([]byte, len(tx))
	if len(tx) == 0 {
		return rx, nil
	}

	op := tx[0]
	// Single-byte direct command.
	if len(tx) == 1 {
		switch op {
		case cmdTransmitWUPA, cmdTransmitNoCRC, cmdTransmitWithCRC:
			if m.eventIdx < len(m.events) {
				evt := m.events[m.eventIdx]
				m.activeEvt = &evt
				m.eventIdx++
			} else {
				m.activeEvt = &rxEvent{timerIRQ: irqTimerNRE}
			}
		case cmdClearFIFO:
			m.activeEvt = nil
		}
		return rx, nil
	}

	switch {
	case op == opLoadPTMemA:
		m.ptMem = append([]byte(nil), tx[1:]...)
	case op == opLoadFIFO:
		m.lastTx = append([]byte(nil), tx[1:]...)
	case op == opReadFIFO:
		if m.activeEvt != nil {
			copy(rx[1:], m.activeEvt.fifo)
		}
	case op&opReadReg != 0:
		start := op & 0x3F
		for i := 0; i < len(tx)-1; i++ {
			reg := start + uint8(i)
			switch reg {
			case regIdentity:
				rx[1+i] = m.id
			case regADConverterOut:
				rx[1+i] = m.adc
			case regInterrupt:
				if m.activeEvt != nil {
					rx[1+i] = m.activeEvt.mainIRQ
				}
			case regInterruptTimer:
				if m.activeEvt != nil {
					rx[1+i] = m.activeEvt.timerIRQ
				}
			case regInterruptErrWup:
				if m.activeEvt != nil {
					rx[1+i] = m.activeEvt.errIRQ
				}
			case regInterruptTarget:
				rx[1+i] = m.targetIRQ
				m.targetIRQ = 0
			case regFIFOStatus1:
				if m.activeEvt != nil {
					rx[1+i] = uint8(len(m.activeEvt.fifo) & 0xFF)
				}
			case regFIFOStatus2:
				if m.activeEvt != nil {
					rx[1+i] = uint8(((len(m.activeEvt.fifo) >> 8) & 0x03) << 6)
				}
			}
		}
	}
	return rx, nil
}

func (m *mockTransport) Close() error { return nil }

func openFastChip(t *testing.T, tr *mockTransport) *Chip {
	t.Helper()
	c, err := Open(tr)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	c.pollStep = 0
	c.guardT = 0
	return c
}

func TestChipLifecycle(t *testing.T) {
	tr := &mockTransport{id: 0x29, adc: 141}
	c := openFastChip(t, tr)
	defer c.Close()

	if c.Info().SupplyMV != 3299 {
		t.Fatalf("unexpected supply_mv=%d", c.Info().SupplyMV)
	}
	if err := c.FieldOn(); err != nil {
		t.Fatalf("FieldOn failed: %v", err)
	}
	if err := c.FieldOff(); err != nil {
		t.Fatalf("FieldOff failed: %v", err)
	}
	if err := c.StartEmulation([]byte{0x04, 0xA1, 0xB2, 0xC3, 0xD4, 0xE5, 0xF6}, [2]byte{0x44, 0x00}, 0x00); err != nil {
		t.Fatalf("StartEmulation failed: %v", err)
	}
	if len(tr.ptMem) != 15 {
		t.Fatalf("expected 15-byte PT memory load, got %d", len(tr.ptMem))
	}

	if _, err := Open(&mockTransport{id: 0xFF}); err == nil {
		t.Fatal("expected error for missing chip")
	}
	if _, err := Open(nil); err == nil {
		t.Fatal("expected error for nil transport")
	}
	tr.failErr = errors.New("spi error")
	if err := c.FieldOn(); err == nil {
		t.Fatal("expected SPI error propagation")
	}
}

func TestPollISO14443A(t *testing.T) {
	bcc := func(b0, b1, b2, b3 byte) byte { return b0 ^ b1 ^ b2 ^ b3 }

	tests := []struct {
		name      string
		events    []rxEvent
		wantErr   error
		wantUID   []byte
		wantATQA  [2]byte
		wantSAK   uint8
		wantPages int
	}{
		{
			name: "4-byte MIFARE Classic 1K",
			events: []rxEvent{
				{mainIRQ: irqMainRXE, fifo: []byte{0x04, 0x00}},                                                 // WUPA -> ATQA
				{mainIRQ: irqMainRXE, fifo: []byte{0xDE, 0xAD, 0xBE, 0xEF, bcc(0xDE, 0xAD, 0xBE, 0xEF)}},       // CL1 anticollision
				{mainIRQ: irqMainRXE, fifo: []byte{0x08, 0xB6, 0xDD}},                                           // CL1 SELECT -> SAK + CRC
			},
			wantUID:  []byte{0xDE, 0xAD, 0xBE, 0xEF},
			wantATQA: [2]byte{0x04, 0x00},
			wantSAK:  0x08,
		},
		{
			name: "7-byte NTAG with 8 pages",
			events: []rxEvent{
				{mainIRQ: irqMainRXE, fifo: []byte{0x44, 0x00}},                                                 // WUPA -> ATQA
				{mainIRQ: irqMainRXE, fifo: []byte{0x88, 0x04, 0xA1, 0xB2, bcc(0x88, 0x04, 0xA1, 0xB2)}},       // CL1 CT + UID0..2
				{mainIRQ: irqMainRXE, fifo: []byte{0x04, 0xDA, 0x17}},                                           // CL1 SAK (cascade)
				{mainIRQ: irqMainRXE, fifo: []byte{0xC3, 0xD4, 0xE5, 0xF6, bcc(0xC3, 0xD4, 0xE5, 0xF6)}},       // CL2 UID3..6
				{mainIRQ: irqMainRXE, fifo: []byte{0x00, 0xFE, 0x51}},                                           // CL2 SAK (NTAG)
				{mainIRQ: irqMainRXE, fifo: []byte{ // READ(0): pages 0..3 + 2 CRC; CC[2]=0x02 => 4 + 4 = 8 total pages
					0x04, 0xA1, 0xB2, 0x77,
					0xC3, 0xD4, 0xE5, 0xF6,
					0x11, 0x48, 0x00, 0x00,
					0xE1, 0x10, 0x02, 0x00,
					0xAA, 0xBB,
				}},
				{mainIRQ: irqMainRXE, fifo: []byte{ // READ(4): pages 4..7 + 2 CRC
					0x03, 0x09, 0xD1, 0x01,
					0x05, 0x55, 0x04, 'x',
					'.', 'c', 'c', 0xFE,
					0x00, 0x00, 0x00, 0x00,
					0xCC, 0xDD,
				}},
			},
			wantUID:   []byte{0x04, 0xA1, 0xB2, 0xC3, 0xD4, 0xE5, 0xF6},
			wantATQA:  [2]byte{0x44, 0x00},
			wantSAK:   0x00,
			wantPages: 8,
		},
		{
			name: "no tag response (NRE)",
			events: []rxEvent{
				{timerIRQ: irqTimerNRE},
			},
			wantErr: ErrNoTag,
		},
		{
			name: "bit collision during anticollision",
			events: []rxEvent{
				{mainIRQ: irqMainRXE, fifo: []byte{0x04, 0x00}},
				{mainIRQ: irqMainCol},
			},
			wantErr: ErrCollision,
		},
		{
			name: "CRC error on SELECT",
			events: []rxEvent{
				{mainIRQ: irqMainRXE, fifo: []byte{0x04, 0x00}},
				{mainIRQ: irqMainRXE, fifo: []byte{0xDE, 0xAD, 0xBE, 0xEF, bcc(0xDE, 0xAD, 0xBE, 0xEF)}},
				{mainIRQ: irqMainRXE, errIRQ: irqErrCRC, fifo: []byte{0x08, 0x00, 0x00}},
			},
			wantErr: ErrCRC,
		},
		{
			name: "BCC mismatch in anticollision",
			events: []rxEvent{
				{mainIRQ: irqMainRXE, fifo: []byte{0x04, 0x00}},
				{mainIRQ: irqMainRXE, fifo: []byte{0xDE, 0xAD, 0xBE, 0xEF, 0x00}}, // wrong BCC
			},
			wantErr: ErrProtocol,
		},
		{
			name: "short ATQA",
			events: []rxEvent{
				{mainIRQ: irqMainRXE, fifo: []byte{0x04}},
			},
			wantErr: ErrProtocol,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tr := &mockTransport{id: 0x29, adc: 141, events: tc.events}
			c := openFastChip(t, tr)
			defer c.Close()

			tag, err := c.PollISO14443A()
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("got err=%v want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if !bytes.Equal(tag.UID, tc.wantUID) {
				t.Fatalf("UID=%X want %X", tag.UID, tc.wantUID)
			}
			if tag.ATQA != tc.wantATQA || tag.SAK != tc.wantSAK {
				t.Fatalf("ATQA=%X SAK=%02X want %X/%02X", tag.ATQA, tag.SAK, tc.wantATQA, tc.wantSAK)
			}
			if len(tag.Pages) != tc.wantPages {
				t.Fatalf("pages=%d want %d", len(tag.Pages), tc.wantPages)
			}
		})
	}
}

func TestMeasureExternalField(t *testing.T) {
	tr := &mockTransport{id: 0x29, adc: 160}
	c := openFastChip(t, tr)
	defer c.Close()

	amp, err := c.MeasureExternalField()
	if err != nil {
		t.Fatalf("MeasureExternalField unexpected err: %v", err)
	}
	if amp != 160 {
		t.Fatalf("amp = %d, want 160", amp)
	}
	if got := ExternalFieldPercent(amp); got < 55 || got > 65 {
		t.Fatalf("ExternalFieldPercent(160) = %d, want ~61", got)
	}
	if got := ExternalFieldPercent(0); got != 0 {
		t.Fatalf("ExternalFieldPercent(0) = %d, want 0", got)
	}
	if got := ExternalFieldPercent(255); got != 100 {
		t.Fatalf("ExternalFieldPercent(255) = %d, want 100", got)
	}
}

func TestPollTargetHitAndConcurrency(t *testing.T) {
	tr := &mockTransport{id: 0x29, adc: 141}
	c := openFastChip(t, tr)
	defer c.Close()

	tr.mu.Lock()
	tr.targetIRQ = irqTargetSDDC
	tr.mu.Unlock()

	hit, err := c.PollTargetHit()
	if err != nil || !hit {
		t.Fatalf("PollTargetHit()=(%v, %v) want (true, nil)", hit, err)
	}
	hit2, err := c.PollTargetHit()
	if err != nil || hit2 {
		t.Fatalf("second PollTargetHit()=(%v, %v) want (false, nil)", hit2, err)
	}

	// Verify thread-safety under -race between concurrent poll & emulation calls.
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 10 {
				_, _ = c.PollISO14443A()
				_, _ = c.PollTargetHit()
			}
		}()
	}
	wg.Wait()
}
