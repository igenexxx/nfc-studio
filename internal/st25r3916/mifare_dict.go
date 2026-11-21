package st25r3916

import (
	"context"
	"errors"
	"fmt"
	"time"

	"nfc-studio/internal/nfcfile"
)

// ErrNotMifareClassic is returned when a dictionary scan targets a non-MIFARE Classic card.
var ErrNotMifareClassic = errors.New("st25r3916: tag is not MIFARE Classic")

const (
	sakMifare1K        uint8 = 0x08
	sakMifare4K        uint8 = 0x18
	cmdMifareAuthKeyA  uint8 = 0x60
	cmdMifareAuthKeyB  uint8 = 0x61
	mifare1KSectors          = 16
	mifare4KSectors          = 40
	mifareAuthTimeout  uint32 = 10
	mifareAuthWait            = 25 * time.Millisecond
)

// SectorResult holds recovered keys and readable 16-byte blocks for one MIFARE Classic sector.
type SectorResult struct {
	Sector   int        `json:"sector"`
	HasKeyA  bool       `json:"has_key_a"`
	KeyA     [6]byte    `json:"-"`
	HasKeyB  bool       `json:"has_key_b"`
	KeyB     [6]byte    `json:"-"`
	Blocks   [][16]byte `json:"-"`
	Readable []bool     `json:"readable"`
}

// DictProgress reports live sector-by-sector recovery progress during a dictionary scan.
type DictProgress struct {
	Sector       int
	TotalSectors int
	KeysFoundA   int
	KeysFoundB   int
	BlocksRead   int
}

// MifareDump holds the complete or partial result of a MIFARE Classic dictionary read.
type MifareDump struct {
	UID        []byte
	ATQA       [2]byte
	SAK        uint8
	CardSize   string // "1K" or "4K"
	Sectors    []SectorResult
	KeysTested int
	ElapsedMs  int64
}

// ToNfcTag converts a MifareDump into a Flipper Zero .nfc v4 tag with recovered keys in sector trailers.
func (d *MifareDump) ToNfcTag() *nfcfile.NfcTag {
	totalBlocks := mifareTotalBlocks(d.CardSize)
	pages := make([][]byte, totalBlocks)
	unreadable := make([]bool, totalBlocks)
	for i := range totalBlocks {
		pages[i] = make([]byte, 16)
		unreadable[i] = true
	}
	for _, sec := range d.Sectors {
		firstBlk, count := mifareSectorGeometry(sec.Sector)
		for i := range count {
			idx := firstBlk + i
			if idx >= totalBlocks {
				break
			}
			if i < len(sec.Readable) && sec.Readable[i] && i < len(sec.Blocks) {
				copy(pages[idx], sec.Blocks[i][:])
				unreadable[idx] = false
			}
		}
		trailerIdx := firstBlk + count - 1
		if trailerIdx < totalBlocks && (sec.HasKeyA || sec.HasKeyB) {
			if sec.HasKeyA {
				copy(pages[trailerIdx][0:6], sec.KeyA[:])
			}
			if sec.HasKeyB {
				copy(pages[trailerIdx][10:16], sec.KeyB[:])
			}
			unreadable[trailerIdx] = false
		}
	}
	return &nfcfile.NfcTag{
		FileType:   "Flipper NFC device",
		Version:    4,
		DeviceType: "Mifare Classic",
		UID:        append([]byte(nil), d.UID...),
		ATQA:       d.ATQA,
		SAK:        d.SAK,
		PagesTotal: totalBlocks,
		Pages:      pages,
		MultiMeta: nfcfile.MultiMeta{
			Protocol:   "NFC-A",
			MifareType: d.CardSize,
			Unreadable: unreadable,
		},
	}
}

// ReadMifareClassicDict tests known keys against MIFARE Classic 1K/4K sectors (no CRYPTO1 cracking).
func (c *Chip) ReadMifareClassicDict(ctx context.Context, keys [][6]byte, onProgress func(DictProgress)) (*MifareDump, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	start := time.Now()
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
	cardSize, totalSectors, err := classifyMifareSAK(sak)
	if err != nil {
		return nil, err
	}

	dump := &MifareDump{
		UID:      uid,
		ATQA:     atqa,
		SAK:      sak,
		CardSize: cardSize,
		Sectors:  make([]SectorResult, 0, totalSectors),
	}
	if err := c.scanMifareSectorsLocked(ctx, dump, totalSectors, keys, onProgress); err != nil {
		dump.ElapsedMs = time.Since(start).Milliseconds()
		return dump, err
	}
	dump.ElapsedMs = time.Since(start).Milliseconds()
	return dump, nil
}

func classifyMifareSAK(sak uint8) (string, int, error) {
	switch sak {
	case sakMifare1K:
		return "1K", mifare1KSectors, nil
	case sakMifare4K:
		return "4K", mifare4KSectors, nil
	default:
		return "", 0, fmt.Errorf("%w: SAK=0x%02X", ErrNotMifareClassic, sak)
	}
}

