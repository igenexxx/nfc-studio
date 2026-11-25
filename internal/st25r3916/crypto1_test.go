package st25r3916

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestCrypto1MutualAuthAndStreamRoundTrip(t *testing.T) {
	key := [6]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
	uid := uint32(0xDEADBEEF)
	nt := uint32(0x01020304)
	nr := [4]byte{0x11, 0x22, 0x33, 0x44}

	// Reader side initializes LFSR with key and feeds uid^nt.
	reader := NewCrypto1(key)
	reader.Word(uid^nt, false)
	nrEnc, nrPar := reader.EncryptReaderNonce(nr)
	ar := PRNGSuccessor(nt, 64)
	var arBuf [4]byte
	binary.BigEndian.PutUint32(arBuf[:], ar)
	arEnc, arPar := reader.EncryptWithParity(arBuf[:])

	// Tag side performs mirror decryption with the same known key.
	tag := NewCrypto1(key)
	tag.Word(uid^nt, false)
	var tagNR [4]byte
	for i := range 4 {
		ks := tag.Byte(nrEnc[i], true)
		tagNR[i] = nrEnc[i] ^ ks
		if nrPar[i] != (OddParity(tagNR[i]) ^ tag.FilterBit()) {
			t.Fatalf("nr parity mismatch at byte %d", i)
		}
	}
	if tagNR != nr {
		t.Fatalf("tag recovered nr=%X, want %X", tagNR, nr)
	}
	tagAR, arOK := tag.DecryptWithParity(arEnc, arPar)
	if !arOK || binary.BigEndian.Uint32(tagAR) != ar {
		t.Fatalf("tag ar verify failed: ok=%v got=%X want=%08X", arOK, tagAR, ar)
	}

	// Tag replies with encrypted at = PRNGSuccessor(nt, 96).
	at := PRNGSuccessor(nt, 96)
	var atBuf [4]byte
	binary.BigEndian.PutUint32(atBuf[:], at)
	atEnc, atPar := tag.EncryptWithParity(atBuf[:])
	atPlain, atOK := reader.DecryptWithParity(atEnc, atPar)
	if !atOK || binary.BigEndian.Uint32(atPlain) != at {
		t.Fatalf("reader at verify failed: ok=%v got=%X want=%08X", atOK, atPlain, at)
	}

	// Verify subsequent encrypted 18-byte WRITE block + 9-bit parity pack/unpack round-trip.
	payload := AppendCRC16A([]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15})
	cBytes, cPar := reader.EncryptWithParity(payload)
	packed, totalBits := PackParityBits(cBytes, cPar)
	if totalBits != 18*9 {
		t.Fatalf("totalBits = %d, want %d", totalBits, 18*9)
	}
	uBytes, uPar, ok := UnpackParityBits(packed, 18)
	if !ok {
		t.Fatal("UnpackParityBits returned ok=false")
	}
	plain, parOK := tag.DecryptWithParity(uBytes, uPar)
	if !parOK || !VerifyCRC16A(plain) || !bytes.Equal(plain, payload) {
		t.Fatalf("encrypted block round-trip failed: parOK=%v crc=%v", parOK, VerifyCRC16A(plain))
	}
}

func TestPRNGSuccessorPeriodAndCRC16A(t *testing.T) {
	// Any valid 32-bit state on the 16-bit MIFARE LFSR cycle must repeat after 2^16 - 1 = 65535 steps.
	onCycle := PRNGSuccessor(0x12345678, 32)
	if got := PRNGSuccessor(onCycle, 65535); got != onCycle {
		t.Fatalf("PRNGSuccessor(onCycle, 65535) = 0x%08X, want 0x%08X", got, onCycle)
	}
	if PRNGSuccessor(onCycle, 64) == onCycle {
		t.Fatal("PRNGSuccessor(onCycle, 64) did not advance")
	}

	// ISO 14443-A standard test vector: READ(0) = [0x30, 0x00] -> CRC [0x02, 0xA8].
	crc := CRC16A([]byte{0x30, 0x00})
	if crc != [2]byte{0x02, 0xA8} {
		t.Fatalf("CRC16A(30 00) = %02X %02X, want 02 A8", crc[0], crc[1])
	}
	if !VerifyCRC16A([]byte{0x30, 0x00, 0x02, 0xA8}) || VerifyCRC16A([]byte{0x30, 0x00, 0x00, 0x00}) || VerifyCRC16A([]byte{0x30}) {
		t.Fatal("VerifyCRC16A failed boundary checks")
	}
	if OddParity(0x00) != 1 || OddParity(0x01) != 0 || OddParity(0xFF) != 1 {
		t.Fatal("OddParity failed bit-weight checks")
	}
	if _, _, ok := UnpackParityBits([]byte{0x00}, 2); ok {
		t.Fatal("UnpackParityBits must fail on short buffer")
	}
}

func FuzzPackUnpackParityBits(f *testing.F) {
	f.Add([]byte{0x00, 0xFF, 0xA5, 0x5A})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 64 {
			data = data[:64]
		}
		par := make([]uint8, len(data))
		for i, b := range data {
			par[i] = OddParity(b)
		}
		packed, bits := PackParityBits(data, par)
		if bits != len(data)*9 {
			t.Fatalf("bits=%d want %d", bits, len(data)*9)
		}
		gotData, gotPar, ok := UnpackParityBits(packed, len(data))
		if !ok || !bytes.Equal(gotData, data) || !bytes.Equal(gotPar, par) {
			t.Fatalf("round-trip mismatch ok=%v", ok)
		}
	})
}
