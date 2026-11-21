package st25r3916

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"nfc-studio/internal/nfcfile"
)

var (
	// ErrNotISODEP is returned when a card's SAK lacks the ISO 14443-4 (0x20) bit.
	ErrNotISODEP = errors.New("st25r3916: tag does not support ISO-DEP (SAK 0x20)")
	// ErrForbiddenAPDU is returned when a caller attempts any non-read-only APDU.
	ErrForbiddenAPDU = errors.New("st25r3916: non-read-only APDU blocked")
)

const (
	sakISODEPBit       uint8  = 0x20
	cmdRATS            uint8  = 0xE0
	ratsParam          uint8  = 0x80
	pcbIBlockBase      uint8  = 0x02
	pcbWTX             uint8  = 0xF2
	isoDEPTimeoutMs    uint32 = 50
	isoDEPWait                = 80 * time.Millisecond
	maxWTXExtensions          = 5
	emvTagAID          uint16 = 0x4F
	emvTagLabel        uint16 = 0x50
	emvTagTrack2       uint16 = 0x57
	emvTagPAN          uint16 = 0x5A
	emvTagExpiry       uint16 = 0x5F24
)

var apduSelectPPSE = []byte{
	0x00, 0xA4, 0x04, 0x00, 0x0E,
	'2', 'P', 'A', 'Y', '.', 'S', 'Y', 'S', '.', 'D', 'D', 'F', '0', '1',
	0x00,
}

// ValidateReadOnlyAPDU enforces the strict read-only EMV APDU allowlist.
// Only SELECT (00 A4), READ RECORD (00 B2), and empty-PDOL GPO (80 A8) are allowed.
func ValidateReadOnlyAPDU(apdu []byte) error {
	if len(apdu) < 4 {
		return fmt.Errorf("%w: APDU shorter than 4 bytes", ErrForbiddenAPDU)
	}
	cla, ins := apdu[0], apdu[1]
	switch {
	case cla == 0x00 && ins == 0xA4: // SELECT
		return nil
	case cla == 0x00 && ins == 0xB2: // READ RECORD
		return nil
	case cla == 0x80 && ins == 0xA8: // Read-only GPO with zero-amount empty PDOL only
		if len(apdu) >= 5 && apdu[4] <= 2 {
			return nil
		}
		return fmt.Errorf("%w: non-empty PDOL GPO blocked", ErrForbiddenAPDU)
	default:
		return fmt.Errorf("%w: CLA=0x%02X INS=0x%02X", ErrForbiddenAPDU, cla, ins)
	}
}

// TransceiveISODEP transmits a read-only APDU wrapped in an ISO-DEP I-Block (handling S(WTX)).
func (c *Chip) TransceiveISODEP(ctx context.Context, apdu []byte) ([]byte, error) {
	if err := ValidateReadOnlyAPDU(apdu); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	var blockNum uint8
	return c.transceiveISODEPLocked(ctx, apdu, &blockNum)
}

// InspectEMV performs a strictly read-only RATS -> SELECT PPSE -> SELECT AID -> READ RECORD flow.
// All PANs are unconditionally masked before returning.
func (c *Chip) InspectEMV(ctx context.Context) (*nfcfile.EMVInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.resetAndEnableField(); err != nil {
		return nil, err
	}
	defer func() { _ = c.fieldOffLocked() }()

	if _, err := c.sendWUPA(); err != nil {
		return nil, err
	}
	uid, sak, err := c.runAnticollisionCascade()
	if err != nil {
		return nil, err
	}
	if (sak & sakISODEPBit) == 0 {
		return nil, fmt.Errorf("%w: SAK=0x%02X", ErrNotISODEP, sak)
	}
	ats, err := c.sendRATSLocked()
	if err != nil {
		return nil, err
	}

	info := &nfcfile.EMVInfo{
		UIDHex: formatUIDHex(uid),
		ATSHex: nfcfile.FormatHex(ats),
	}
	var blockNum uint8
	if app, ok := c.inspectPPSEAndRecordsLocked(ctx, &blockNum); ok {
		info.Apps = append(info.Apps, app)
	}
	return info, nil
}

func (c *Chip) inspectPPSEAndRecordsLocked(ctx context.Context, blockNum *uint8) (nfcfile.EMVApp, bool) {
	ppseResp, err := c.transceiveISODEPLocked(ctx, apduSelectPPSE, blockNum)
	if err != nil || !isSW9000(ppseResp) {
		return nfcfile.EMVApp{}, false
	}
	tlvs, err := nfcfile.ParseEMVTLV(stripSW(ppseResp))
	if err != nil {
		return nfcfile.EMVApp{}, false
	}
	aidBytes := tlvs[emvTagAID]
	if len(aidBytes) == 0 {
		return nfcfile.EMVApp{}, false
	}
	app := nfcfile.EMVApp{
		AID:   strings.ToUpper(hex.EncodeToString(aidBytes)),
		Label: strings.TrimSpace(string(tlvs[emvTagLabel])),
	}
	c.readEMVAppRecordsLocked(ctx, aidBytes, &app, blockNum)
	return app, true
}

