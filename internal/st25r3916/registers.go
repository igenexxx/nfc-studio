package st25r3916

import (
	"errors"
	"fmt"
)

var (
	// ErrNoTag indicates the No-Response Timer expired with no card reply.
	ErrNoTag = errors.New("st25r3916: no tag response")
	// ErrCollision indicates multiple cards collided during anticollision.
	ErrCollision = errors.New("st25r3916: bit collision")
	// ErrTimeout indicates the host wait loop timed out waiting for IRQ.
	ErrTimeout = errors.New("st25r3916: wait timeout")
	// ErrCRC indicates a framing, parity, or CRC error on receive.
	ErrCRC = errors.New("st25r3916: rx framing/crc error")
	// ErrProtocol indicates an unexpected ISO14443-A response length or BCC mismatch.
	ErrProtocol = errors.New("st25r3916: protocol error")
)

const (
	// SPI opcodes for ST25R3916.
	opReadReg    uint8 = 0x40
	opLoadFIFO   uint8 = 0x80
	opReadFIFO   uint8 = 0x9F
	opLoadPTMemA uint8 = 0xA0
	opLoadPTMemF uint8 = 0xA8

	// Direct commands.
	cmdSetDefault       uint8 = 0xC1
	cmdStopAll          uint8 = 0xC2
	cmdTransmitWithCRC  uint8 = 0xC4
	cmdTransmitNoCRC    uint8 = 0xC5
	cmdTransmitREQA     uint8 = 0xC6
	cmdTransmitWUPA     uint8 = 0xC7
	cmdInitialFieldOn   uint8 = 0xC8
	cmdMeasureAmplitude uint8 = 0xD3
	cmdMeasurePhase     uint8 = 0xD5
	cmdClearFIFO        uint8 = 0xDB
	cmdMeasurePowerSupp uint8 = 0xDF
	cmdSpaceBAccess     uint8 = 0xFB
	cmdTestAccess       uint8 = 0xFC

	// Register space A.
	regIOConfig1       uint8 = 0x00
	regIOConfig2       uint8 = 0x01
	regOpControl       uint8 = 0x02
	regMode            uint8 = 0x03
	regBitrate         uint8 = 0x04
	regISO14443A       uint8 = 0x05
	regISO14443B       uint8 = 0x06
	regISO14443B2      uint8 = 0x07
	regPassiveTarget   uint8 = 0x08
	regAux             uint8 = 0x0A
	regTimerEMVControl uint8 = 0x0F
	regNoRespTimer1    uint8 = 0x10
	regNoRespTimer2    uint8 = 0x11
	regInterruptMask   uint8 = 0x16
	regInterrupt       uint8 = 0x1A
	regInterruptTimer  uint8 = 0x1B
	regInterruptErrWup uint8 = 0x1C
	regInterruptTarget uint8 = 0x1D
	regFIFOStatus1     uint8 = 0x1E
	regFIFOStatus2     uint8 = 0x1F
	regTxBytes1        uint8 = 0x22
	regTxBytes2        uint8 = 0x23
	regADConverterOut  uint8 = 0x25
	regAntennaTuning1  uint8 = 0x26
	regAntennaTuning2  uint8 = 0x27
	regTxDriver        uint8 = 0x28
	regPTModulation    uint8 = 0x29
	regFieldOnThresh   uint8 = 0x2A
	regFieldOffThresh  uint8 = 0x2B
	regRegulatorCtrl   uint8 = 0x2C
	regIdentity        uint8 = 0x3F

	// Register space B.
	regBEmdSuppression uint8 = 0x05
	regBResistiveAm    uint8 = 0x2A

	// Identity mask/value for ST25R3916.
	identityTypeMask uint8 = 0xF8
	identityType     uint8 = 0x28

	// Operation control bits.
	opEnable   uint8 = 0x80
	opRxEnable uint8 = 0x40
	opTxEnable uint8 = 0x08
	ioSupply3V uint8 = 0x80

	// Protocol / Aux bits.
	iso14443AAntcl uint8 = 0x01
	auxNoCRCRx     uint8 = 0x80

	// Main IRQ register (0x1A) bits.
	irqMainRXS uint8 = 0x20
	irqMainRXE uint8 = 0x10
	irqMainTXE uint8 = 0x08
	irqMainCol uint8 = 0x04

	// Timer & NFC IRQ register (0x1B) bits.
	irqTimerNRE uint8 = 0x40

	// Error & Wake-up IRQ register (0x1C) bits.
	irqErrCRC         uint8 = 0x40
	irqErrParity      uint8 = 0x20
	irqErrSoftFraming uint8 = 0x10
	irqErrHardFraming uint8 = 0x08

	// Passive Target IRQ register (0x1D) bits.
	irqTargetWUA  uint8 = 0x80
	irqTargetWUF  uint8 = 0x40
	irqTargetSDDC uint8 = 0x10

	// Target mode register (0x03) values.
	modeTargetNFCA uint8 = 0x88
	modeTargetNFCF uint8 = 0x90

	// ISO 14443-A Cascade Tag byte.
	cascadeTag uint8 = 0x88

	// Multi-protocol Mode & Bitrate register values.
	modeNFCA   uint8 = 0x08
	modeNFCB   uint8 = 0x10
	modeNFCF   uint8 = 0x18
	modeNFCV   uint8 = 0x30
	bitrate106 uint8 = 0x00
	bitrate212 uint8 = 0x11
)

