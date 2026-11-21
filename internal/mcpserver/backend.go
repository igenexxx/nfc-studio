package mcpserver

import (
	"sync"

	"nfc-studio/internal/st25r3916"
)

// NFCBackend abstracts ST25R3916 hardware access for testing without physical silicon.
type NFCBackend interface {
	Info() st25r3916.ChipInfo
	PollISO14443A() (*st25r3916.TagInfo, error)
	StartEmulation(uid []byte, atqa [2]byte, sak uint8) error
	PollTargetHit() (bool, error)
	FieldOff() error
}

// ChipBackend serializes access to a real *st25r3916.Chip with a mutex.
type ChipBackend struct {
	mu   sync.Mutex
	chip *st25r3916.Chip
}

// NewChipBackend wraps a real ST25R3916 chip. Returns nil when chip is nil (sim mode).
func NewChipBackend(chip *st25r3916.Chip) NFCBackend {
	if chip == nil {
		return nil
	}
	return &ChipBackend{chip: chip}
}

func (b *ChipBackend) Info() st25r3916.ChipInfo {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.chip.Info()
}

func (b *ChipBackend) PollISO14443A() (*st25r3916.TagInfo, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.chip.PollISO14443A()
}

func (b *ChipBackend) StartEmulation(uid []byte, atqa [2]byte, sak uint8) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.chip.StartEmulation(uid, atqa, sak)
}

func (b *ChipBackend) PollTargetHit() (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.chip.PollTargetHit()
}

func (b *ChipBackend) FieldOff() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.chip.FieldOff()
}
