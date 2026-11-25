package nfcfile

import "fmt"

// TargetFamily is the hardware-agnostic family of a physical blank card detected before writing.
type TargetFamily string

const (
	TargetNTAG        TargetFamily = "ntag"
	TargetMagicGen1A  TargetFamily = "magic_gen1a"
	TargetMifare      TargetFamily = "mifare_classic"
	TargetNFCV        TargetFamily = "nfcv"
	TargetUnsupported TargetFamily = "unsupported"
)

// TargetDescriptor describes a probed physical blank card without importing hardware drivers.
type TargetDescriptor struct {
	Family       TargetFamily
	Label        string
	UIDLen       int
	MaxUserPages int // NTAG213=36, NTAG215=126, NTAG216=222
	MaxBlocks    int // MIFARE 1K=64, 4K=256; NFC-V=64
}

// CompatLevel grades compatibility between a source .nfc dump and a detected physical blank card.
type CompatLevel string

const (
	CompatExact        CompatLevel = "exact"
	CompatAdapted      CompatLevel = "adapted"
	CompatIncompatible CompatLevel = "incompatible"
)

// CompatResult carries the reconciled WritePlan and human-readable compatibility explanation.
type CompatResult struct {
	Level  CompatLevel `json:"compat_level"`
	Reason string      `json:"compat_note"`
	Plan   WritePlan   `json:"plan"`
}

// ReconcileWritePlan matches a source .nfc tag against a detected physical blank card,
// adapting compatible cross-capacity writes and rejecting incompatible silicon cleanly.
func ReconcileWritePlan(tag *NfcTag, target TargetDescriptor, opts PlanOptions) (CompatResult, error) {
	if tag == nil {
		return CompatResult{}, ErrNilTag
	}
	if target.Family == TargetUnsupported {
		return incompatibleResult(tag, fmt.Sprintf("Blank %s is read-only / not writable", target.Label)), nil
	}
	proto := effectiveProtocol(tag)
	switch {
	case proto == "NFC-V":
		return reconcileNFCV(tag, target)
	case proto == "NFC-B" || proto == "NFC-F":
		return incompatibleResult(tag, fmt.Sprintf("%s cards cannot be written onto blank tags", proto)), nil
	case isMifareClassicTag(tag):
		return reconcileMifare(tag, target, opts)
	case isNTAGType2Tag(tag):
		return reconcileNTAG(tag, target, opts)
	default:
		return incompatibleResult(tag, fmt.Sprintf("%s cannot be written onto %s", tag.DeviceType, target.Label)), nil
	}
}

func reconcileNTAG(tag *NfcTag, target TargetDescriptor, opts PlanOptions) (CompatResult, error) {
	if target.Family != TargetNTAG {
		return incompatibleResult(tag, fmt.Sprintf("Incompatible: NTAG (%dB) != %s", len(tag.UID), target.Label)), nil
	}
	baseOpts := opts
	baseOpts.MaxNTAGUserPages = 0
	plan, err := BuildWritePlan(tag, baseOpts)
	if err != nil {
		return CompatResult{}, err
	}
	if plan.Strategy != StrategyNTAGPages {
		return incompatibleResult(tag, plan.Reason), nil
	}
	capPages := target.MaxUserPages
	if capPages <= 0 || len(plan.NTAGPages) <= capPages {
		return CompatResult{
			Level:  CompatExact,
			Reason: fmt.Sprintf("Exact fit on %s (%d/%d pg)", target.Label, len(plan.NTAGPages), max(capPages, len(plan.NTAGPages))),
			Plan:   plan,
		}, nil
	}
	trimmed := TrimNTAGTrailingZeroPages(plan.NTAGPages)
	if len(trimmed) <= capPages {
		plan.NTAGPages = trimmed
		plan.Summary = fmt.Sprintf("NTAG adapted P%02d..P%02d (%d pg)", plan.NTAGStartPage, int(plan.NTAGStartPage)+len(trimmed)-1, len(trimmed))
		plan.Notice = fmt.Sprintf("Trimmed zero tail to fit %s", target.Label)
		return CompatResult{
			Level:  CompatAdapted,
			Reason: fmt.Sprintf("Adapted: active payload fits %s (%d/%d pg)", target.Label, len(trimmed), capPages),
			Plan:   plan,
		}, nil
	}
	return incompatibleResult(tag, fmt.Sprintf("Payload needs %d pg, %s holds %d pg", len(trimmed), target.Label, capPages)), nil
}

