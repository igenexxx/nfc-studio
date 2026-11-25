package nfcfile

import (
	"errors"
	"fmt"
	"strings"
)

// ErrNilTag is returned when BuildWritePlan receives a nil tag pointer.
var ErrNilTag = errors.New("nfcfile: tag is nil")

// WriteStrategy identifies the physical card-writing strategy inferred for an NfcTag.
type WriteStrategy string

const (
	StrategyNTAGPages     WriteStrategy = "ntag_pages"
	StrategyMagicGen1A    WriteStrategy = "magic_gen1a"
	StrategyMagicGen2     WriteStrategy = "magic_gen2"
	StrategyMifareSectors WriteStrategy = "mifare_sectors"
	StrategyNFCVBlocks    WriteStrategy = "nfcv_blocks"
	StrategyUnsupported   WriteStrategy = "unsupported"

	ntagUserStartPage = 4
	ntag213UserEnd    = 39
	ntag215UserEnd    = 129
	ntag216UserEnd    = 225
	ntag213CCSize     = 0x12
	ntag215CCSize     = 0x3E
	ntag216CCSize     = 0x6D
	mifare1KMaxBlocks = 64
	mifare4KMaxBlocks = 256
	sakType2NTAG      = 0x00
	sakMifare1K       = 0x08
	sakMifare4K       = 0x18
	sakISODEP         = 0x20
)

// PlanOptions customizes how BuildWritePlan prepares a safe physical write plan.
type PlanOptions struct {
	MagicGen         string // "gen1a" (default), "gen2", or "regular" (sectors only, no Block 0)
	UIDOnly          bool   // Magic cards only: clone Block 0 UID without writing sector data blocks
	IncludeTrailers  bool   // Include MIFARE sector trailers (default false to avoid bricking access bits)
	MaxNTAGUserPages int    // Optional user-page cap for NTAG21x (0 = auto from CC page / dump length)
}

// MifareBlockOp describes one 16-byte MIFARE Classic block scheduled for writing.
type MifareBlockOp struct {
	Block     uint8
	Sector    int
	Data      [16]byte
	IsTrailer bool
}

// MifareSectorKey stores known Key A / Key B extracted from a sector trailer in a .nfc dump.
type MifareSectorKey struct {
	Sector  int
	HasKeyA bool
	KeyA    [6]byte
	HasKeyB bool
	KeyB    [6]byte
}

// WritePlan is the Single Source of Truth write specification shared by UI and MCP.
type WritePlan struct {
	Strategy          WriteStrategy     `json:"strategy"`
	DeviceType        string            `json:"device_type"`
	Summary           string            `json:"summary"`
	Notice            string            `json:"notice,omitempty"`
	Reason            string            `json:"reason,omitempty"`
	UID               []byte            `json:"-"`
	ATQA              [2]byte           `json:"-"`
	SAK               uint8             `json:"-"`
	NTAGStartPage     uint8             `json:"ntag_start_page,omitempty"`
	NTAGPages         [][4]byte         `json:"-"`
	MifareBlocks      []MifareBlockOp   `json:"-"`
	SectorKeys        []MifareSectorKey `json:"-"`
	SkippedUnreadable int               `json:"skipped_unreadable,omitempty"`
	SkippedTrailers   int               `json:"skipped_trailers,omitempty"`
	NFCVStartBlock    uint8             `json:"nfcv_start_block,omitempty"`
	NFCVBlocks        [][4]byte         `json:"-"`
}

// IsMifareTrailerBlock reports whether blk is a MIFARE Classic 1K/4K sector trailer block.
func IsMifareTrailerBlock(blk int) bool {
	if blk < 0 || blk >= mifare4KMaxBlocks {
		return false
	}
	if blk < 128 {
		return blk%4 == 3
	}
	return (blk-128)%16 == 15
}

// MifareBlockSector returns the 0-based sector number for a MIFARE Classic 1K/4K block index.
func MifareBlockSector(blk int) int {
	if blk < 128 {
		return blk / 4
	}
	return 32 + (blk-128)/16
}

// MifareSectorTrailerBlock returns the trailer block index for a MIFARE Classic 1K/4K sector.
func MifareSectorTrailerBlock(sector int) int {
	if sector < 32 {
		return sector*4 + 3
	}
	return 128 + (sector-32)*16 + 15
}

// BuildWritePlan translates a Flipper .nfc tag into a safe, validated physical card write plan.
func BuildWritePlan(tag *NfcTag, opts PlanOptions) (WritePlan, error) {
	if tag == nil {
		return WritePlan{}, ErrNilTag
	}
	plan := WritePlan{
		DeviceType: tag.DeviceType,
		UID:        append([]byte(nil), tag.UID...),
		ATQA:       tag.ATQA,
		SAK:        tag.SAK,
	}
	proto := effectiveProtocol(tag)
	switch proto {
	case "NFC-V":
		return planNFCVWrite(tag, plan), nil
	case "NFC-B":
		return unsupportedPlan(plan, "ISO 14443-B (NFC-B) cards are read-only on ST25R3916"), nil
	case "NFC-F":
		return unsupportedPlan(plan, "FeliCa (NFC-F) cloning onto blank tags is unsupported; use Emulate"), nil
	}
	if isMifareClassicTag(tag) {
		return planMifareWrite(tag, plan, opts), nil
	}
	if isNTAGType2Tag(tag) {
		return planNTAGWrite(tag, plan, opts), nil
	}
	if tag.SAK == sakISODEP || strings.Contains(strings.ToUpper(tag.DeviceType), "DESFIRE") || strings.Contains(strings.ToUpper(tag.DeviceType), "EMV") {
		return unsupportedPlan(plan, "ISO 14443-4 / DESFire / EMV smartcards cannot be cloned onto blank tags"), nil
	}
	return unsupportedPlan(plan, fmt.Sprintf("Unsupported card type (%s, SAK=0x%02X)", tag.DeviceType, tag.SAK)), nil
}

