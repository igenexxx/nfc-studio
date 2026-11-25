package st25r3916

import (
	"context"
	"errors"
	"fmt"
)

// BlankKind classifies a physical blank card or keyfob detected on the ST25R3916 antenna.
type BlankKind string

const (
	BlankNTAG213         BlankKind = "ntag213"
	BlankNTAG215         BlankKind = "ntag215"
	BlankNTAG216         BlankKind = "ntag216"
	BlankMagicGen1A      BlankKind = "magic_gen1a"
	BlankMifareClassic1K BlankKind = "mifare_1k"
	BlankMifareClassic4K BlankKind = "mifare_4k"
	BlankNFCV            BlankKind = "nfcv"
	BlankUnsupported     BlankKind = "unsupported"
)

// BlankTarget describes a physical card/fob discovered non-destructively before writing.
type BlankTarget struct {
	Kind          BlankKind `json:"kind"`
	Label         string    `json:"label"`
	Protocol      Protocol  `json:"protocol"`
	UID           []byte    `json:"-"`
	UIDHex        string    `json:"uid_hex"`
	ATQA          [2]byte   `json:"-"`
	SAK           uint8     `json:"sak"`
	MaxUserPages  int       `json:"max_user_pages,omitempty"`
	MaxBlocks     int       `json:"max_blocks,omitempty"`
	Gen1ABackdoor bool      `json:"gen1a_backdoor,omitempty"`
}

// ProbeBlankTarget non-destructively identifies the physical card/fob on the Cap NFC coil
// (NTAG213/215/216, Gen1A Magic 0x40/0x43, MIFARE Classic 1K/4K, ISO 15693 NFC-V, or RO types).
func (c *Chip) ProbeBlankTarget(ctx context.Context) (*BlankTarget, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	defer func() { _ = c.fieldOffLocked() }()

	if target, err := c.probeNFCAtargetLocked(); err == nil {
		return target, nil
	} else if !errors.Is(err, ErrNoTag) && !errors.Is(err, ErrProtocol) {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return c.probeNonNFCATargetLocked()
}

func (c *Chip) probeNFCAtargetLocked() (*BlankTarget, error) {
	if err := c.resetAndEnableField(); err != nil {
		return nil, err
	}
	atqa, err := c.sendWUPA()
	if err != nil {
		return nil, err
	}
	uid, sak, err := c.runAnticollisionCascade()
	if err != nil {
		return nil, err
	}
	base := &BlankTarget{
		Protocol: ProtoNFCA,
		UID:      uid,
		UIDHex:   formatUIDHex(uid),
		ATQA:     atqa,
		SAK:      sak,
	}
	switch sak {
	case sakType2:
		return c.classifyType2BlankLocked(base), nil
	case sakMifare1K, sakMifare4K:
		return c.classifyMifareBlankLocked(base), nil
	case 0x20:
		base.Kind = BlankUnsupported
		base.Label = "ISO14443-4 / DESFire / EMV (RO)"
		return base, nil
	default:
		base.Kind = BlankUnsupported
		base.Label = fmt.Sprintf("ISO14443-3A SAK=0x%02X (RO)", sak)
		return base, nil
	}
}

func (c *Chip) classifyType2BlankLocked(t *BlankTarget) *BlankTarget {
	model, err := c.detectNTAGModelLocked()
	if err != nil {
		t.Kind = BlankUnsupported
		t.Label = "Type 2 tag (non-NTAG21x / locked)"
		return t
	}
	userPages := int(model.UserEndPage) - int(model.UserStartPage) + 1
	t.MaxUserPages = userPages
	switch model.Name {
	case "NTAG213":
		t.Kind = BlankNTAG213
	case "NTAG216":
		t.Kind = BlankNTAG216
	default:
		t.Kind = BlankNTAG215
	}
	t.Label = fmt.Sprintf("%s (%d pg)", model.Name, userPages)
	return t
}

func (c *Chip) classifyMifareBlankLocked(t *BlankTarget) *BlankTarget {
	maxBlk := 64
	sizeLbl := "1K"
	if t.SAK == sakMifare4K {
		maxBlk = 256
		sizeLbl = "4K"
	}
	t.MaxBlocks = maxBlk
	// Non-destructive Gen1A backdoor probe (0x40 / 0x43 only; zero write commands!).
	if err := c.openGen1ABackdoorLocked(); err == nil {
		t.Kind = BlankMagicGen1A
		t.Gen1ABackdoor = true
		t.Label = fmt.Sprintf("Magic Gen1A (%s)", sizeLbl)
		return t
	}
	if maxBlk == 256 {
		t.Kind = BlankMifareClassic4K
	} else {
		t.Kind = BlankMifareClassic1K
	}
	t.Label = fmt.Sprintf("MIFARE %s (Gen2/Std)", sizeLbl)
	return t
}

func (c *Chip) probeNonNFCATargetLocked() (*BlankTarget, error) {
	if v, err := c.pollNFCVLocked(0); err == nil && v != nil {
		uid := append([]byte(nil), v.UID[:]...)
		return &BlankTarget{
			Kind:      BlankNFCV,
			Label:     "ISO 15693 / NFC-V",
			Protocol:  ProtoNFCV,
			UID:       uid,
			UIDHex:    formatUIDHex(uid),
			MaxBlocks: nfcvMaxBlocksCap,
		}, nil
	}
	if f, err := c.pollNFCFLocked(); err == nil && f != nil {
		uid := append([]byte(nil), f.IDm[:]...)
		return &BlankTarget{
			Kind:     BlankUnsupported,
			Label:    "FeliCa / NFC-F (use Emulate)",
			Protocol: ProtoNFCF,
			UID:      uid,
			UIDHex:   formatUIDHex(uid),
		}, nil
	}
	if b, err := c.pollNFCBLocked(); err == nil && b != nil {
		uid := append([]byte(nil), b.PUPI[:]...)
		return &BlankTarget{
			Kind:     BlankUnsupported,
			Label:    "ISO 14443-B / NFC-B (RO)",
			Protocol: ProtoNFCB,
			UID:      uid,
			UIDHex:   formatUIDHex(uid),
		}, nil
	}
	return nil, ErrNoTag
}
