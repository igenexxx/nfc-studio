package st25r3916

import (
	"encoding/binary"
	"math/bits"
)

const (
	lfPolyOdd  uint32 = 0x29ce5c
	lfPolyEven uint32 = 0x870804
	crc16AInit uint16 = 0x6363
	crc16APoly uint16 = 0x8408
)

// Crypto1 implements the standard 48-bit MIFARE Classic LFSR stream cipher
// strictly for known-key authentication and encrypted block read/write on ST25R3916.
type Crypto1 struct {
	odd  uint32
	even uint32
}

// NewCrypto1 initializes a Crypto1 cipher state from a known 6-byte MIFARE key.
func NewCrypto1(key [6]byte) *Crypto1 {
	u := uint64(key[0])<<40 | uint64(key[1])<<32 | uint64(key[2])<<24 |
		uint64(key[3])<<16 | uint64(key[4])<<8 | uint64(key[5])
	s := &Crypto1{}
	for i := 47; i > 0; i -= 2 {
		s.odd = (s.odd << 1) | uint32((u>>uint((i-1)^7))&1)
		s.even = (s.even << 1) | uint32((u>>uint(i^7))&1)
	}
	return s
}

// FilterBit returns the current non-linear filter output bit without advancing the LFSR.
func (s *Crypto1) FilterBit() uint8 {
	return crypto1Filter(s.odd)
}

// Bit advances the LFSR by one bit and returns the generated keystream bit.
func (s *Crypto1) Bit(in uint8, isEncrypted bool) uint8 {
	feedin := uint32(in & 1)
	ret := crypto1Filter(s.odd)
	if isEncrypted {
		feedin ^= uint32(ret)
	}
	feedin ^= lfPolyOdd & s.odd
	feedin ^= lfPolyEven & s.even
	s.even = (s.even << 1) | uint32(bits.OnesCount32(feedin)&1)
	s.odd, s.even = s.even, s.odd
	return ret
}

// Byte advances the LFSR by 8 bits (LSB first) and returns the keystream byte.
func (s *Crypto1) Byte(in uint8, isEncrypted bool) uint8 {
	var ret uint8
	for i := range 8 {
		ret |= s.Bit((in>>i)&1, isEncrypted) << i
	}
	return ret
}

// Word advances the LFSR across 4 bytes in network order (MSB byte first, LSB bit first).
func (s *Crypto1) Word(in uint32, isEncrypted bool) uint32 {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], in)
	var out [4]byte
	for i := range 4 {
		out[i] = s.Byte(b[i], isEncrypted)
	}
	return binary.BigEndian.Uint32(out[:])
}

// EncryptWithParity encrypts plaintext bytes and computes their encrypted ISO 14443-A odd parity bits.
func (s *Crypto1) EncryptWithParity(plain []byte) (cipher []byte, par []uint8) {
	cipher = make([]byte, len(plain))
	par = make([]uint8, len(plain))
	for i, b := range plain {
		cipher[i] = b ^ s.Byte(0, false)
		par[i] = OddParity(b) ^ s.FilterBit()
	}
	return cipher, par
}

// EncryptReaderNonce feeds reader nonce bytes into the LFSR and returns encrypted bytes + parity bits.
func (s *Crypto1) EncryptReaderNonce(nr [4]byte) (cipher [4]byte, par [4]uint8) {
	for i, b := range nr {
		cipher[i] = b ^ s.Byte(b, false)
		par[i] = OddParity(b) ^ s.FilterBit()
	}
	return cipher, par
}

// DecryptWithParity decrypts ciphertext bytes and verifies each byte's encrypted odd parity bit.
func (s *Crypto1) DecryptWithParity(cipher []byte, par []uint8) (plain []byte, parityOK bool) {
	plain = make([]byte, len(cipher))
	parityOK = true
	for i, c := range cipher {
		p := c ^ s.Byte(0, false)
		plain[i] = p
		if i < len(par) && par[i] != (OddParity(p)^s.FilterBit()) {
			parityOK = false
		}
	}
	return plain, parityOK
}

