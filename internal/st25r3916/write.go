package st25r3916

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"
)

var (
	// ErrProtectedPage is returned when a Type 2 write targets UID/Lock/CC/Config pages without AllowSystemPages.
	ErrProtectedPage = errors.New("st25r3916: protected system/lock/config page")
	// ErrWriteNAK is returned when a tag rejects a WRITE or Magic command with a non-ACK reply.
	ErrWriteNAK = errors.New("st25r3916: tag returned NAK on write")
	// ErrVerifyFailed is returned when read-back verification disagrees with written page bytes.
	ErrVerifyFailed = errors.New("st25r3916: read-back verification mismatch")
	// ErrInvalidUIDLen is returned when a Magic Card clone UID is not 4 bytes.
	ErrInvalidUIDLen = errors.New("st25r3916: magic clone requires 4-byte UID")
)

const (
	cmdType2Write     uint8  = 0xA2
	cmdMifareWriteBlk uint8  = 0xA0
	cmdISO14443AHalt  uint8  = 0x50
	cmdGen1ABackdoor1 uint8  = 0x40
	cmdGen1ABackdoor2 uint8  = 0x43
	type2ACKNibble    uint8  = 0x0A
	ntag213CCSize     uint8  = 0x12
	ntag215CCSize     uint8  = 0x3E
	ntag216CCSize     uint8  = 0x6D
	writeTimeoutMs    uint32 = 15
	writeWaitDuration        = 35 * time.Millisecond
)

// NTAGModel describes the memory geometry of an NTAG213/215/216 tag.
type NTAGModel struct {
	Name          string
	UserStartPage uint8
	UserEndPage   uint8
	TotalPages    uint8
}

// WritePageOp represents a single 4-byte NTAG Type 2 page write.
type WritePageOp struct {
	Page uint8
	Data [4]byte
}

// WriteOptions controls safety guards, verification, and optional progress updates for Type 2 writes.
type WriteOptions struct {
	AllowSystemPages bool
	VerifyReadBack   bool
	OnProgress       func(written, total int)
}

// WriteReport summarizes the outcome of a Type 2 multi-page write.
type WriteReport struct {
	Model        string `json:"model"`
	PagesWritten int    `json:"pages_written"`
	Verified     bool   `json:"verified"`
	FailedPage   int    `json:"failed_page"`
}

// IdentifyNTAGModel parses Capability Container Page 3 to identify NTAG213/215/216.
func IdentifyNTAGModel(ccPage [4]byte) (NTAGModel, error) {
	if ccPage[0] != ndefCCMagic {
		return NTAGModel{}, fmt.Errorf("%w: invalid NDEF CC magic 0x%02X", ErrProtocol, ccPage[0])
	}
	switch ccPage[2] {
	case ntag213CCSize:
		return NTAGModel{Name: "NTAG213", UserStartPage: 4, UserEndPage: 39, TotalPages: 45}, nil
	case ntag215CCSize:
		return NTAGModel{Name: "NTAG215", UserStartPage: 4, UserEndPage: 129, TotalPages: 135}, nil
	case ntag216CCSize:
		return NTAGModel{Name: "NTAG216", UserStartPage: 4, UserEndPage: 225, TotalPages: 231}, nil
	default:
		return NTAGModel{}, fmt.Errorf("%w: unsupported NTAG CC size 0x%02X", ErrProtocol, ccPage[2])
	}
}

// WriteNTAGPage writes a single 4-byte user page with read-back verification and system-page protection.
func (c *Chip) WriteNTAGPage(page uint8, data [4]byte) error {
	_, err := c.WriteType2Pages(
		context.Background(),
		[]WritePageOp{{Page: page, Data: data}},
		WriteOptions{AllowSystemPages: false, VerifyReadBack: true},
	)
	return err
}

// WriteNTAGPages is an alias for WriteType2Pages.
func (c *Chip) WriteNTAGPages(ctx context.Context, ops []WritePageOp, opts WriteOptions) (WriteReport, error) {
	return c.WriteType2Pages(ctx, ops, opts)
}

