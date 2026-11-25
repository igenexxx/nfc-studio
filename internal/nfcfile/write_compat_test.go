package nfcfile

import "testing"

func TestReconcileWritePlanCrossCardMatrix(t *testing.T) {
	t.Run("NTAG215 with trailing zeroes adapts cleanly to NTAG213", func(t *testing.T) {
		tag := makeNTAGTag(ntag215CCSize, 130)
		for i := 4; i < len(tag.Pages); i++ {
			tag.Pages[i] = []byte{0, 0, 0, 0}
		}
		tag.Pages[4] = []byte{0x03, 0x08, 0xD1, 0x01}
		tag.Pages[5] = []byte{0x04, 0x55, 0x04, 'a'}
		tag.Pages[6] = []byte{'.', 'c', 'c', 0xFE}

		res, err := ReconcileWritePlan(tag, TargetDescriptor{
			Family: TargetNTAG, Label: "NTAG213 (36 pg)", UIDLen: 7, MaxUserPages: 36,
		}, PlanOptions{})
		if err != nil || res.Level != CompatAdapted || len(res.Plan.NTAGPages) != 3 {
			t.Fatalf("NTAG215->213 adapt = (%+v, %v)", res, err)
		}
	})

	t.Run("NTAG215 with 50 active pages rejected on NTAG213", func(t *testing.T) {
		tag := makeNTAGTag(ntag215CCSize, 60)
		res, err := ReconcileWritePlan(tag, TargetDescriptor{
			Family: TargetNTAG, Label: "NTAG213 (36 pg)", UIDLen: 7, MaxUserPages: 36,
		}, PlanOptions{})
		if err != nil || res.Level != CompatIncompatible || res.Plan.Strategy != StrategyUnsupported {
			t.Fatalf("oversize NTAG215->213 = (%+v, %v)", res, err)
		}
	})

	t.Run("NTAG215 rejected on MIFARE Classic 1K blank (live bug reproduction)", func(t *testing.T) {
		tag := makeNTAGTag(ntag215CCSize, 20)
		res, err := ReconcileWritePlan(tag, TargetDescriptor{
			Family: TargetMifare, Label: "MIFARE 1K (Gen2/Std)", UIDLen: 4, MaxBlocks: 64,
		}, PlanOptions{})
		if err != nil || res.Level != CompatIncompatible {
			t.Fatalf("NTAG->MIFARE = (%+v, %v)", res, err)
		}
	})

	t.Run("MIFARE 1K onto Gen1A vs non-Gen1A MIFARE vs 4K vs NTAG", func(t *testing.T) {
		mf1k := makeMifare1KTag(-1)

		rGen1A, _ := ReconcileWritePlan(mf1k, TargetDescriptor{Family: TargetMagicGen1A, Label: "Magic Gen1A (1K)", UIDLen: 4, MaxBlocks: 64}, PlanOptions{})
		if rGen1A.Level != CompatExact || rGen1A.Plan.Strategy != StrategyMagicGen1A {
			t.Fatalf("MF1K->Gen1A = %+v", rGen1A)
		}

		rGen2, _ := ReconcileWritePlan(mf1k, TargetDescriptor{Family: TargetMifare, Label: "MIFARE 1K (Gen2/Std)", UIDLen: 4, MaxBlocks: 64}, PlanOptions{})
		if rGen2.Level != CompatExact || rGen2.Plan.Strategy != StrategyMagicGen2 {
			t.Fatalf("MF1K->MIFARE = %+v", rGen2)
		}

		r4K, _ := ReconcileWritePlan(mf1k, TargetDescriptor{Family: TargetMagicGen1A, Label: "Magic Gen1A (4K)", UIDLen: 4, MaxBlocks: 256}, PlanOptions{})
		if r4K.Level != CompatAdapted {
			t.Fatalf("MF1K->Gen1A 4K = %+v", r4K)
		}

		rNTAG, _ := ReconcileWritePlan(mf1k, TargetDescriptor{Family: TargetNTAG, Label: "NTAG215 (126 pg)", UIDLen: 7, MaxUserPages: 126}, PlanOptions{})
		if rNTAG.Level != CompatIncompatible {
			t.Fatalf("MF1K->NTAG = %+v", rNTAG)
		}
	})

	t.Run("MIFARE 4K rejected on 1K blank and RO targets rejected", func(t *testing.T) {
		mf4k := makeMifare1KTag(-1)
		mf4k.SAK = 0x18
		mf4k.MifareType = "4K"
		res, _ := ReconcileWritePlan(mf4k, TargetDescriptor{Family: TargetMagicGen1A, Label: "Magic Gen1A (1K)", UIDLen: 4, MaxBlocks: 64}, PlanOptions{})
		if res.Level != CompatIncompatible {
			t.Fatalf("MF4K->1K = %+v", res)
		}

		resRO, _ := ReconcileWritePlan(mf4k, TargetDescriptor{Family: TargetUnsupported, Label: "DESFire (RO)"}, PlanOptions{})
		if resRO.Level != CompatIncompatible {
			t.Fatalf("RO target = %+v", resRO)
		}
	})
}
