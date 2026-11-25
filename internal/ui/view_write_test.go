package ui

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"nfc-studio/internal/nfcfile"
	"nfc-studio/internal/st25r3916"
)

type mockWriteChip struct {
	mockReader
	mu           sync.Mutex
	blankTarget  *st25r3916.BlankTarget
	ntagCalls    int
	gen1aCalls   int
	cryptoCalls  int
	autoCalls    int
	nfcvCalls    int
	failErr      error
	blk0Fallback bool
}

func (m *mockWriteChip) ProbeBlankTarget(_ context.Context) (*st25r3916.BlankTarget, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.blankTarget == nil {
		return nil, st25r3916.ErrNoTag
	}
	return m.blankTarget, nil
}

func (m *mockWriteChip) WriteType2Pages(_ context.Context, ops []st25r3916.WritePageOp, opts st25r3916.WriteOptions) (st25r3916.WriteReport, error) {
	m.mu.Lock()
	m.ntagCalls++
	err := m.failErr
	m.mu.Unlock()
	if err != nil {
		return st25r3916.WriteReport{FailedPage: int(ops[0].Page)}, err
	}
	if opts.OnProgress != nil {
		for i := range ops {
			opts.OnProgress(i+1, len(ops))
		}
	}
	return st25r3916.WriteReport{Model: "NTAG215", PagesWritten: len(ops), Verified: true, FailedPage: -1}, nil
}

func (m *mockWriteChip) CloneMagicUID(_ context.Context, gen st25r3916.MagicCardGen, uid []byte, _ [2]byte, _ uint8, _ [6]byte) (st25r3916.CloneReport, error) {
	return st25r3916.CloneReport{Gen: gen, TargetUID: nfcfile.FormatHex(uid), ReadBackID: nfcfile.FormatHex(uid), Verified: true}, nil
}

func (m *mockWriteChip) WriteMagicGen1ADump(_ context.Context, uid []byte, _ [2]byte, _ uint8, blocks []st25r3916.MifareBlockWrite, _ bool, onProg func(int, int)) (st25r3916.MagicDumpReport, error) {
	m.mu.Lock()
	m.gen1aCalls++
	err := m.failErr
	m.mu.Unlock()
	if err != nil {
		return st25r3916.MagicDumpReport{Gen: st25r3916.MagicGen1A, FailedBlock: 0}, err
	}
	tot := 1 + len(blocks)
	if onProg != nil {
		onProg(tot, tot)
	}
	return st25r3916.MagicDumpReport{Gen: st25r3916.MagicGen1A, ReadBackUID: nfcfile.FormatHex(uid), BlocksWritten: tot, Verified: true, FailedBlock: -1}, nil
}

func (m *mockWriteChip) WriteMifareCrypto1Dump(_ context.Context, gen st25r3916.MagicCardGen, uid []byte, _ [2]byte, _ uint8, writeBlk0 bool, blocks []st25r3916.MifareBlockWrite, _ []st25r3916.MifareSectorAuth, _ bool, onProg func(int, int)) (st25r3916.MagicDumpReport, error) {
	m.mu.Lock()
	m.cryptoCalls++
	err := m.failErr
	m.mu.Unlock()
	if err != nil {
		return st25r3916.MagicDumpReport{Gen: gen, FailedBlock: 1}, err
	}
	tot := len(blocks)
	if writeBlk0 {
		tot++
	}
	if onProg != nil {
		onProg(tot, tot)
	}
	return st25r3916.MagicDumpReport{Gen: gen, ReadBackUID: nfcfile.FormatHex(uid), BlocksWritten: tot, Verified: true, FailedBlock: -1}, nil
}

func (m *mockWriteChip) WriteMifareCrypto1Auto(_ context.Context, uid []byte, _ [2]byte, _ uint8, blocks []st25r3916.MifareBlockWrite, _ []st25r3916.MifareSectorAuth, _, _ bool, onProg func(int, int)) (st25r3916.MagicDumpReport, error) {
	m.mu.Lock()
	m.autoCalls++
	err := m.failErr
	fb := m.blk0Fallback
	m.mu.Unlock()
	if err != nil {
		return st25r3916.MagicDumpReport{FailedBlock: 0}, err
	}
	if onProg != nil {
		onProg(len(blocks), len(blocks))
	}
	return st25r3916.MagicDumpReport{
		Gen: st25r3916.MagicRegular, ReadBackUID: nfcfile.FormatHex(uid),
		BlocksWritten: len(blocks), Verified: true, Block0LockedFallback: fb, FailedBlock: -1,
	}, nil
}