// WriteType2Pages validates page guards, writes NTAG21x pages via WRITE (0xA2), and optionally verifies read-back.
func (c *Chip) WriteType2Pages(ctx context.Context, ops []WritePageOp, opts WriteOptions) (WriteReport, error) {
	report := WriteReport{FailedPage: -1}
	if len(ops) == 0 {
		return report, errors.New("st25r3916: no pages to write")
	}
	if err := ctx.Err(); err != nil {
		return report, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.resetAndEnableField(); err != nil {
		return report, err
	}
	defer func() { _ = c.fieldOffLocked() }()

	if _, err := c.sendWUPA(); err != nil {
		return report, err
	}
	if _, _, err := c.runAnticollisionCascade(); err != nil {
		return report, err
	}
	model, err := c.detectNTAGModelLocked()
	if err != nil {
		return report, err
	}
	report.Model = model.Name
	if err := validateWriteOps(ops, model, opts.AllowSystemPages); err != nil {
		return report, err
	}
	return c.executeType2WritesLocked(ctx, ops, opts, report)
}

func (c *Chip) detectNTAGModelLocked() (NTAGModel, error) {
	first, err := c.readFourPages(0)
	if err != nil || len(first) < 4 || len(first[3]) < 4 {
		if err == nil {
			err = fmt.Errorf("%w: missing CC page", ErrProtocol)
		}
		return NTAGModel{}, err
	}
	var cc [4]byte
	copy(cc[:], first[3])
	return IdentifyNTAGModel(cc)
}

func validateWriteOps(ops []WritePageOp, model NTAGModel, allowSystem bool) error {
	for _, op := range ops {
		if op.Page >= model.TotalPages {
			return fmt.Errorf("%w: page %d out of range for %s", ErrProtectedPage, op.Page, model.Name)
		}
		if !allowSystem && (op.Page < model.UserStartPage || op.Page > model.UserEndPage) {
			return fmt.Errorf("%w: page %d (%s user range %d..%d)", ErrProtectedPage, op.Page, model.Name, model.UserStartPage, model.UserEndPage)
		}
	}
	return nil
}

func (c *Chip) executeType2WritesLocked(ctx context.Context, ops []WritePageOp, opts WriteOptions, report WriteReport) (WriteReport, error) {
	for _, op := range ops {
		if err := ctx.Err(); err != nil {
			report.FailedPage = int(op.Page)
			return report, err
		}
		if err := c.writeSingleType2PageLocked(op.Page, op.Data); err != nil {
			report.FailedPage = int(op.Page)
			return report, err
		}
		if opts.VerifyReadBack {
			if err := c.verifyType2PageLocked(op.Page, op.Data); err != nil {
				report.FailedPage = int(op.Page)
				return report, err
			}
		}
		report.PagesWritten++
		if opts.OnProgress != nil {
			opts.OnProgress(report.PagesWritten, len(ops))
		}
	}
	report.Verified = opts.VerifyReadBack
	return report, nil
}

func (c *Chip) writeSingleType2PageLocked(page uint8, data [4]byte) error {
	frame := []byte{cmdType2Write, page, data[0], data[1], data[2], data[3]}
	return c.txExpectACKLocked(frame, true)
}

func (c *Chip) verifyType2PageLocked(page uint8, want [4]byte) error {
	pages, err := c.readFourPages(page)
	if err != nil || len(pages) == 0 || len(pages[0]) < 4 {
		if err == nil {
			err = ErrVerifyFailed
		}
		return fmt.Errorf("%w: page %d read-back failed: %v", ErrVerifyFailed, page, err)
	}
	if !bytes.Equal(pages[0][:4], want[:]) {
		return fmt.Errorf("%w: page %d got %X want %X", ErrVerifyFailed, page, pages[0][:4], want[:])
	}
	return nil
}

func (c *Chip) txExpectACKLocked(frame []byte, withCRC bool) error {
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
	if err := c.writeFIFO(frame); err != nil {
		return err
	}
	if err := c.setTxBytes(len(frame)); err != nil {
		return err
	}
	cmd := cmdTransmitNoCRC
	if withCRC {
		cmd = cmdTransmitWithCRC
	}
	if err := c.command(cmd); err != nil {
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
