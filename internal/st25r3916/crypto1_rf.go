package st25r3916

import (
	"crypto/rand"
	"encoding/binary"
)

func (c *Chip) selectCardForCrypto1Locked() ([]byte, error) {
	if err := c.resetAndEnableField(); err != nil {
		return nil, err
	}
	if _, err := c.sendWUPA(); err != nil {
		return nil, err
	}
	uid, _, err := c.runAnticollisionCascade()
	return uid, err
}

func (c *Chip) authSectorWithCandidatesLocked(blk uint8, uid []byte, sa MifareSectorAuth, nested bool) (*Crypto1, error) {
	type cand struct {
		cmd uint8
		key [6]byte
	}
	cands := []cand{{cmdMifareAuthKeyA, defaultMifareKey}}
	if sa.HasKeyA && sa.KeyA != defaultMifareKey {
		cands = append(cands, cand{cmdMifareAuthKeyA, sa.KeyA})
	}
	if sa.HasKeyB {
		cands = append(cands, cand{cmdMifareAuthKeyB, sa.KeyB})
	}
	var lastErr error
	for i, cd := range cands {
		if i > 0 || nested {
			if _, err := c.selectCardForCrypto1Locked(); err != nil {
				return nil, err
			}
		}
		cs, err := c.authCrypto1Locked(cd.cmd, blk, cd.key, uid)
		if err == nil {
			return cs, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

// authCrypto1Locked executes authentic 3-pass MIFARE Classic Crypto1 mutual authentication
// over ST25R3916 transparent 9-bit parity framing using a caller-known 6-byte key.
func (c *Chip) authCrypto1Locked(authCmd, block uint8, key [6]byte, uid []byte) (*Crypto1, error) {
	if len(uid) < 4 {
		return nil, ErrInvalidUIDLen
	}
	nt, err := c.requestMifareNonceLocked(authCmd, block)
	if err != nil {
		return nil, err
	}
	uid32 := binary.BigEndian.Uint32(uid[len(uid)-4:])
	cs := NewCrypto1(key)
	cs.Word(uid32^nt, false)

	txPlain, txPar := buildCrypto1AuthReply(cs, nt)
	rawRx, err := c.txCrypto1RawLocked(txPlain[:], txPar[:])
	if err != nil {
		return nil, err
	}
	return verifyCrypto1TagToken(cs, rawRx, nt)
}

func (c *Chip) requestMifareNonceLocked(authCmd, block uint8) (uint32, error) {
	if err := c.writeReg(regISO14443A, 0x00); err != nil {
		return 0, err
	}
	if err := c.writeReg(regAux, auxNoCRCRx); err != nil {
		return 0, err
	}
	if err := c.setNoResponseTimer(mifareAuthTimeout); err != nil {
		return 0, err
	}
	c.clearInterrupts()
	if err := c.command(cmdClearFIFO); err != nil {
		return 0, err
	}
	if err := c.writeFIFO([]byte{authCmd, block}); err != nil {
		return 0, err
	}
	if err := c.setTxBytes(2); err != nil {
		return 0, err
	}
	if err := c.command(cmdTransmitWithCRC); err != nil {
		return 0, err
	}
	ntBytes, err := c.waitRx(mifareAuthWait, false)
	if err != nil {
		return 0, err
	}
	if len(ntBytes) < 4 {
		return 0, ErrProtocol
	}
	return binary.BigEndian.Uint32(ntBytes[:4]), nil
}

func buildCrypto1AuthReply(cs *Crypto1, nt uint32) (txPlain [8]byte, txPar [8]uint8) {
	var nr [4]byte
	_, _ = rand.Read(nr[:])
	nrEnc, nrPar := cs.EncryptReaderNonce(nr)

	var arBytes [4]byte
	binary.BigEndian.PutUint32(arBytes[:], PRNGSuccessor(nt, 64))
	arEnc, arPar := cs.EncryptWithParity(arBytes[:])

	copy(txPlain[0:4], nrEnc[:])
	copy(txPlain[4:8], arEnc[:])
	copy(txPar[0:4], nrPar[:])
	copy(txPar[4:8], arPar[:])
	return txPlain, txPar
}

func verifyCrypto1TagToken(cs *Crypto1, rawRx []byte, nt uint32) (*Crypto1, error) {
	atEnc, atPar, ok := UnpackParityBits(rawRx, 4)
	if !ok {
		return nil, ErrProtocol
	}
	atPlain, parOK := cs.DecryptWithParity(atEnc, atPar)
	if !parOK {
		return nil, ErrCRC
	}
	if binary.BigEndian.Uint32(atPlain) != PRNGSuccessor(nt, 96) {
		return nil, ErrProtocol
	}
	return cs, nil
}

func (c *Chip) writeCrypto1BlockLocked(cs *Crypto1, blk uint8, data [16]byte) error {
	cmdFrame := AppendCRC16A([]byte{cmdMifareWriteBlk, blk})
	cCmd, pCmd := cs.EncryptWithParity(cmdFrame)
	ackRx, err := c.txCrypto1RawLocked(cCmd, pCmd)
	if err != nil {
		return err
	}
	if len(ackRx) < 1 || cs.DecryptNibble4(ackRx[0]) != type2ACKNibble {
		return ErrWriteNAK
	}
	dataFrame := AppendCRC16A(data[:])
	cData, pData := cs.EncryptWithParity(dataFrame)
	ackRx2, err := c.txCrypto1RawLocked(cData, pData)
	if err != nil {
		return err
	}
	if len(ackRx2) < 1 || cs.DecryptNibble4(ackRx2[0]) != type2ACKNibble {
		return ErrWriteNAK
	}
	return nil
}

func (c *Chip) readCrypto1BlockLocked(cs *Crypto1, blk uint8) ([16]byte, error) {
	var out [16]byte
	cmdFrame := AppendCRC16A([]byte{cmdType2Read, blk})
	cCmd, pCmd := cs.EncryptWithParity(cmdFrame)
	rawRx, err := c.txCrypto1RawLocked(cCmd, pCmd)
	if err != nil {
		return out, err
	}
	rxEnc, rxPar, ok := UnpackParityBits(rawRx, 18)
	if !ok {
		return out, ErrProtocol
	}
	plain, parOK := cs.DecryptWithParity(rxEnc, rxPar)
	if !parOK || !VerifyCRC16A(plain) {
		return out, ErrCRC
	}
	copy(out[:], plain[:16])
	return out, nil
}

func (c *Chip) txCrypto1RawLocked(data []byte, par []uint8) ([]byte, error) {
	packed, totalBits := PackParityBits(data, par)
	if err := c.writeReg(regISO14443A, iso14443ANoTxPar|iso14443ANoRxPar); err != nil {
		return nil, err
	}
	if err := c.writeReg(regAux, auxNoCRCRx); err != nil {
		return nil, err
	}
	if err := c.setNoResponseTimer(writeTimeoutMs); err != nil {
		return nil, err
	}
	c.clearInterrupts()
	if err := c.command(cmdClearFIFO); err != nil {
		return nil, err
	}
	if err := c.writeFIFO(packed); err != nil {
		return nil, err
	}
	if err := c.setTxBits(totalBits); err != nil {
		return nil, err
	}
	if err := c.command(cmdTransmitNoCRC); err != nil {
		return nil, err
	}
	return c.waitRx(writeWaitDuration, false)
}

func (c *Chip) setTxBits(totalBits int) error {
	fullBytes := totalBits / 8
	remBits := totalBits % 8
	if err := c.writeReg(regTxBytes1, uint8((fullBytes>>5)&0xFF)); err != nil {
		return err
	}
	return c.writeReg(regTxBytes2, uint8(((fullBytes&0x1F)<<3)|(remBits&0x07)))
}
