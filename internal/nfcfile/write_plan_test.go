package nfcfile

import (
	"errors"
	"testing"
)

func makeNTAGTag(ccSize uint8, totalPages int) *NfcTag {
	pages := make([][]byte, totalPages)
	for i := range totalPages {
		pages[i] = []byte{byte(i), 0x11, 0x22, 0x33}
	}
	if totalPages > 3 {
		pages[3] = []byte{0xE1, 0x10, ccSize, 0x00}
	}
	return &NfcTag{
		FileType:   "Flipper NFC device",
		Version:    4,
		DeviceType: "NTAG215",
		UID:        []byte{0x04, 0xA1, 0xB2, 0xC3, 0xD4, 0xE5, 0xF6},
		ATQA:       [2]byte{0x44, 0x00},
		SAK:        0x00,
		PagesTotal: totalPages,
		Pages:      pages,
	}
}

func makeMifare1KTag(unreadableBlk int) *NfcTag {
	pages := make([][]byte, 64)
	unreadable := make([]bool, 64)
	for i := range 64 {
		b := make([]byte, 16)
		b[0] = byte(i)
		if IsMifareTrailerBlock(i) {
			copy(b[0:6], []byte{0xA0, 0xA1, 0xA2, 0xA3, 0xA4, 0xA5})
			copy(b[6:10], []byte{0xFF, 0x07, 0x80, 0x69})
			copy(b[10:16], []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF})
		}
		pages[i] = b
	}
	if unreadableBlk >= 0 && unreadableBlk < 64 {
		unreadable[unreadableBlk] = true
	}
	return &NfcTag{
		FileType:   "Flipper NFC device",
		Version:    4,
		DeviceType: "Mifare Classic",
		UID:        []byte{0xDE, 0xAD, 0xBE, 0xEF},
		ATQA:       [2]byte{0x04, 0x00},
		SAK:        0x08,
		PagesTotal: 64,
		Pages:      pages,
		MultiMeta: MultiMeta{
			Protocol:   "NFC-A",
			MifareType: "1K",
			Unreadable: unreadable,
		},
	}
}