func reconcileMifare(tag *NfcTag, target TargetDescriptor, opts PlanOptions) (CompatResult, error) {
	if target.Family != TargetMagicGen1A && target.Family != TargetMifare {
		return incompatibleResult(tag, fmt.Sprintf("Incompatible: MIFARE (%dB) != %s", len(tag.UID), target.Label)), nil
	}
	srcIs4K := tag.SAK == sakMifare4K || tag.MifareType == "4K" || len(tag.Pages) > mifare1KMaxBlocks
	if srcIs4K && target.MaxBlocks > 0 && target.MaxBlocks < mifare4KMaxBlocks {
		return incompatibleResult(tag, fmt.Sprintf("4K dump (256 blk) does not fit on %s", target.Label)), nil
	}
	resolvedOpts := opts
	if target.Family == TargetMagicGen1A {
		resolvedOpts.MagicGen = "gen1a"
	} else {
		resolvedOpts.MagicGen = "gen2"
	}
	plan, err := BuildWritePlan(tag, resolvedOpts)
	if err != nil {
		return CompatResult{}, err
	}
	if plan.Strategy == StrategyUnsupported {
		return incompatibleResult(tag, plan.Reason), nil
	}
	if !srcIs4K && target.MaxBlocks >= mifare4KMaxBlocks {
		return CompatResult{
			Level:  CompatAdapted,
			Reason: fmt.Sprintf("1K dump on 4K %s (sets SAK 0x08)", target.Label),
			Plan:   plan,
		}, nil
	}
	if target.Family == TargetMagicGen1A {
		return CompatResult{Level: CompatExact, Reason: "Gen1A backdoor detected (UID + data)", Plan: plan}, nil
	}
	return CompatResult{Level: CompatExact, Reason: "MIFARE Crypto1 (Gen2 Blk0 or Std sectors)", Plan: plan}, nil
}

func reconcileNFCV(tag *NfcTag, target TargetDescriptor) (CompatResult, error) {
	if target.Family != TargetNFCV {
		return incompatibleResult(tag, fmt.Sprintf("Incompatible: NFC-V != %s", target.Label)), nil
	}
	plan, err := BuildWritePlan(tag, PlanOptions{})
	if err != nil {
		return CompatResult{}, err
	}
	if plan.Strategy != StrategyNFCVBlocks {
		return incompatibleResult(tag, plan.Reason), nil
	}
	return CompatResult{Level: CompatExact, Reason: "Exact fit on ISO 15693 blank", Plan: plan}, nil
}

func incompatibleResult(tag *NfcTag, reason string) CompatResult {
	return CompatResult{
		Level:  CompatIncompatible,
		Reason: reason,
		Plan: WritePlan{
			Strategy:   StrategyUnsupported,
			DeviceType: tag.DeviceType,
			Summary:    "Incompatible blank card",
			Reason:     reason,
			UID:        append([]byte(nil), tag.UID...),
			ATQA:       tag.ATQA,
			SAK:        tag.SAK,
		},
	}
}

// TrimNTAGTrailingZeroPages trims trailing all-zero pages from NTAG user memory,
// preserving all pages up to the NDEF Terminator TLV (0xFE) or the last non-zero byte.
func TrimNTAGTrailingZeroPages(pages [][4]byte) [][4]byte {
	if len(pages) == 0 {
		return nil
	}
	lastNonZero := -1
	for i := len(pages) - 1; i >= 0; i-- {
		if pages[i] != ([4]byte{}) {
			lastNonZero = i
			break
		}
	}
	if lastNonZero < 0 {
		return pages[:1]
	}
	return pages[:lastNonZero+1]
}
