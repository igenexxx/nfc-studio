package st25r3916

import (
	"bytes"
	"context"
	"errors"
	"fmt"
)

// ErrBlock0Locked is returned when a non-magic MIFARE Classic card rejects Block 0 writes with NAK.
var ErrBlock0Locked = errors.New("st25r3916: block 0 locked (regular MIFARE Classic, need Magic card)")

// MifareBlockWrite describes one 16-byte MIFARE Classic block to write.
type MifareBlockWrite struct {
	Block uint8
	Data  [16]byte
}

// MifareSectorAuth supplies known Key A / Key B credentials for Crypto1 sector authentication.
type MifareSectorAuth struct {
	Sector  int
	HasKeyA bool
	KeyA    [6]byte
	HasKeyB bool
	KeyB    [6]byte
}

// MagicDumpReport summarizes a multi-block Magic Card or MIFARE Classic dump write.
type MagicDumpReport struct {
	Gen                  MagicCardGen `json:"gen"`
	TargetUID            string       `json:"target_uid"`
	ReadBackUID          string       `json:"read_back_uid,omitempty"`
	BlocksWritten        int          `json:"blocks_written"`
	BlocksSkipped        int          `json:"blocks_skipped,omitempty"`
	Verified             bool         `json:"verified"`
	Block0LockedFallback bool         `json:"block0_locked_fallback,omitempty"`
	FailedBlock          int          `json:"failed_block"`
}

// MagicRegular targets standard non-magic MIFARE Classic cards (data blocks only, no Block 0).
const MagicRegular MagicCardGen = "regular"

var defaultMifareKey = [6]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}

// WriteMagicGen1ADump unlocks a Gen1A Magic Card (0x40/0x43 backdoor), writes Block 0 UID
// plus all requested data blocks in plaintext, verifies read-back, and re-polls UID.
func (c *Chip) WriteMagicGen1ADump(
	ctx context.Context,
	uid []byte,
	atqa [2]byte,
	sak uint8,
	blocks []MifareBlockWrite,
	verify bool,
	onProgress func(done, total int),
) (MagicDumpReport, error) {
	blk0, err := BuildBlock0(uid, sak, atqa)
	if err != nil {
		return MagicDumpReport{Gen: MagicGen1A, FailedBlock: 0}, err
	}
	if err := ctx.Err(); err != nil {
		return MagicDumpReport{Gen: MagicGen1A, FailedBlock: -1}, err
	}
	rep := MagicDumpReport{Gen: MagicGen1A, TargetUID: formatUIDHex(uid), FailedBlock: -1}
	total := 1 + len(blocks)

	c.mu.Lock()
	defer c.mu.Unlock()
	defer func() { _ = c.fieldOffLocked() }()

	if err := c.openGen1ABackdoorLocked(); err != nil {
		rep.FailedBlock = 0
		return rep, err
	}
	if err := c.writePlainMifareBlockLocked(0, blk0); err != nil {
		rep.FailedBlock = 0
		return rep, err
	}
	rep.BlocksWritten++
	notifyProgress(onProgress, rep.BlocksWritten, total)

	for _, op := range blocks {
		if err := ctx.Err(); err != nil {
			rep.FailedBlock = int(op.Block)
			return rep, err
		}
		if err := c.writePlainMifareBlockLocked(op.Block, op.Data); err != nil {
			rep.FailedBlock = int(op.Block)
			return rep, err
		}
		if verify {
			if err := c.verifyPlainMifareBlockLocked(op.Block, op.Data); err != nil {
				rep.FailedBlock = int(op.Block)
				return rep, err
			}
		}
		rep.BlocksWritten++
		notifyProgress(onProgress, rep.BlocksWritten, total)
	}

	cloneRep, err := c.verifyClonedUIDLocked(CloneReport{Gen: MagicGen1A, TargetUID: rep.TargetUID}, uid)
	rep.ReadBackUID = cloneRep.ReadBackID
	if err != nil {
		rep.FailedBlock = 0
		return rep, err
	}
	rep.Verified = verify || len(blocks) == 0
	return rep, nil
}

// WriteMifareCrypto1Dump writes blocks to a Gen2/CUID Magic Card (writeBlock0=true) or a
// standard MIFARE Classic card (writeBlock0=false) using pure-Go known-key Crypto1 authentication.
func (c *Chip) WriteMifareCrypto1Dump(
	ctx context.Context,
	gen MagicCardGen,
	uid []byte,
	atqa [2]byte,
	sak uint8,
	writeBlock0 bool,
	blocks []MifareBlockWrite,
	sectorAuth []MifareSectorAuth,
	verify bool,
	onProgress func(done, total int),
) (MagicDumpReport, error) {
	return c.writeMifareCrypto1Core(ctx, gen, uid, atqa, sak, writeBlock0, false, blocks, sectorAuth, verify, onProgress)
}

// WriteMifareCrypto1Auto attempts Gen2/CUID Block 0 write first and, if Block 0 returns NAK
// (regular non-magic MIFARE Classic), either falls back to writing data blocks or returns ErrBlock0Locked.
func (c *Chip) WriteMifareCrypto1Auto(
	ctx context.Context,
	uid []byte,
	atqa [2]byte,
	sak uint8,
	blocks []MifareBlockWrite,
	sectorAuth []MifareSectorAuth,
	allowRegularFallback bool,
	verify bool,
	onProgress func(done, total int),
) (MagicDumpReport, error) {
	return c.writeMifareCrypto1Core(ctx, MagicGen2CUID, uid, atqa, sak, true, allowRegularFallback, blocks, sectorAuth, verify, onProgress)
}

