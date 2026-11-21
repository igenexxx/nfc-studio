package st25r3916

import (
	"fmt"
	"time"
)

const (
	fieldResetGuard        = 3 * time.Millisecond
	sakCascadeBit    uint8 = 0x04
	sakType2         uint8 = 0x00
	cmdType2Read     uint8 = 0x30
	ndefCCMagic      uint8 = 0xE1
	defaultType2Pages      = 16
	maxType2Pages          = 232
)

// PollISO14443A performs a full ISO 14443-A discovery cycle:
// RF field reset -> WUPA -> Cascade 1..3 Anticollision & SELECT -> NTAG/Ultralight page dump.
// Returns ErrNoTag if no card is present in the field.
func (c *Chip) PollISO14443A() (*TagInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.resetAndEnableField(); err != nil {
		return nil, err
	}
	defer func() { _ = c.fieldOffLocked() }()

	atqa, err := c.sendWUPA()
	if err != nil {
		return nil, err
	}
	uid, sak, err := c.runAnticollisionCascade()
	if err != nil {
		return nil, err
	}

	tag := &TagInfo{UID: uid, ATQA: atqa, SAK: sak}
	if sak == sakType2 {
		tag.Pages = c.readType2Pages()
	}
	return tag, nil
}

func (c *Chip) resetAndEnableField() error {
	// Brief field reset guarantees any card in HALT or ISO14443-4 state returns to IDLE.
	if err := c.fieldOffLocked(); err != nil {
		return err
	}
	if c.guardT > 0 {
		time.Sleep(fieldResetGuard)
	}
	return c.fieldOnLocked()
}

func (c *Chip) runAnticollisionCascade() ([]byte, uint8, error) {
	cascadeCmds := [3]uint8{0x93, 0x95, 0x97}
	var uid []byte
	var sak uint8

	for _, sel := range cascadeCmds {
		clBytes, err := c.anticollision(sel)
		if err != nil {
			return nil, 0, err
		}
		sakByte, err := c.selectCascade(sel, clBytes)
		if err != nil {
			return nil, 0, err
		}
		sak = sakByte
		if clBytes[0] == cascadeTag || (sak&sakCascadeBit) != 0 {
			uid = append(uid, clBytes[1], clBytes[2], clBytes[3])
			continue
		}
		uid = append(uid, clBytes[0], clBytes[1], clBytes[2], clBytes[3])
		break
	}
	if len(uid) == 0 {
		return nil, 0, ErrProtocol
	}
	return uid, sak, nil
}

func (c *Chip) sendWUPA() ([2]byte, error) {
	if err := c.writeReg(regISO14443A, 0x00); err != nil {
		return [2]byte{}, err
	}
	if err := c.writeReg(regAux, 0x00); err != nil {
		return [2]byte{}, err
	}
	if err := c.setNoResponseTimer(5); err != nil {
		return [2]byte{}, err
	}
	c.clearInterrupts()
	if err := c.command(cmdClearFIFO); err != nil {
		return [2]byte{}, err
	}
	if err := c.command(cmdTransmitWUPA); err != nil {
		return [2]byte{}, err
	}
	rx, err := c.waitRx(25*time.Millisecond, false)
	if err != nil {
		return [2]byte{}, err
	}
	if len(rx) < 2 {
		return [2]byte{}, fmt.Errorf("%w: short ATQA (%d bytes)", ErrProtocol, len(rx))
	}
	return [2]byte{rx[0], rx[1]}, nil
}