func (c *Chip) readEMVAppRecordsLocked(ctx context.Context, aid []byte, app *nfcfile.EMVApp, blockNum *uint8) {
	selAID := buildSelectAIDAPDU(aid)
	resp, err := c.transceiveISODEPLocked(ctx, selAID, blockNum)
	if err != nil || !isSW9000(resp) {
		return
	}
	if app.Label == "" {
		if tlvs, err := nfcfile.ParseEMVTLV(stripSW(resp)); err == nil && len(tlvs[emvTagLabel]) > 0 {
			app.Label = strings.TrimSpace(string(tlvs[emvTagLabel]))
		}
	}
	for sfi := uint8(1); sfi <= 2; sfi++ {
		for rec := uint8(1); rec <= 4; rec++ {
			if ctx.Err() != nil {
				return
			}
			apdu := []byte{0x00, 0xB2, rec, (sfi << 3) | 0x04, 0x00}
			recResp, err := c.transceiveISODEPLocked(ctx, apdu, blockNum)
			if err != nil || !isSW9000(recResp) {
				break
			}
			applyEMVRecordTLVs(stripSW(recResp), app)
			if app.MaskedPAN != "" && app.Expiry != "" {
				return
			}
		}
	}
}

func applyEMVRecordTLVs(payload []byte, app *nfcfile.EMVApp) {
	tlvs, err := nfcfile.ParseEMVTLV(payload)
	if err != nil {
		return
	}
	if app.MaskedPAN == "" && len(tlvs[emvTagPAN]) > 0 {
		app.MaskedPAN = nfcfile.MaskPAN(nfcfile.DecodeBCDDigits(tlvs[emvTagPAN]))
	}
	if len(tlvs[emvTagTrack2]) > 0 {
		masked, exp := nfcfile.ExtractTrack2Masked(tlvs[emvTagTrack2])
		if app.MaskedPAN == "" && masked != "" {
			app.MaskedPAN = masked
		}
		if app.Expiry == "" && exp != "" {
			app.Expiry = exp
		}
	}
	if app.Expiry == "" && len(tlvs[emvTagExpiry]) > 0 {
		app.Expiry = nfcfile.FormatEMVExpiry(tlvs[emvTagExpiry])
	}
}

func (c *Chip) sendRATSLocked() ([]byte, error) {
	rx, err := c.txISO14443AWithCRCLocked([]byte{cmdRATS, ratsParam}, isoDEPTimeoutMs, isoDEPWait)
	if err != nil {
		return nil, err
	}
	if len(rx) < 1 {
		return nil, fmt.Errorf("%w: empty ATS", ErrProtocol)
	}
	return stripTrailingCRC(rx), nil
}

func (c *Chip) transceiveISODEPLocked(ctx context.Context, apdu []byte, blockNum *uint8) ([]byte, error) {
	if err := ValidateReadOnlyAPDU(apdu); err != nil {
		return nil, err
	}
	pcb := pcbIBlockBase | (*blockNum & 0x01)
	frame := make([]byte, 1+len(apdu))
	frame[0] = pcb
	copy(frame[1:], apdu)
	*blockNum ^= 1

	rx, err := c.txISO14443AWithCRCLocked(frame, isoDEPTimeoutMs, isoDEPWait)
	if err != nil {
		return nil, err
	}
	for range maxWTXExtensions {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		rx = stripTrailingCRC(rx)
		if len(rx) >= 2 && rx[0] == pcbWTX {
			rx, err = c.txISO14443AWithCRCLocked([]byte{pcbWTX, rx[1]}, isoDEPTimeoutMs, isoDEPWait)
			if err != nil {
				return nil, err
			}
			continue
		}
		if len(rx) < 1 {
			return nil, fmt.Errorf("%w: empty ISO-DEP response", ErrProtocol)
		}
		return rx[1:], nil
	}
	return nil, fmt.Errorf("%w: too many WTX frames", ErrProtocol)
}

func (c *Chip) txISO14443AWithCRCLocked(frame []byte, timeoutMs uint32, wait time.Duration) ([]byte, error) {
	if err := c.writeReg(regISO14443A, 0x00); err != nil {
		return nil, err
	}
	if err := c.writeReg(regAux, 0x00); err != nil {
		return nil, err
	}
	if err := c.setNoResponseTimer(timeoutMs); err != nil {
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
	return c.waitRx(wait, true)
}

func buildSelectAIDAPDU(aid []byte) []byte {
	apdu := make([]byte, 0, 6+len(aid))
	apdu = append(apdu, 0x00, 0xA4, 0x04, 0x00, uint8(len(aid)))
	apdu = append(apdu, aid...)
	apdu = append(apdu, 0x00)
	return apdu
}

func stripTrailingCRC(rx []byte) []byte {
	if len(rx) > 2 {
		return rx[:len(rx)-2]
	}
	return rx
}

func isSW9000(resp []byte) bool {
	n := len(resp)
	return n >= 2 && resp[n-2] == 0x90 && resp[n-1] == 0x00
}

func stripSW(resp []byte) []byte {
	if len(resp) <= 2 {
		return nil
	}
	return resp[:len(resp)-2]
}