func TestBuildWritePlanAdversarial(t *testing.T) {
	t.Run("nil tag returns ErrNilTag", func(t *testing.T) {
		if _, err := BuildWritePlan(nil, PlanOptions{}); !errors.Is(err, ErrNilTag) {
			t.Fatalf("got %v, want ErrNilTag", err)
		}
	})

	t.Run("NTAG213 excludes system pages P00..P03 and tail P40..P44", func(t *testing.T) {
		tag := makeNTAGTag(ntag213CCSize, 45)
		plan, err := BuildWritePlan(tag, PlanOptions{})
		if err != nil {
			t.Fatalf("BuildWritePlan error = %v", err)
		}
		if plan.Strategy != StrategyNTAGPages || plan.NTAGStartPage != 4 || len(plan.NTAGPages) != 36 {
			t.Fatalf("unexpected NTAG213 plan: strategy=%s start=%d pages=%d", plan.Strategy, plan.NTAGStartPage, len(plan.NTAGPages))
		}
		if plan.NTAGPages[0][0] != 4 || plan.NTAGPages[35][0] != 39 {
			t.Fatalf("wrong NTAG213 page boundaries: first=%d last=%d", plan.NTAGPages[0][0], plan.NTAGPages[35][0])
		}
	})

	t.Run("NTAG215 respects MaxNTAGUserPages cap", func(t *testing.T) {
		tag := makeNTAGTag(ntag215CCSize, 135)
		plan, err := BuildWritePlan(tag, PlanOptions{MaxNTAGUserPages: 10})
		if err != nil || len(plan.NTAGPages) != 10 {
			t.Fatalf("got len=%d err=%v, want 10", len(plan.NTAGPages), err)
		}
	})

	t.Run("MIFARE Classic 1K Gen1A skips trailers and unreadable blocks by default", func(t *testing.T) {
		tag := makeMifare1KTag(5)
		plan, err := BuildWritePlan(tag, PlanOptions{MagicGen: "gen1a"})
		if err != nil {
			t.Fatalf("BuildWritePlan error = %v", err)
		}
		// 64 total - 1 (Blk 0) - 16 (trailers) - 1 (blk 5 unreadable) = 46 blocks
		if plan.Strategy != StrategyMagicGen1A || len(plan.MifareBlocks) != 46 || plan.SkippedTrailers != 16 || plan.SkippedUnreadable != 1 {
			t.Fatalf("unexpected Gen1A plan: strat=%s blks=%d skipTr=%d skipUnr=%d", plan.Strategy, len(plan.MifareBlocks), plan.SkippedTrailers, plan.SkippedUnreadable)
		}
		if len(plan.SectorKeys) != 16 || plan.SectorKeys[0].KeyA[0] != 0xA0 {
			t.Fatalf("expected 16 extracted sector keys, got %d", len(plan.SectorKeys))
		}
	})

	t.Run("MIFARE Classic Gen2 with IncludeTrailers includes all readable blocks", func(t *testing.T) {
		tag := makeMifare1KTag(-1)
		plan, err := BuildWritePlan(tag, PlanOptions{MagicGen: "gen2", IncludeTrailers: true})
		if err != nil {
			t.Fatalf("BuildWritePlan error = %v", err)
		}
		if plan.Strategy != StrategyMagicGen2 || len(plan.MifareBlocks) != 63 || plan.SkippedTrailers != 0 {
			t.Fatalf("unexpected Gen2 trailer plan: strat=%s blks=%d skipTr=%d", plan.Strategy, len(plan.MifareBlocks), plan.SkippedTrailers)
		}
	})

	t.Run("MIFARE UIDOnly produces 0 sector blocks", func(t *testing.T) {
		tag := makeMifare1KTag(-1)
		plan, err := BuildWritePlan(tag, PlanOptions{MagicGen: "gen1a", UIDOnly: true})
		if err != nil || len(plan.MifareBlocks) != 0 {
			t.Fatalf("got blks=%d err=%v, want 0", len(plan.MifareBlocks), err)
		}
	})

	t.Run("MIFARE 7-byte UID rejected for Magic Gen1A/Gen2 but allowed for regular sectors", func(t *testing.T) {
		tag := makeMifare1KTag(-1)
		tag.UID = []byte{0x04, 1, 2, 3, 4, 5, 6}
		pMagic, _ := BuildWritePlan(tag, PlanOptions{MagicGen: "gen1a"})
		if pMagic.Strategy != StrategyUnsupported {
			t.Fatalf("expected StrategyUnsupported for 7B magic UID, got %s", pMagic.Strategy)
		}
		pReg, _ := BuildWritePlan(tag, PlanOptions{MagicGen: "regular"})
		if pReg.Strategy != StrategyMifareSectors || len(pReg.MifareBlocks) != 47 {
			t.Fatalf("expected StrategyMifareSectors with 47 blocks, got %s (%d)", pReg.Strategy, len(pReg.MifareBlocks))
		}
	})

	t.Run("ISO 15693 NFC-V plans all 4-byte blocks", func(t *testing.T) {
		tag := &NfcTag{
			DeviceType: "ISO15693",
			UID:        []byte{0xE0, 0x04, 0x01, 0x50, 0x12, 0x34, 0x56, 0x78},
			Pages:      [][]byte{{1, 2, 3, 4}, {5, 6, 7, 8}},
			MultiMeta:  MultiMeta{Protocol: "NFC-V"},
		}
		plan, err := BuildWritePlan(tag, PlanOptions{})
		if err != nil || plan.Strategy != StrategyNFCVBlocks || len(plan.NFCVBlocks) != 2 {
			t.Fatalf("unexpected NFC-V plan: %+v err=%v", plan, err)
		}
	})

	t.Run("Unsupported card families return StrategyUnsupported with reason", func(t *testing.T) {
		cases := []*NfcTag{
			{DeviceType: "FeliCa", UID: []byte{1, 2, 3, 4, 5, 6, 7, 8}, MultiMeta: MultiMeta{Protocol: "NFC-F"}},
			{DeviceType: "ISO14443-3B", UID: []byte{1, 2, 3, 4}, MultiMeta: MultiMeta{Protocol: "NFC-B"}},
			{DeviceType: "ISO14443-4 / DESFire", UID: []byte{1, 2, 3, 4}, SAK: 0x20},
			{DeviceType: "NTAG215", UID: []byte{4, 1, 2, 3, 4, 5, 6}, SAK: 0x00, Pages: [][]byte{{1, 2, 3, 4}}},
		}
		for _, tc := range cases {
			p, err := BuildWritePlan(tc, PlanOptions{})
			if err != nil || p.Strategy != StrategyUnsupported || p.Reason == "" {
				t.Fatalf("case %s: got (%+v, %v), want StrategyUnsupported with reason", tc.DeviceType, p, err)
			}
		}
	})
}

func TestMifare4KGeometryHelpers(t *testing.T) {
	if !IsMifareTrailerBlock(3) || !IsMifareTrailerBlock(127) || !IsMifareTrailerBlock(143) || !IsMifareTrailerBlock(255) {
		t.Fatal("expected blocks 3, 127, 143, 255 to be trailers")
	}
	if IsMifareTrailerBlock(0) || IsMifareTrailerBlock(128) || IsMifareTrailerBlock(131) || IsMifareTrailerBlock(256) {
		t.Fatal("non-trailer block misclassified")
	}
	if MifareBlockSector(128) != 32 || MifareBlockSector(255) != 39 {
		t.Fatalf("unexpected 4K sector mapping: 128->%d 255->%d", MifareBlockSector(128), MifareBlockSector(255))
	}
	if MifareSectorTrailerBlock(0) != 3 || MifareSectorTrailerBlock(32) != 143 || MifareSectorTrailerBlock(39) != 255 {
		t.Fatal("unexpected trailer block index")
	}
}

func FuzzBuildWritePlan(f *testing.F) {
	f.Add("NTAG215", uint8(0x00), []byte{0x04, 1, 2, 3, 4, 5, 6}, 12, "gen1a")
	f.Add("Mifare Classic", uint8(0x08), []byte{0xDE, 0xAD, 0xBE, 0xEF}, 64, "gen2")
	f.Fuzz(func(t *testing.T, dt string, sak uint8, uid []byte, nPages int, gen string) {
		if nPages < 0 {
			nPages = 0
		}
		if nPages > 300 {
			nPages = 300
		}
		pages := make([][]byte, nPages)
		sz := 4
		if sak == 0x08 || sak == 0x18 {
			sz = 16
		}
		for i := range nPages {
			pages[i] = make([]byte, sz)
		}
		tag := &NfcTag{DeviceType: dt, SAK: sak, UID: uid, Pages: pages}
		plan, err := BuildWritePlan(tag, PlanOptions{MagicGen: gen})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if plan.Strategy == "" {
			t.Fatal("empty strategy")
		}
	})
}