func (c *Chip) writeMifareCrypto1Core(
	ctx context.Context,
	gen MagicCardGen,
	uid []byte,
	atqa [2]byte,
	sak uint8,
	writeBlock0 bool,
	allowRegularFallback bool,
	blocks []MifareBlockWrite,
	sectorAuth []MifareSectorAuth,
	verify bool,
	onProgress func(done, total int),
) (MagicDumpReport, error) {
	var blk0 [16]byte
	if writeBlock0 {
		var err error
		blk0, err = BuildBlock0(uid, sak, atqa)
		if err != nil {
			return MagicDumpReport{Gen: gen, FailedBlock: 0}, err
		}
	}
	if err := ctx.Err(); err != nil {
		return MagicDumpReport{Gen: gen, FailedBlock: -1}, err
	}
	rep := MagicDumpReport{Gen: gen, TargetUID: formatUIDHex(uid), FailedBlock: -1}
	total := len(blocks)
	if writeBlock0 {
		total++
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	defer func() { _ = c.fieldOffLocked() }()

	cardUID, err := c.selectCardForCrypto1Locked()
	if err != nil {
		rep.FailedBlock = 0
		return rep, err
	}
	keyMap := indexSectorAuth(sectorAuth)
	var cs *Crypto1
	activeSec := -1

	writeOne := func(blk uint8, data [16]byte) error {
		sec := blockToSector(int(blk))
		if sec != activeSec || cs == nil {
			var authErr error
			cs, authErr = c.authSectorWithCandidatesLocked(blk, cardUID, keyMap[sec], cs != nil)
			if authErr != nil {
				return authErr
			}
			activeSec = sec
		}
		if err := c.writeCrypto1BlockLocked(cs, blk, data); err != nil {
			return err
		}
		if verify {
			got, err := c.readCrypto1BlockLocked(cs, blk)
			if err != nil || !bytes.Equal(got[:], data[:]) {
				return fmt.Errorf("%w: block %d crypto1 verify failed", ErrVerifyFailed, blk)
			}
		}
		return nil
	}

	block0Written := false
	if writeBlock0 {
		err := writeOne(0, blk0)
		if err != nil {
			if errors.Is(err, ErrWriteNAK) {
				if allowRegularFallback && len(blocks) > 0 {
					rep.Gen = MagicRegular
					rep.Block0LockedFallback = true
					rep.ReadBackUID = formatUIDHex(cardUID)
					total = len(blocks)
					if newUID, selErr := c.selectCardForCrypto1Locked(); selErr == nil {
						cardUID = newUID
					}
					cs = nil
					activeSec = -1
				} else {
					rep.FailedBlock = 0
					return rep, ErrBlock0Locked
				}
			} else {
				rep.FailedBlock = 0
				return rep, err
			}
		} else {
			block0Written = true
			rep.BlocksWritten++
			notifyProgress(onProgress, rep.BlocksWritten, total)
			if len(blocks) > 0 {
				if newUID, selErr := c.selectCardForCrypto1Locked(); selErr == nil {
					cardUID = newUID
				}
				cs = nil
				activeSec = -1
			}
		}
	}

	for _, op := range blocks {
		if err := ctx.Err(); err != nil {
			rep.FailedBlock = int(op.Block)
			return rep, err
		}
		if err := writeOne(op.Block, op.Data); err != nil {
			rep.FailedBlock = int(op.Block)
			return rep, err
		}
		rep.BlocksWritten++
		notifyProgress(onProgress, rep.BlocksWritten, total)
	}

	if block0Written {
		cloneRep, err := c.verifyClonedUIDLocked(CloneReport{Gen: gen, TargetUID: rep.TargetUID}, uid)
		rep.ReadBackUID = cloneRep.ReadBackID
		if err != nil {
			rep.FailedBlock = 0
			return rep, err
		}
	}
	rep.Verified = verify || block0Written
	return rep, nil
}

func (c *Chip) openGen1ABackdoorLocked() error {
	if err := c.resetAndEnableField(); err != nil {
		return err
	}
	_ = c.sendHALTLocked()
	if err := c.txShort7BitExpectACKLocked(cmdGen1ABackdoor1); err != nil {
		return err
	}
	return c.txExpectACKLocked([]byte{cmdGen1ABackdoor2}, false)
}

func (c *Chip) writePlainMifareBlockLocked(blk uint8, data [16]byte) error {
	if err := c.txExpectACKLocked([]byte{cmdMifareWriteBlk, blk}, true); err != nil {
		return err
	}
	return c.txExpectACKLocked(data[:], true)
}

func (c *Chip) verifyPlainMifareBlockLocked(blk uint8, want [16]byte) error {
	got, err := c.readMifareBlockLocked(blk)
	if err != nil {
		return fmt.Errorf("%w: block %d read-back failed: %v", ErrVerifyFailed, blk, err)
	}
	if !bytes.Equal(got[:], want[:]) {
		return fmt.Errorf("%w: block %d mismatch", ErrVerifyFailed, blk)
	}
	return nil
}

func blockToSector(blk int) int {
	if blk < 128 {
		return blk / 4
	}
	return 32 + (blk-128)/16
}

func indexSectorAuth(list []MifareSectorAuth) map[int]MifareSectorAuth {
	m := make(map[int]MifareSectorAuth, len(list))
	for _, s := range list {
		m[s.Sector] = s
	}
	return m
}

func notifyProgress(fn func(done, total int), done, total int) {
	if fn != nil {
		fn(done, total)
	}
}
