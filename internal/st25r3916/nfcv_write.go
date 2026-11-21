package st25r3916

import (
	"context"
	"fmt"
)

// WriteNFCVBlocks writes consecutive 4-byte blocks to an ISO 15693 (NFC-V) tag using
// addressed WRITE_SINGLE_BLOCK (0x21) with optional READ_SINGLE_BLOCK (0x20) verification.
func (c *Chip) WriteNFCVBlocks(ctx context.Context, startBlock uint8, blocks [][4]byte, verify bool) (int, error) {
	if len(blocks) == 0 {
		return 0, fmt.Errorf("%w: no NFC-V blocks provided", ErrProtocol)
	}
	if int(startBlock)+len(blocks) > 256 {
		return 0, fmt.Errorf("%w: NFC-V block range exceeds 255", ErrProtocol)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	defer func() { _ = c.fieldOffLocked() }()

	if err := c.enableProtocolFieldLocked(modeNFCV, bitrate106, c.guardT); err != nil {
		return 0, err
	}
	invRx, err := c.txMultiFrameLocked([]byte{0x26, 0x01, 0x00})
	if err != nil {
		return 0, err
	}
	if len(invRx) < 10 || (invRx[0]&nfcvErrFlagMask) != 0 {
		return 0, fmt.Errorf("%w: invalid ISO15693 INVENTORY response", ErrProtocol)
	}
	uidLSB := invRx[2:10]
	written := 0
	for i, data := range blocks {
		if err := ctx.Err(); err != nil {
			return written, err
		}
		blk := uint8(int(startBlock) + i)
		if err := c.writeNFCVSingleBlockLocked(uidLSB, blk, data, verify); err != nil {
			return written, err
		}
		written++
	}
	return written, nil
}

func (c *Chip) writeNFCVSingleBlockLocked(uidLSB []byte, blk uint8, data [4]byte, verify bool) error {
	// ISO 15693 addressed WRITE_SINGLE_BLOCK: flags=0x22 (high data rate + addressed), cmd=0x21.
	frame := make([]byte, 0, 15)
	frame = append(frame, 0x22, 0x21)
	frame = append(frame, uidLSB...)
	frame = append(frame, blk)
	frame = append(frame, data[:]...)
	rx, err := c.txMultiFrameLocked(frame)
	if err != nil {
		return fmt.Errorf("st25r3916: write NFC-V block %d: %w", blk, err)
	}
	if len(rx) < 1 || (rx[0]&nfcvErrFlagMask) != 0 {
		return fmt.Errorf("%w: NFC-V write rejected at block %d", ErrProtocol, blk)
	}
	if !verify {
		return nil
	}
	readFrame := make([]byte, 0, 11)
	readFrame = append(readFrame, 0x22, 0x20)
	readFrame = append(readFrame, uidLSB...)
	readFrame = append(readFrame, blk)
	blkRx, err := c.txMultiFrameLocked(readFrame)
	if err != nil || len(blkRx) < 5 || (blkRx[0]&nfcvErrFlagMask) != 0 {
		return fmt.Errorf("%w: NFC-V read-back failed at block %d", ErrProtocol, blk)
	}
	for j := range 4 {
		if blkRx[1+j] != data[j] {
			return fmt.Errorf("%w: NFC-V verify mismatch at block %d", ErrProtocol, blk)
		}
	}
	return nil
}
