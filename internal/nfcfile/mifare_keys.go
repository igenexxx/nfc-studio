package nfcfile

import (
	"bufio"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const maxDictLines = 10000

// MifareKey is a 6-byte MIFARE Classic sector key (Key A or Key B).
type MifareKey [6]byte

var builtinMifareKeys = []MifareKey{
	{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF},
	{0xA0, 0xA1, 0xA2, 0xA3, 0xA4, 0xA5},
	{0xD3, 0xF7, 0xD3, 0xF7, 0xD3, 0xF7},
	{0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
	{0xB0, 0xB1, 0xB2, 0xB3, 0xB4, 0xB5},
	{0x4D, 0x3A, 0x99, 0xC3, 0x51, 0xDD},
	{0x1A, 0x98, 0x2C, 0x7E, 0x45, 0x9A},
	{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF},
	{0x71, 0x4C, 0x5C, 0x88, 0x6E, 0x97},
	{0x58, 0x7E, 0xE5, 0xF9, 0x35, 0x0F},
	{0xA0, 0x47, 0x8C, 0xC3, 0x90, 0x91},
	{0x53, 0x3C, 0xB6, 0xC7, 0x23, 0xF6},
	{0x8F, 0xD0, 0xA4, 0xF2, 0x56, 0xE9},
}

// DefaultMifareKeys returns a copy of the 13 built-in factory default MIFARE Classic keys.
func DefaultMifareKeys() []MifareKey {
	out := make([]MifareKey, len(builtinMifareKeys))
	copy(out, builtinMifareKeys)
	return out
}

// DefaultKeyDictPath returns $HOME/.config/nfc-studio/mf_keys.dict.
func DefaultKeyDictPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".config", "nfc-studio", "mf_keys.dict")
}

// LoadMifareKeyDict returns built-in default keys followed by deduplicated keys from path.
// A missing user dictionary file is silently skipped without error.
func LoadMifareKeyDict(path string) ([]MifareKey, error) {
	keys := DefaultMifareKeys()
	seen := make(map[MifareKey]bool, len(keys))
	for _, k := range keys {
		seen[k] = true
	}
	if strings.TrimSpace(path) == "" {
		return keys, nil
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return keys, nil
		}
		return keys, err
	}
	defer func() { _ = f.Close() }()

	userKeys, err := ParseMifareKeyDict(f)
	if err != nil {
		return keys, err
	}
	for _, k := range userKeys {
		if !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	return keys, nil
}

// ParseMifareKeyDict parses 6-byte hex keys from r, skipping blank lines, '#' comments, and malformed entries.
func ParseMifareKeyDict(r io.Reader) ([]MifareKey, error) {
	if r == nil {
		return nil, nil
	}
	scanner := bufio.NewScanner(r)
	seen := make(map[MifareKey]bool)
	var out []MifareKey

	for lines := 0; lines < maxDictLines && scanner.Scan(); lines++ {
		key, ok := parseKeyLine(scanner.Text())
		if !ok || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, key)
	}
	return out, scanner.Err()
}

func parseKeyLine(raw string) (MifareKey, bool) {
	line := strings.TrimSpace(raw)
	if line == "" || strings.HasPrefix(line, "#") {
		return MifareKey{}, false
	}
	if idx := strings.IndexByte(line, '#'); idx >= 0 {
		line = strings.TrimSpace(line[:idx])
	}
	cleaned := strings.NewReplacer(" ", "", ":", "", "-", "", "\t", "").Replace(line)
	if len(cleaned) != 12 {
		return MifareKey{}, false
	}
	b, err := hex.DecodeString(cleaned)
	if err != nil || len(b) != 6 {
		return MifareKey{}, false
	}
	var k MifareKey
	copy(k[:], b)
	return k, true
}