func effectiveProtocol(tag *NfcTag) string {
	if tag.Protocol != "" {
		return strings.ToUpper(strings.TrimSpace(tag.Protocol))
	}
	return inferProtocolFromDeviceType(tag.DeviceType)
}

func isMifareClassicTag(tag *NfcTag) bool {
	if tag.MifareType != "" || tag.SAK == sakMifare1K || tag.SAK == sakMifare4K {
		return true
	}
	upper := strings.ToUpper(tag.DeviceType)
	return strings.Contains(upper, "MIFARE CLASSIC")
}

func isNTAGType2Tag(tag *NfcTag) bool {
	upper := strings.ToUpper(tag.DeviceType)
	if strings.Contains(upper, "NTAG") || strings.Contains(upper, "ULTRALIGHT") || strings.Contains(upper, "MFU") {
		return true
	}
	return tag.SAK == sakType2NTAG && len(tag.Pages) >= ntagUserStartPage+1 && len(tag.Pages[0]) == 4
}

func planNTAGWrite(tag *NfcTag, plan WritePlan, opts PlanOptions) WritePlan {
	if len(tag.Pages) <= ntagUserStartPage {
		return unsupportedPlan(plan, "NTAG dump has no writable user pages (P04+)")
	}
	userEnd := inferNTAGUserEnd(tag)
	if opts.MaxNTAGUserPages > 0 {
		userEnd = min(userEnd, ntagUserStartPage+opts.MaxNTAGUserPages-1)
	}
	lastPage := min(len(tag.Pages)-1, userEnd)
	pages := make([][4]byte, 0, max(0, lastPage-ntagUserStartPage+1))
	for pg := ntagUserStartPage; pg <= lastPage; pg++ {
		if len(tag.Pages[pg]) < 4 {
			break
		}
		var p [4]byte
		copy(p[:], tag.Pages[pg][:4])
		pages = append(pages, p)
	}
	if len(pages) == 0 {
		return unsupportedPlan(plan, "NTAG dump contains no valid 4-byte user pages")
	}
	plan.Strategy = StrategyNTAGPages
	plan.NTAGStartPage = ntagUserStartPage
	plan.NTAGPages = pages
	plan.Summary = fmt.Sprintf("NTAG user P%02d..P%02d (%d pg)", ntagUserStartPage, ntagUserStartPage+len(pages)-1, len(pages))
	plan.Notice = "UID P00-P02 & locks are RO (user pages only)"
	return plan
}

func inferNTAGUserEnd(tag *NfcTag) int {
	if len(tag.Pages) > 3 && len(tag.Pages[3]) >= 4 && tag.Pages[3][0] == 0xE1 {
		switch tag.Pages[3][2] {
		case ntag213CCSize:
			return ntag213UserEnd
		case ntag215CCSize:
			return ntag215UserEnd
		case ntag216CCSize:
			return ntag216UserEnd
		}
	}
	upper := strings.ToUpper(tag.DeviceType)
	switch {
	case strings.Contains(upper, "213"):
		return ntag213UserEnd
	case strings.Contains(upper, "216"):
		return ntag216UserEnd
	default:
		return ntag215UserEnd
	}
}

func planMifareWrite(tag *NfcTag, plan WritePlan, opts PlanOptions) WritePlan {
	gen := strings.ToLower(strings.TrimSpace(opts.MagicGen))
	if gen == "" {
		gen = "gen1a"
	}
	if gen != "regular" && len(tag.UID) != 4 {
		return unsupportedPlan(plan, fmt.Sprintf("Magic UID clone requires 4-byte UID (got %dB)", len(tag.UID)))
	}
	switch gen {
	case "gen1a":
		plan.Strategy = StrategyMagicGen1A
	case "gen2":
		plan.Strategy = StrategyMagicGen2
	case "regular":
		plan.Strategy = StrategyMifareSectors
	default:
		return unsupportedPlan(plan, fmt.Sprintf("Unknown MIFARE write mode %q", gen))
	}

	plan.SectorKeys = extractMifareSectorKeys(tag)
	if !opts.UIDOnly {
		collectMifareBlocks(tag, &plan, opts.IncludeTrailers)
	}
	if plan.Strategy == StrategyMifareSectors && len(plan.MifareBlocks) == 0 {
		return unsupportedPlan(plan, "MIFARE dump has no writable 16-byte data blocks")
	}
	plan.Summary, plan.Notice = formatMifarePlanText(plan, opts)
	return plan
}