func (c *Chip) anticollision(sel uint8) ([5]byte, error) {
	if err := c.writeReg(regISO14443A, iso14443AAntcl); err != nil {
		return [5]byte{}, err
	}
	if err := c.writeReg(regAux, auxNoCRCRx); err != nil {
		return [5]byte{}, err
	}
	if err := c.setNoResponseTimer(5); err != nil {
		return [5]byte{}, err
	}
	c.clearInterrupts()
	if err := c.command(cmdClearFIFO); err != nil {
		return [5]byte{}, err
	}
	if err := c.writeFIFO([]byte{sel, 0x20}); err != nil {
		return [5]byte{}, err
	}
	if err := c.setTxBytes(2); err != nil {
		return [5]byte{}, err
	}
	if err := c.command(cmdTransmitNoCRC); err != nil {
		return [5]byte{}, err
	}
	rx, err := c.waitRx(25*time.Millisecond, false)
	if err != nil {
		return [5]byte{}, err
	}
	if len(rx) < 5 {
		return [5]byte{}, fmt.Errorf("%w: short anticollision (%d bytes)", ErrProtocol, len(rx))
	}
	bcc := rx[0] ^ rx[1] ^ rx[2] ^ rx[3]
	if bcc != rx[4] {
		return [5]byte{}, fmt.Errorf("%w: BCC mismatch (got 0x%02X want 0x%02X)", ErrProtocol, rx[4], bcc)
	}
	return [5]byte{rx[0], rx[1], rx[2], rx[3], rx[4]}, nil
}

func (c *Chip) selectCascade(sel uint8, cl [5]byte) (uint8, error) {
	if err := c.writeReg(regISO14443A, 0x00); err != nil {
		return 0, err
	}
	if err := c.writeReg(regAux, 0x00); err != nil {
		return 0, err
	}
	if err := c.setNoResponseTimer(10); err != nil {
		return 0, err
	}
	c.clearInterrupts()
	if err := c.command(cmdClearFIFO); err != nil {
		return 0, err
	}
	frame := []byte{sel, 0x70, cl[0], cl[1], cl[2], cl[3], cl[4]}
	if err := c.writeFIFO(frame); err != nil {
		return 0, err
	}
	if err := c.setTxBytes(len(frame)); err != nil {
		return 0, err
	}
	if err := c.command(cmdTransmitWithCRC); err != nil {
		return 0, err
	}
	rx, err := c.waitRx(30*time.Millisecond, true)
	if err != nil {
		return 0, err
	}
	if len(rx) < 1 {
		return 0, fmt.Errorf("%w: empty SAK", ErrProtocol)
	}
	return rx[0], nil
}

func (c *Chip) readType2Pages() [][]byte {
	// Start by reading pages 0..3 (UID + Lock + Capability Container).
	first, err := c.readFourPages(0)
	if err != nil {
		return nil
	}
	pages := make([][]byte, 0, 48)
	pages = append(pages, first...)

	totalPages := defaultType2Pages
	if len(first) == 4 && len(first[3]) == 4 && first[3][0] == ndefCCMagic {
		// NDEF Capability Container: byte 2 = memory size / 8 bytes.
		if userPages := int(first[3][2]) * 2; userPages > 0 {
			totalPages = 4 + userPages
		}
	}
	totalPages = min(totalPages, maxType2Pages)

	for pg := 4; pg < totalPages; pg += 4 {
		chunk, err := c.readFourPages(uint8(pg))
		if err != nil {
			break
		}
		for i := range chunk {
			if pg+i >= totalPages {
				break
			}
			pages = append(pages, chunk[i])
		}
	}
	return pages
}

func (c *Chip) readFourPages(startPage uint8) ([][]byte, error) {
	if err := c.writeReg(regISO14443A, 0x00); err != nil {
		return nil, err
	}
	if err := c.writeReg(regAux, 0x00); err != nil {
		return nil, err
	}
	if err := c.setNoResponseTimer(10); err != nil {
		return nil, err
	}
	c.clearInterrupts()
	if err := c.command(cmdClearFIFO); err != nil {
		return nil, err
	}
	if err := c.writeFIFO([]byte{cmdType2Read, startPage}); err != nil {
		return nil, err
	}
	if err := c.setTxBytes(2); err != nil {
		return nil, err
	}
	if err := c.command(cmdTransmitWithCRC); err != nil {
		return nil, err
	}
	rx, err := c.waitRx(30*time.Millisecond, true)
	if err != nil {
		return nil, err
	}
	// ST25R3916 keeps the 2-byte CRC in FIFO -> 16 data bytes (+ 2 CRC bytes).
	if len(rx) < 16 {
		return nil, fmt.Errorf("%w: short READ(%d) response (%d bytes)", ErrProtocol, startPage, len(rx))
	}
	out := make([][]byte, 4)
	for i := range 4 {
		out[i] = append([]byte(nil), rx[i*4:(i+1)*4]...)
	}
	return out, nil
}