func (m *mockWriteChip) WriteNFCVBlocksWithProgress(_ context.Context, _ uint8, blocks [][4]byte, _ bool, onProg func(int, int)) (int, error) {
	m.mu.Lock()
	m.nfcvCalls++
	err := m.failErr
	m.mu.Unlock()
	if err != nil {
		return 0, err
	}
	if onProg != nil {
		onProg(len(blocks), len(blocks))
	}
	return len(blocks), nil
}

func waitWriteDone(t *testing.T, app *App) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		app.Step()
		if !app.WriteBusy && app.WriteResultSet {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for write completion (busy=%v set=%v)", app.WriteBusy, app.WriteResultSet)
}

func TestWriteViewTwoStepArmingAndSimWrite(t *testing.T) {
	app := NewApp(nil, t.TempDir())
	defer app.Close()
	c, _ := NewCanvas("")

	app.HandleKey(KeyWrite)
	if app.Mode != ModeWrite || app.WriteSub != WriteSubDump || !app.isWriteAutoMode() {
		t.Fatalf("expected ModeWrite/Dump/AUTO, got mode=%v sub=%v auto=%v", app.Mode, app.WriteSub, app.isWriteAutoMode())
	}
	// Space triggers blank preview in SIM mode.
	app.HandleKey(KeySpace)
	time.Sleep(20 * time.Millisecond)
	app.Step()
	if app.WriteDetectedBlank == "" {
		t.Fatal("expected Space blank probe preview to populate WriteDetectedBlank")
	}
	app.Render(c)

	app.HandleKey(KeyNav4)
	if app.WriteBusy || !time.Now().Before(app.WriteArmedUntil) {
		t.Fatalf("expected armed state, got busy=%v", app.WriteBusy)
	}
	app.HandleKey(KeyNav4)
	waitWriteDone(t, app)
	if !app.WriteResultOK {
		t.Fatalf("expected SIM write OK, got err=%q", app.WriteResultMsg)
	}

	app.HandleKey(KeyNav7)
	app.HandleKey(KeyNav5)
	app.HandleKey(KeyDown)
	app.HandleKey(KeyNav6)
	if len(app.SavedFiles) == 0 {
		t.Fatal("expected saved NDEF .nfc file")
	}
	app.HandleKey(KeyBack)
	if app.Mode != ModeHome {
		t.Fatalf("expected ModeHome after KeyBack, got %v", app.Mode)
	}
}