// Protocol identifies an ST25R3916 13.56 MHz contactless modulation standard.
type Protocol string

const (
	ProtoNFCA Protocol = "NFC-A"
	ProtoNFCB Protocol = "NFC-B"
	ProtoNFCF Protocol = "NFC-F"
	ProtoNFCV Protocol = "NFC-V"
)

// ChipInfo describes the detected ST25R3916 silicon and measured supply.
type ChipInfo struct {
	Identity uint8
	Revision uint8
	SupplyMV uint32
}

// TagInfo represents a live ISO 14443-A tag discovered by ST25R3916.
type TagInfo struct {
	UID   []byte
	ATQA  [2]byte
	SAK   uint8
	Pages [][]byte // 4-byte pages for NTAG21x / MIFARE Ultralight (when SAK == 0x00)
}

// IsST25R3916 returns true if the identity register matches ST25R3916 (upper 5 bits = 0x28).
func IsST25R3916(identity uint8) bool {
	return (identity & identityTypeMask) == identityType
}

// ADCToSupplyMV converts the ST25R3916 AD_CONV_OUT byte to millivolts (23.4 mV/step).
func ADCToSupplyMV(raw uint8) uint32 {
	return uint32(raw) * 234 / 10
}

// NoResponseTicks converts timeoutMs into 64-carrier-period ticks (~4.72 µs/tick).
func NoResponseTicks(timeoutMs uint32) uint16 {
	if timeoutMs == 0 {
		timeoutMs = 1
	}
	ticks := (uint64(timeoutMs)*13560 + 63) / 64
	if ticks > 0xFFFF {
		return 0xFFFF
	}
	return uint16(ticks)
}

// BuildPTMemoryA constructs the 15-byte ST25R3916 Passive Target A memory image
// for hardware-assisted REQA/anticollision/SELECT (4-byte or 7-byte UID).
func BuildPTMemoryA(uid []byte, atqa [2]byte, sak uint8) ([]byte, error) {
	if len(uid) != 4 && len(uid) != 7 {
		return nil, fmt.Errorf("st25r3916: UID length must be 4 or 7, got %d", len(uid))
	}
	// ST25R3916 PT_A memory: [ATQA0, ATQA1, UID0..UIDn, SAK...] (15 bytes padded)
	mem := make([]byte, 15)
	mem[0] = atqa[0]
	mem[1] = atqa[1]
	if len(uid) == 4 {
		copy(mem[2:6], uid)
		mem[6] = sak
	} else {
		// 7-byte UID: 3-byte UID0..2 + 4-byte UID3..6, SAK at index 9
		copy(mem[2:5], uid[0:3])
		copy(mem[5:9], uid[3:7])
		mem[9] = sak
	}
	return mem, nil
}

// DefaultFeliCaPMm is a standard FeliCa Lite-S 8-byte manufacturing parameter (PMm).
var DefaultFeliCaPMm = [8]byte{0x01, 0x20, 0x22, 0x04, 0x27, 0x67, 0x4E, 0xFF}

// BuildPTMemoryF constructs the 21-byte ST25R3916 Passive Target F (FeliCa) memory image
// for hardware-assisted SENSF_REQ -> SENSF_RES replies (8-byte IDm, 8-byte PMm, 2-byte System Code).
func BuildPTMemoryF(idm, pmm []byte, sysCode [2]byte) ([]byte, error) {
	if len(idm) != 8 {
		return nil, fmt.Errorf("st25r3916: FeliCa IDm length must be 8, got %d", len(idm))
	}
	if len(pmm) != 0 && len(pmm) != 8 {
		return nil, fmt.Errorf("st25r3916: FeliCa PMm length must be 8, got %d", len(pmm))
	}
	effPMm := DefaultFeliCaPMm[:]
	if len(pmm) == 8 {
		allZero := true
		for _, b := range pmm {
			if b != 0 {
				allZero = false
				break
			}
		}
		if !allZero {
			effPMm = pmm
		}
	}
	// ST25R3916 PT_F memory (21 bytes): [SC0, SC1, 0x12 (SENSF_RES len), 0x01, IDm(8), PMm(8), 0x00]
	mem := make([]byte, 21)
	mem[0] = sysCode[0]
	mem[1] = sysCode[1]
	mem[2] = 0x12
	mem[3] = 0x01
	copy(mem[4:12], idm)
	copy(mem[12:20], effPMm)
	mem[20] = 0x00
	return mem, nil
}