func (c *Chip) scanMifareSectorsLocked(ctx context.Context, dump *MifareDump, totalSectors int, keys [][6]byte, onProgress func(DictProgress)) error {
	var prog DictProgress
	prog.TotalSectors = totalSectors

	for sec := range totalSectors {
		if err := ctx.Err(); err != nil {
			return err
		}
		res, removed := c.scanOneSectorLocked(ctx, sec, dump.UID, keys, &dump.KeysTested)
		dump.Sectors = append(dump.Sectors, res)
		if res.HasKeyA {
			prog.KeysFoundA++
		}
		if res.HasKeyB {
			prog.KeysFoundB++
		}
		for _, ok := range res.Readable {
			if ok {
				prog.BlocksRead++
			}
		}
		prog.Sector = sec + 1
		if onProgress != nil {
			onProgress(prog)
		}
		if removed {
			return ErrNoTag
		}
	}
	return nil
}

func (c *Chip) scanOneSectorLocked(ctx context.Context, sec int, uid []byte, keys [][6]byte, tested *int) (SectorResult, bool) {
	firstBlk, blkCount := mifareSectorGeometry(sec)
	res := SectorResult{
		Sector:   sec,
		Blocks:   make([][16]byte, blkCount),
		Readable: make([]bool, blkCount),
	}
	if key, ok, removed := c.trySectorKeysLocked(ctx, cmdMifareAuthKeyA, uint8(firstBlk), uid, keys, tested); removed {
		return res, true
	} else if ok {
		res.HasKeyA = true
		res.KeyA = key
		c.readSectorBlocksLocked(firstBlk, &res)
	}
	if key, ok, removed := c.trySectorKeysLocked(ctx, cmdMifareAuthKeyB, uint8(firstBlk), uid, keys, tested); removed {
		return res, true
	} else if ok {
		res.HasKeyB = true
		res.KeyB = key
		c.readSectorBlocksLocked(firstBlk, &res)
	}
	return res, false
}

func (c *Chip) trySectorKeysLocked(ctx context.Context, authCmd, block uint8, uid []byte, keys [][6]byte, tested *int) ([6]byte, bool, bool) {
	for _, key := range keys {
		if ctx.Err() != nil {
			return [6]byte{}, false, false
		}
		*tested++
		err := c.authMifareBlockLocked(authCmd, block, key, uid)
		if err == nil {
			return key, true, false
		}
		if err := c.reselectCardLocked(); err != nil {
			if errors.Is(err, ErrNoTag) {
				return [6]byte{}, false, true
			}
		}
	}
	return [6]byte{}, false, false
}

func (c *Chip) readSectorBlocksLocked(firstBlk int, res *SectorResult) {
	for i := range res.Blocks {
		if res.Readable[i] {
			continue
		}
		blk, err := c.readMifareBlockLocked(uint8(firstBlk + i))
		if err != nil {
			continue
		}
		res.Blocks[i] = blk
		res.Readable[i] = true
	}
}

func (c *Chip) reselectCardLocked() error {
	if err := c.resetAndEnableField(); err != nil {
		return err
	}
	if _, err := c.sendWUPA(); err != nil {
		return err
	}
	_, _, err := c.runAnticollisionCascade()
	return err
}

func (c *Chip) authMifareBlockLocked(authCmd, block uint8, key [6]byte, uid []byte) error {
	if err := c.writeReg(regISO14443A, 0x00); err != nil {
		return err
	}
	if err := c.writeReg(regAux, 0x00); err != nil {
		return err
	}
	if err := c.setNoResponseTimer(mifareAuthTimeout); err != nil {
		return err
	}
	c.clearInterrupts()
	if err := c.command(cmdClearFIFO); err != nil {
		return err
	}
	// Send MIFARE Classic Auth command [0x60|0x61, block] with CRC and check for 4-byte tag nonce.
	frame := []byte{authCmd, block}
	if err := c.writeFIFO(frame); err != nil {
		return err
	}
	if err := c.setTxBytes(len(frame)); err != nil {
		return err
	}
	if err := c.command(cmdTransmitWithCRC); err != nil {
		return err
	}
	rx, err := c.waitRx(mifareAuthWait, false)
	if err != nil {
		return err
	}
	if len(rx) < 4 {
		return ErrProtocol
	}
	// Known-key response exchange (key + uid context); expects 4-byte tag auth token.
	resp := make([]byte, 0, 10)
	resp = append(resp, key[:]...)
	if len(uid) >= 4 {
		resp = append(resp, uid[len(uid)-4:]...)
	}
	c.clearInterrupts()
	_ = c.command(cmdClearFIFO)
	if err := c.writeFIFO(resp); err != nil {
		return err
	}
	if err := c.setTxBytes(len(resp)); err != nil {
		return err
	}
	if err := c.command(cmdTransmitNoCRC); err != nil {
		return err
	}
	ack, err := c.waitRx(mifareAuthWait, false)
	if err != nil || len(ack) < 4 {
		if err == nil {
			err = ErrProtocol
		}
		return err
	}
	return nil
}

func (c *Chip) readMifareBlockLocked(block uint8) ([16]byte, error) {
	var out [16]byte
	pages, err := c.readFourPages(block)
	if err != nil || len(pages) < 4 {
		if err == nil {
			err = ErrProtocol
		}
		return out, err
	}
	for i := range 4 {
		copy(out[i*4:(i+1)*4], pages[i])
	}
	return out, nil
}

func mifareSectorGeometry(sector int) (firstBlock, blockCount int) {
	if sector < 32 {
		return sector * 4, 4
	}
	return 128 + (sector-32)*16, 16
}

func mifareTotalBlocks(cardSize string) int {
	if cardSize == "4K" {
		return 256
	}
	return 64
}
