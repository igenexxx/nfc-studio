package st25r3916

import (
	"bytes"
	"context"
	"fmt"
	"time"
)

// MagicCardGen identifies the magic card backdoor/write generation.
type MagicCardGen string

const (
	MagicGen1A    MagicCardGen = "gen1a"
	MagicGen2CUID MagicCardGen = "gen2"
)

// CloneReport summarizes a Magic Card UID clone operation.
type CloneReport struct {
	Gen        MagicCardGen `json:"gen"`
	TargetUID  string       `json:"target_uid"`
	Verified   bool         `json:"verified"`
	ReadBackID string       `json:"read_back_uid,omitempty"`
}

// BuildBlock0 constructs a 16-byte MIFARE Classic Block 0 with XOR BCC at byte 4.
func BuildBlock0(uid []byte, sak uint8, atqa [2]byte) ([16]byte, error) {
	var blk [16]byte
	if len(uid) != 4 {
		return blk, fmt.Errorf("%w: got %d bytes", ErrInvalidUIDLen, len(uid))
	}
	copy(blk[0:4], uid)
	blk[4] = uid[0] ^ uid[1] ^ uid[2] ^ uid[3]
	blk[5] = sak
	blk[6] = atqa[0]
	blk[7] = atqa[1]
	blk[8] = 0x44
	blk[9] = 0x00
	return blk, nil
}

// CloneMagicUID writes a 4-byte UID Block 0 to a Gen1A or Gen2/CUID Magic Card and verifies via re-poll.
func (c *Chip) CloneMagicUID(ctx context.Context, gen MagicCardGen, uid []byte, atqa [2]byte, sak uint8, keyA [6]byte) (CloneReport, error) {
	blk0, err := BuildBlock0(uid, sak, atqa)
	if err != nil {
		return CloneReport{Gen: gen}, err
	}
	if gen != MagicGen1A && gen != MagicGen2CUID {
		return CloneReport{Gen: gen}, fmt.Errorf("%w: unknown magic gen %q", ErrProtocol, gen)
	}
	if err := ctx.Err(); err != nil {
		return CloneReport{Gen: gen}, err
	}
	report := CloneReport{Gen: gen, TargetUID: formatUIDHex(uid)}

	c.mu.Lock()
	defer c.mu.Unlock()
	defer func() { _ = c.fieldOffLocked() }()

	if gen == MagicGen1A {
		err = c.cloneGen1ALocked(blk0)
	} else {
		err = c.cloneGen2Locked(blk0, keyA)
	}
	if err != nil {
		return report, err
	}
	return c.verifyClonedUIDLocked(report, uid)
}

func (c *Chip) cloneGen1ALocked(blk0 [16]byte) error {
	if err := c.resetAndEnableField(); err != nil {
		return err
	}
	_ = c.sendHALTLocked()
	if err := c.txShort7BitExpectACKLocked(cmdGen1ABackdoor1); err != nil {
		return err
	}
	if err := c.txExpectACKLocked([]byte{cmdGen1ABackdoor2}, false); err != nil {
		return err
	}
	if err := c.txExpectACKLocked([]byte{cmdMifareWriteBlk, 0x00}, true); err != nil {
		return err
	}
	return c.txExpectACKLocked(blk0[:], true)
}

func (c *Chip) cloneGen2Locked(blk0 [16]byte, keyA [6]byte) error {
	uid, err := c.selectCardForCrypto1Locked()
	if err != nil {
		return err
	}
	cs, err := c.authCrypto1Locked(cmdMifareAuthKeyA, 0, keyA, uid)
	if err != nil {
		return err
	}
	return c.writeCrypto1BlockLocked(cs, 0, blk0)
}

func (c *Chip) sendHALTLocked() error {
	if err := c.writeReg(regISO14443A, 0x00); err != nil {
		return err
	}
	if err := c.writeReg(regAux, 0x00); err != nil {
		return err
	}
	if err := c.setNoResponseTimer(5); err != nil {
		return err
	}
	c.clearInterrupts()
	_ = c.command(cmdClearFIFO)
	if err := c.writeFIFO([]byte{cmdISO14443AHalt, 0x00}); err != nil {
		return err
	}
	if err := c.setTxBytes(2); err != nil {
		return err
	}
	if err := c.command(cmdTransmitWithCRC); err != nil {
		return err
	}
	_, _ = c.waitRx(10*time.Millisecond, false)
	return nil
}

func (c *Chip) txShort7BitExpectACKLocked(cmdByte uint8) error {
	if err := c.writeReg(regISO14443A, 0x00); err != nil {
		return err
	}
	if err := c.writeReg(regAux, auxNoCRCRx); err != nil {
		return err
	}
	if err := c.setNoResponseTimer(writeTimeoutMs); err != nil {
		return err
	}
	c.clearInterrupts()
	if err := c.command(cmdClearFIFO); err != nil {
		return err
	}
	if err := c.writeFIFO([]byte{cmdByte}); err != nil {
		return err
	}
	// 7-bit short frame: 0 full bytes, 7 trailing bits in regTxBytes2[2:0].
	if err := c.writeReg(regTxBytes1, 0x00); err != nil {
		return err
	}
	if err := c.writeReg(regTxBytes2, 0x07); err != nil {
		return err
	}
	if err := c.command(cmdTransmitNoCRC); err != nil {
		return err
	}
	rx, err := c.waitRx(writeWaitDuration, false)
	if err != nil {
		return err
	}
	if len(rx) < 1 || (rx[0]&0x0F) != type2ACKNibble {
		return ErrWriteNAK
	}
	return nil
}

func (c *Chip) verifyClonedUIDLocked(report CloneReport, targetUID []byte) (CloneReport, error) {
	if err := c.resetAndEnableField(); err != nil {
		return report, err
	}
	if _, err := c.sendWUPA(); err != nil {
		return report, fmt.Errorf("%w: re-poll WUPA failed: %v", ErrVerifyFailed, err)
	}
	readUID, _, err := c.runAnticollisionCascade()
	if err != nil {
		return report, fmt.Errorf("%w: re-poll select failed: %v", ErrVerifyFailed, err)
	}
	report.ReadBackID = formatUIDHex(readUID)
	if !bytes.Equal(readUID, targetUID) {
		return report, fmt.Errorf("%w: got UID %s want %s", ErrVerifyFailed, report.ReadBackID, report.TargetUID)
	}
	report.Verified = true
	return report, nil
}

func formatUIDHex(uid []byte) string {
	var b bytes.Buffer
	for i, v := range uid {
		if i > 0 {
			b.WriteByte(' ')
		}
		fmt.Fprintf(&b, "%02X", v)
	}
	return b.String()
}