func TestWriteViewAutoDetectCompatAndFallback(t *testing.T) {
	mc := &mockWriteChip{
		blankTarget: &st25r3916.BlankTarget{
			Kind: st25r3916.BlankMifareClassic1K, Label: "MIFARE 1K (Gen2/Std)",
			Protocol: st25r3916.ProtoNFCA, UID: []byte{0xA3, 0x7F, 0x7D, 0x0A}, SAK: 0x08, MaxBlocks: 64,
		},
		blk0Fallback: true,
	}
	app := NewApp(mc, t.TempDir())
	defer app.Close()
	c, _ := NewCanvas("")

	// 1. NTAG215 dump onto MIFARE 1K blank -> AUTO rejects cleanly BEFORE touching WriteType2Pages!
	app.CurrentTag = defaultSimTag()
	app.openWriteView(ModeSaved)
	app.HandleKey(KeyNav4)
	app.HandleKey(KeyEnter)
	waitWriteDone(t, app)
	if app.WriteResultOK || mc.ntagCalls != 0 || !strings.Contains(app.WriteResultMsg, "Incompatible") {
		t.Fatalf("expected AUTO incompatibility block, ok=%v ntagCalls=%d msg=%q", app.WriteResultOK, mc.ntagCalls, app.WriteResultMsg)
	}
	app.Render(c)

	// 2. MIFARE 1K dump onto regular MIFARE 1K blank -> AUTO falls back to data sectors cleanly!
	mfPages := make([][]byte, 8)
	for i := range mfPages {
		mfPages[i] = make([]byte, 16)
	}
	app.CurrentTag = &nfcfile.NfcTag{
		DeviceType: "Mifare Classic", UID: []byte{0xDE, 0xAD, 0xBE, 0xEF},
		ATQA: [2]byte{0x04, 0x00}, SAK: 0x08, Pages: mfPages,
		MultiMeta: nfcfile.MultiMeta{Protocol: "NFC-A", MifareType: "1K"},
	}
	app.HandleKey(KeyNav4)
	app.HandleKey(KeyNav4)
	waitWriteDone(t, app)
	if !app.WriteResultOK || mc.autoCalls != 1 || !strings.Contains(app.WriteResultMsg, "UID locked") {
		t.Fatalf("expected AUTO regular fallback OK, ok=%v autoCalls=%d msg=%q", app.WriteResultOK, mc.autoCalls, app.WriteResultMsg)
	}

	// 3. MIFARE 1K dump onto Gen1A Magic blank -> AUTO dispatches WriteMagicGen1ADump!
	mc.mu.Lock()
	mc.blankTarget = &st25r3916.BlankTarget{
		Kind: st25r3916.BlankMagicGen1A, Label: "Magic Gen1A (1K)",
		Protocol: st25r3916.ProtoNFCA, UID: []byte{1, 2, 3, 4}, SAK: 0x08, MaxBlocks: 64, Gen1ABackdoor: true,
	}
	mc.mu.Unlock()
	app.HandleKey(KeyNav4)
	app.HandleKey(KeyNav4)
	waitWriteDone(t, app)
	if !app.WriteResultOK || mc.gen1aCalls != 1 {
		t.Fatalf("expected AUTO Gen1A dispatch, ok=%v gen1aCalls=%d msg=%q", app.WriteResultOK, mc.gen1aCalls, app.WriteResultMsg)
	}

	// 4. NTAG215 dump onto NTAG215 blank -> AUTO dispatches WriteType2Pages!
	mc.mu.Lock()
	mc.blankTarget = &st25r3916.BlankTarget{
		Kind: st25r3916.BlankNTAG215, Label: "NTAG215 (126 pg)",
		Protocol: st25r3916.ProtoNFCA, UID: []byte{4, 1, 2, 3, 4, 5, 6}, SAK: 0x00, MaxUserPages: 126,
	}
	mc.mu.Unlock()
	app.CurrentTag = defaultSimTag()
	app.HandleKey(KeyNav4)
	app.HandleKey(KeyNav4)
	waitWriteDone(t, app)
	if !app.WriteResultOK || mc.ntagCalls != 1 {
		t.Fatalf("expected AUTO NTAG dispatch, ok=%v ntagCalls=%d msg=%q", app.WriteResultOK, mc.ntagCalls, app.WriteResultMsg)
	}
}

func TestDumpViewerMifareColoringAndViews(t *testing.T) {
	pages := make([][]byte, 8)
	for i := range pages {
		pages[i] = make([]byte, 16)
	}
	copy(pages[1], "CardputerZeroOK!")
	tag := &nfcfile.NfcTag{
		DeviceType: "Mifare Classic", UID: []byte{0xF3, 0xCC, 0x8C, 0x97},
		ATQA: [2]byte{0x04, 0x00}, SAK: 0x08, Pages: pages,
		MultiMeta: nfcfile.MultiMeta{Protocol: "NFC-A", MifareType: "1K", Unreadable: []bool{false, false, false, false, true, false, false, false}},
	}
	if dumpRowColor(tag, 0, true) != ColCyan {
		t.Fatal("expected B00 UID to be Cyan")
	}
	if dumpRowColor(tag, 1, true) != ColEmerald || dumpRowColor(tag, 2, true) != ColEmerald {
		t.Fatal("expected B01/B02 data blocks to be Emerald")
	}
	if dumpRowColor(tag, 3, true) != ColAmber || dumpRowColor(tag, 7, true) != ColAmber {
		t.Fatal("expected B03 and B07 sector trailers to be Amber")
	}
	if dumpRowColor(tag, 4, true) != ColMuted {
		t.Fatal("expected unreadable B04 to be Muted")
	}
	if got := formatDumpRow(tag, 1, 2, true); !strings.Contains(got, "CardputerZeroOK!") || !strings.Contains(got, "DATA") {
		t.Fatalf("unexpected ASCII dump row: %q", got)
	}
	if got := formatDumpRow(tag, 4, 0, true); !strings.Contains(got, "?? ??") {
		t.Fatalf("expected ?? for unreadable block, got %q", got)
	}
}
