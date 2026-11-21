package mcpserver

import (
	"context"
	"sync"

	"nfc-studio/internal/nfcfile"
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

func (b *ChipBackend) StartEmulationF(idm, pmm []byte, sysCode [2]byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.chip.StartEmulationF(idm, pmm, sysCode)
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

func (b *ChipBackend) MeasureField() (uint8, uint8, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.chip.MeasureField()
}

func (b *ChipBackend) MeasureExternalField() (uint8, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.chip.MeasureExternalField()
}

func (b *ChipBackend) WriteType2Pages(ctx context.Context, ops []st25r3916.WritePageOp, opts st25r3916.WriteOptions) (st25r3916.WriteReport, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.chip.WriteType2Pages(ctx, ops, opts)
}

func (b *ChipBackend) CloneMagicUID(ctx context.Context, gen st25r3916.MagicCardGen, uid []byte, atqa [2]byte, sak uint8, keyA [6]byte) (st25r3916.CloneReport, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.chip.CloneMagicUID(ctx, gen, uid, atqa, sak, keyA)
}

func (b *ChipBackend) ReadMifareClassicDict(ctx context.Context, keys [][6]byte, onProgress func(st25r3916.DictProgress)) (*st25r3916.MifareDump, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.chip.ReadMifareClassicDict(ctx, keys, onProgress)
}

func (b *ChipBackend) InspectEMV(ctx context.Context) (*nfcfile.EMVInfo, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.chip.InspectEMV(ctx)
}

func (b *ChipBackend) PollMultiProtocol(ctx context.Context, protos []st25r3916.Protocol) (*st25r3916.MultiTagInfo, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.chip.PollMultiProtocol(ctx, protos)
}
