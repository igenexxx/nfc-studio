package st25r3916

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const (
	nfcbSensBPrefix     uint8  = 0x50
	nfcfSensFRespCode   uint8  = 0x01
	nfcvErrFlagMask     uint8  = 0x01
	nfcvMaxBlocksCap           = 64
	nfcvDefaultBlocks          = 16
	multiPollTimeoutMs  uint32 = 15
	multiPollWait              = 35 * time.Millisecond
	felicaGuardMin             = 20 * time.Millisecond
)

// NFCBInfo holds ISO 14443-B SENSB_RES metadata.
type NFCBInfo struct {
	PUPI         [4]byte
	AppData      [4]byte
	ProtocolInfo [3]byte
}

// FeliCaInfo holds NFC-F (FeliCa) SENSF_RES metadata.
type FeliCaInfo struct {
	IDm        [8]byte
	PMm        [8]byte
	SystemCode [2]byte
	HasSysCode bool
}

// NFCVInfo holds ISO 15693 (NFC-V) inventory and optional block memory.
type NFCVInfo struct {
	UID    [8]byte // MSB-first display order
	DSFID  uint8
	Blocks [][]byte
}

// MultiTagInfo wraps a tag discovered by PollMultiProtocol.
type MultiTagInfo struct {
	Protocol Protocol
	NFCA     *TagInfo
	NFCB     *NFCBInfo
	NFCF     *FeliCaInfo
	NFCV     *NFCVInfo
}

// PollNFCB discovers an ISO 14443-B tag via SENSB_REQ.
func (c *Chip) PollNFCB() (*NFCBInfo, error) {
	return c.PollISO14443B()
}

// PollISO14443B configures ST25R3916 for NFC-B (106 kbps) and transmits SENSB_REQ.
func (c *Chip) PollISO14443B() (*NFCBInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	defer func() { _ = c.fieldOffLocked() }()
	return c.pollNFCBLocked()
}

func (c *Chip) pollNFCBLocked() (*NFCBInfo, error) {
	if err := c.enableProtocolFieldLocked(modeNFCB, bitrate106, c.guardT); err != nil {
		return nil, err
	}
	if err := c.writeReg(regISO14443B, 0x00); err != nil {
		return nil, err
	}
	rx, err := c.txMultiFrameLocked([]byte{0x05, 0x00, 0x00})
	if err != nil {
		return nil, err
	}
	if len(rx) < 12 || rx[0] != nfcbSensBPrefix {
		return nil, fmt.Errorf("%w: invalid SENSB_RES (len=%d)", ErrProtocol, len(rx))
	}
	info := &NFCBInfo{}
	copy(info.PUPI[:], rx[1:5])
	copy(info.AppData[:], rx[5:9])
	copy(info.ProtocolInfo[:], rx[9:12])
	return info, nil
}

// PollNFCF discovers a FeliCa (NFC-F) tag via SENSF_REQ.
func (c *Chip) PollNFCF() (*FeliCaInfo, error) {
	return c.PollFeliCa()
}

// PollFeliCa configures ST25R3916 for NFC-F (212 kbps) and transmits wildcard SENSF_REQ.
func (c *Chip) PollFeliCa() (*FeliCaInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	defer func() { _ = c.fieldOffLocked() }()
	return c.pollNFCFLocked()
}

func (c *Chip) pollNFCFLocked() (*FeliCaInfo, error) {
	guard := c.guardT
	if guard > 0 && guard < felicaGuardMin {
		guard = felicaGuardMin
	}
	if err := c.enableProtocolFieldLocked(modeNFCF, bitrate212, guard); err != nil {
		return nil, err
	}
	rx, err := c.txMultiFrameLocked([]byte{0x06, 0x00, 0xFF, 0xFF, 0x01, 0x00})
	if err != nil {
		return nil, err
	}
	if len(rx) < 18 || rx[1] != nfcfSensFRespCode {
		return nil, fmt.Errorf("%w: invalid SENSF_RES (len=%d)", ErrProtocol, len(rx))
	}
	info := &FeliCaInfo{}
	copy(info.IDm[:], rx[2:10])
	copy(info.PMm[:], rx[10:18])
	if len(rx) >= 20 {
		copy(info.SystemCode[:], rx[18:20])
		info.HasSysCode = true
	}
	return info, nil
}

// PollNFCV discovers an ISO 15693 (NFC-V) tag via INVENTORY and reads up to maxBlocks blocks.
func (c *Chip) PollNFCV(maxBlocks int) (*NFCVInfo, error) {
	return c.PollISO15693(maxBlocks)
}

// PollISO15693 configures ST25R3916 for ISO 15693, sends INVENTORY, and reads up to maxBlocks blocks.
func (c *Chip) PollISO15693(maxBlocks int) (*NFCVInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	defer func() { _ = c.fieldOffLocked() }()
	return c.pollNFCVLocked(maxBlocks)
}