func collectMifareBlocks(tag *NfcTag, plan *WritePlan, includeTrailers bool) {
	maxBlocks := mifare1KMaxBlocks
	if strings.EqualFold(tag.MifareType, "4K") || tag.SAK == sakMifare4K || len(tag.Pages) > mifare1KMaxBlocks {
		maxBlocks = mifare4KMaxBlocks
	}
	limit := min(len(tag.Pages), maxBlocks)
	for blk := 1; blk < limit; blk++ {
		if blk < len(tag.Unreadable) && tag.Unreadable[blk] {
			plan.SkippedUnreadable++
			continue
		}
		if len(tag.Pages[blk]) < 16 {
			continue
		}
		trailer := IsMifareTrailerBlock(blk)
		if trailer && !includeTrailers {
			plan.SkippedTrailers++
			continue
		}
		var data [16]byte
		copy(data[:], tag.Pages[blk][:16])
		plan.MifareBlocks = append(plan.MifareBlocks, MifareBlockOp{
			Block:     uint8(blk),
			Sector:    MifareBlockSector(blk),
			Data:      data,
			IsTrailer: trailer,
		})
	}
}

func extractMifareSectorKeys(tag *NfcTag) []MifareSectorKey {
	if len(tag.Pages) < 4 {
		return nil
	}
	sectors := 16
	if strings.EqualFold(tag.MifareType, "4K") || tag.SAK == sakMifare4K || len(tag.Pages) > mifare1KMaxBlocks {
		sectors = 40
	}
	var out []MifareSectorKey
	for sec := range sectors {
		tr := MifareSectorTrailerBlock(sec)
		if tr >= len(tag.Pages) || len(tag.Pages[tr]) < 16 {
			continue
		}
		if tr < len(tag.Unreadable) && tag.Unreadable[tr] {
			continue
		}
		sk := MifareSectorKey{Sector: sec, HasKeyA: true, HasKeyB: true}
		copy(sk.KeyA[:], tag.Pages[tr][0:6])
		copy(sk.KeyB[:], tag.Pages[tr][10:16])
		out = append(out, sk)
	}
	return out
}

func formatMifarePlanText(plan WritePlan, opts PlanOptions) (summary, notice string) {
	switch plan.Strategy {
	case StrategyMagicGen1A:
		if opts.UIDOnly || len(plan.MifareBlocks) == 0 {
			return "Gen1A Magic: Block 0 UID only", "Backdoor 0x40/0x43 (no Crypto1 needed)"
		}
		notice = "Trailers skipped [SAFE] to protect access bits"
		if opts.IncludeTrailers {
			notice = "WARN: Sector trailers ENABLED (verify access bits!)"
		}
		return fmt.Sprintf("Gen1A Magic: Blk0 UID + %d blk", len(plan.MifareBlocks)), notice
	case StrategyMagicGen2:
		if opts.UIDOnly || len(plan.MifareBlocks) == 0 {
			return "Gen2/CUID: Block 0 UID (Crypto1)", "Auths Sec0 via known Key A (default FFFFFFFFFFFF)"
		}
		notice = "Crypto1 known-key auth; trailers skipped [SAFE]"
		if opts.IncludeTrailers {
			notice = "WARN: Crypto1 + sector trailers ENABLED!"
		}
		return fmt.Sprintf("Gen2/CUID: Blk0 + %d blk (Crypto1)", len(plan.MifareBlocks)), notice
	default:
		notice = "Standard card: UID unchanged; trailers skipped [SAFE]"
		if opts.IncludeTrailers {
			notice = "Standard card: UID unchanged; trailers ENABLED!"
		}
		return fmt.Sprintf("MIFARE Classic: %d data blk", len(plan.MifareBlocks)), notice
	}
}

func planNFCVWrite(tag *NfcTag, plan WritePlan) WritePlan {
	if len(tag.Pages) == 0 {
		return unsupportedPlan(plan, "ISO 15693 dump contains no 4-byte blocks")
	}
	blocks := make([][4]byte, 0, min(len(tag.Pages), 256))
	for i := range min(len(tag.Pages), 256) {
		if len(tag.Pages[i]) < 4 {
			break
		}
		var b [4]byte
		copy(b[:], tag.Pages[i][:4])
		blocks = append(blocks, b)
	}
	if len(blocks) == 0 {
		return unsupportedPlan(plan, "ISO 15693 dump contains no valid 4-byte blocks")
	}
	plan.Strategy = StrategyNFCVBlocks
	plan.NFCVStartBlock = 0
	plan.NFCVBlocks = blocks
	plan.Summary = fmt.Sprintf("ISO15693 Blk 0..%d (%d blk)", len(blocks)-1, len(blocks))
	plan.Notice = "UID is factory RO; writes user blocks via 0x21"
	return plan
}

func unsupportedPlan(plan WritePlan, reason string) WritePlan {
	plan.Strategy = StrategyUnsupported
	plan.Summary = "Unsupported for blank write"
	plan.Reason = reason
	return plan
}