// DecryptNibble4 decrypts a 4-bit MIFARE Classic ACK/NAK nibble using 4 keystream bits.
func (s *Crypto1) DecryptNibble4(raw uint8) uint8 {
	var ks uint8
	for i := range 4 {
		ks |= s.Bit(0, false) << i
	}
	return (raw ^ ks) & 0x0F
}

func crypto1Filter(x uint32) uint8 {
	f := 0xf22c0 >> (x & 0xf) & 16
	f |= 0x6c9c0 >> (x >> 4 & 0xf) & 8
	f |= 0x3c8b0 >> (x >> 8 & 0xf) & 4
	f |= 0x1e458 >> (x >> 12 & 0xf) & 2
	f |= 0x0d938 >> (x >> 16 & 0xf) & 1
	return uint8((uint32(0xEC57E80A) >> f) & 1)
}

// PRNGSuccessor advances the 16-bit MIFARE Classic tag nonce LFSR by n steps.
func PRNGSuccessor(x, n uint32) uint32 {
	x = bits.ReverseBytes32(x)
	for range n {
		x = (x >> 1) | (((x >> 16) ^ (x >> 18) ^ (x >> 19) ^ (x >> 21)) << 31)
	}
	return bits.ReverseBytes32(x)
}

// OddParity returns the ISO 14443-A odd parity bit (0 or 1) for byte b.
func OddParity(b uint8) uint8 {
	return uint8((bits.OnesCount8(b) + 1) & 1)
}

// CRC16A computes the standard ISO 14443-A 16-bit CRC (initial 0x6363, poly 0x8408).
func CRC16A(data []byte) [2]byte {
	crc := crc16AInit
	for _, b := range data {
		crc ^= uint16(b)
		for range 8 {
			if crc&1 != 0 {
				crc = (crc >> 1) ^ crc16APoly
			} else {
				crc >>= 1
			}
		}
	}
	return [2]byte{uint8(crc & 0xFF), uint8(crc >> 8)}
}

// AppendCRC16A returns a copy of data with its 2-byte ISO 14443-A CRC appended.
func AppendCRC16A(data []byte) []byte {
	crc := CRC16A(data)
	out := make([]byte, len(data)+2)
	copy(out, data)
	out[len(data)] = crc[0]
	out[len(data)+1] = crc[1]
	return out
}

// VerifyCRC16A verifies that frame (at least 3 bytes) ends with a valid ISO 14443-A CRC.
func VerifyCRC16A(frame []byte) bool {
	if len(frame) < 3 {
		return false
	}
	want := CRC16A(frame[:len(frame)-2])
	return frame[len(frame)-2] == want[0] && frame[len(frame)-1] == want[1]
}

// PackParityBits packs N 8-bit bytes + N parity bits into a 9*N-bit LSB-first ST25R3916 raw stream.
func PackParityBits(data []byte, par []uint8) (packed []byte, totalBits int) {
	n := len(data)
	totalBits = n * 9
	packed = make([]byte, (totalBits+7)/8)
	bitPos := 0
	for i := range n {
		b := data[i]
		for bit := range 8 {
			if (b>>bit)&1 != 0 {
				packed[bitPos/8] |= 1 << (bitPos % 8)
			}
			bitPos++
		}
		if i < len(par) && (par[i]&1) != 0 {
			packed[bitPos/8] |= 1 << (bitPos % 8)
		}
		bitPos++
	}
	return packed, totalBits
}

// UnpackParityBits extracts nBytes of (data, parity) from an LSB-first ST25R3916 raw bit stream.
func UnpackParityBits(raw []byte, nBytes int) (data []byte, par []uint8, ok bool) {
	if nBytes < 0 || len(raw)*8 < nBytes*9 {
		return nil, nil, false
	}
	data = make([]byte, nBytes)
	par = make([]uint8, nBytes)
	bitPos := 0
	for i := range nBytes {
		var b uint8
		for bit := range 8 {
			if (raw[bitPos/8]>>(bitPos%8))&1 != 0 {
				b |= 1 << bit
			}
			bitPos++
		}
		data[i] = b
		if (raw[bitPos/8]>>(bitPos%8))&1 != 0 {
			par[i] = 1
		}
		bitPos++
	}
	return data, par, true
}