func (c *Chip) pollNFCVLocked(maxBlocks int) (*NFCVInfo, error) {
	if err := c.enableProtocolFieldLocked(modeNFCV, bitrate106, c.guardT); err != nil {
		return nil, err
	}
	rx, err := c.txMultiFrameLocked([]byte{0x26, 0x01, 0x00})
	if err != nil {
		return nil, err
	}
	if len(rx) < 10 || (rx[0]&nfcvErrFlagMask) != 0 {
		return nil, fmt.Errorf("%w: invalid ISO15693 INVENTORY response (len=%d)", ErrProtocol, len(rx))
	}
	info := &NFCVInfo{DSFID: rx[1]}
	uidLSB := rx[2:10]
	for i := range 8 {
		info.UID[i] = uidLSB[7-i]
	}
	limit := min(max(0, maxBlocks), nfcvMaxBlocksCap)
	for blk := range limit {
		frame := make([]byte, 0, 11)
		frame = append(frame, 0x22, 0x20)
		frame = append(frame, uidLSB...)
		frame = append(frame, uint8(blk))
		blkRx, err := c.txMultiFrameLocked(frame)
		if err != nil || len(blkRx) < 5 || (blkRx[0]&nfcvErrFlagMask) != 0 {
			break
		}
		info.Blocks = append(info.Blocks, append([]byte(nil), blkRx[1:5]...))
	}
	return info, nil
}

// PollMultiProtocol sequentially polls the requested 13.56 MHz protocols and returns the first tag found.
func (c *Chip) PollMultiProtocol(ctx context.Context, protos []Protocol) (*MultiTagInfo, error) {
	if len(protos) == 0 {
		protos = []Protocol{ProtoNFCA, ProtoNFCB, ProtoNFCF, ProtoNFCV}
	}
	defer func() { _ = c.FieldOff() }()

	for _, p := range protos {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		res, err := c.pollSingleProtocol(p)
		if err == nil && res != nil {
			return res, nil
		}
		if err != nil && !errors.Is(err, ErrNoTag) && !errors.Is(err, ErrProtocol) {
			return nil, err
		}
	}
	return nil, ErrNoTag
}

func (c *Chip) pollSingleProtocol(p Protocol) (*MultiTagInfo, error) {
	switch p {
	case ProtoNFCA:
		tag, err := c.PollISO14443A()
		if err != nil {
			return nil, err
		}
		return &MultiTagInfo{Protocol: ProtoNFCA, NFCA: tag}, nil
	case ProtoNFCB:
		tag, err := c.PollISO14443B()
		if err != nil {
			return nil, err
		}
		return &MultiTagInfo{Protocol: ProtoNFCB, NFCB: tag}, nil
	case ProtoNFCF:
		tag, err := c.PollFeliCa()
		if err != nil {
			return nil, err
		}
		return &MultiTagInfo{Protocol: ProtoNFCF, NFCF: tag}, nil
	case ProtoNFCV:
		tag, err := c.PollISO15693(nfcvDefaultBlocks)
		if err != nil {
			return nil, err
		}
		return &MultiTagInfo{Protocol: ProtoNFCV, NFCV: tag}, nil
	default:
		return nil, fmt.Errorf("%w: unsupported protocol %q", ErrProtocol, p)
	}
}

func (c *Chip) enableProtocolFieldLocked(mode, bitrate uint8, guard time.Duration) error {
	if err := c.fieldOffLocked(); err != nil {
		return err
	}
	if c.guardT > 0 {
		time.Sleep(fieldResetGuard)
	}
	if err := c.writeReg(regMode, mode); err != nil {
		return err
	}
	if err := c.writeReg(regBitrate, bitrate); err != nil {
		return err
	}
	if err := c.writeReg(regAux, 0x00); err != nil {
		return err
	}
	if err := c.writeReg(regOpControl, opEnable|opRxEnable|opTxEnable); err != nil {
		return err
	}
	if guard > 0 {
		time.Sleep(guard)
	}
	return nil
}

func (c *Chip) txMultiFrameLocked(frame []byte) ([]byte, error) {
	if err := c.setNoResponseTimer(multiPollTimeoutMs); err != nil {
		return nil, err
	}
	c.clearInterrupts()
	if err := c.command(cmdClearFIFO); err != nil {
		return nil, err
	}
	if err := c.writeFIFO(frame); err != nil {
		return nil, err
	}
	if err := c.setTxBytes(len(frame)); err != nil {
		return nil, err
	}
	if err := c.command(cmdTransmitWithCRC); err != nil {
		return nil, err
	}
	return c.waitRx(multiPollWait, true)
}
