package ui

import (
	"fmt"
	"path/filepath"
	"time"

	"nfc-studio/internal/config"
	"nfc-studio/internal/dbsync"
	"nfc-studio/internal/library"
)

func (a *App) refreshProbePacks() {
	if a.Store == nil || a.Probe == nil {
		return
	}
	prevPackID := ""
	if a.PackIdx >= 0 && a.PackIdx < len(a.Packs) {
		prevPackID = a.Packs[a.PackIdx].ID
	}
	a.Packs = a.Store.ListPacks()
	if len(a.Packs) == 0 {
		a.PackIdx = 0
		a.Probe.SetCatalog(library.PackInfo{}, nil)
		return
	}
	a.PackIdx = 0
	for i, p := range a.Packs {
		if p.ID == prevPackID {
			a.PackIdx = i
			break
		}
	}
	a.loadSelectedProbePack()
}

func (a *App) loadSelectedProbePack() {
	if a.PackIdx < 0 || a.PackIdx >= len(a.Packs) {
		return
	}
	pack := a.Packs[a.PackIdx]
	entries, _ := a.Store.ListEntries(pack.ID)
	a.Probe.SetCatalog(pack, entries)
	clear(a.ProbeSelected)
	a.probeDelUntil = time.Time{}
}

func (a *App) stopActiveProbe() {
	if a.Probe != nil && a.Probe.Status == library.ProbeRunning {
		a.Probe.Pause()
	}
	if a.EmuActive {
		a.stopEmulation()
	}
}

func (a *App) stepProbe() {
	if a.Probe == nil {
		return
	}
	ref, idx, due := a.Probe.PollDue(time.Now())
	if !due {
		return
	}
	a.emulateProbeEntry(ref, idx)
	a.Probe.OnEmulated(idx, time.Now())
	if a.Probe.Status == library.ProbeDone {
		a.SetToast("Auto-Emu finished")
	}
}

func (a *App) emulateProbeEntry(ref library.EntryRef, idx int) {
	tag, err := library.LoadTag(ref)
	if err != nil {
		a.SetToast("Bad .nfc: " + truncate(ref.Name, 14))
		return
	}
	if !isEmulatableTag(tag) {
		a.SetToast("EMU supports NFC-A & FeliCa only")
		return
	}
	a.CurrentTag = tag
	a.updateNDEF()
	if err := a.startChipEmulation(tag); err != nil {
		a.SetToast("EMU err")
		return
	}
	a.EmuActive = true
	a.syncPollFlags()
	a.Probe.LastSentIdx = idx
}

func (a *App) handleProbeKey(act KeyAction) {
	if a.Probe == nil {
		return
	}
	if a.Probe.Status == library.ProbeRunning {
		a.handleProbeRunningKey(act)
		return
	}
	a.handleProbeIdleKey(act)
}

func (a *App) handleProbeRunningKey(act KeyAction) {
	switch act {
	case KeyNav4, KeySpace, KeyEnter:
		a.stopActiveProbe()
		a.SetToast(fmt.Sprintf("Stopped at #%d (F=Up)", a.Probe.LastSentIdx+1))
	case KeyLeft, KeyNav5:
		a.adjustProbeInterval(-1)
	case KeyRight, KeyNav6:
		a.adjustProbeInterval(1)
	case KeyNav7:
		a.stopActiveProbe()
		a.cycleProbePack()
	}
}

func (a *App) handleProbeIdleKey(act KeyAction) {
	switch act {
	case KeyUp:
		a.Probe.MoveCursor(-1)
	case KeyDown:
		a.Probe.MoveCursor(1)
	case KeyLeft:
		a.adjustProbeInterval(-1)
	case KeyRight:
		a.adjustProbeInterval(1)
	case KeyNav4, KeySpace:
		if a.Probe.Start(time.Now()) {
			a.SetToast("Auto-Emu " + a.Probe.IntervalLabel())
		} else {
			a.SetToast("Pack is empty")
		}
	case KeyNav5, KeyEnter:
		a.tryCurrentProbeEntry()
	case KeyNav6:
		a.promoteCurrentProbeEntry()
	case KeyNav7:
		a.cycleProbePack()
	case KeyMark:
		a.toggleProbeMark()
	case KeyDelete:
		a.handleProbeDelete()
	}
}

func (a *App) tryCurrentProbeEntry() {
	ref, ok := a.Probe.CurrentEntry()
	if !ok {
		a.SetToast("No tag selected")
		return
	}
	a.emulateProbeEntry(ref, a.Probe.CursorIdx)
	a.Probe.OnEmulated(a.Probe.CursorIdx, time.Now())
	a.SetToast("EMU: " + truncate(ref.Name, 18))
}

func (a *App) promoteCurrentProbeEntry() {
	ref, ok := a.Probe.CurrentEntry()
	if !ok {
		a.SetToast("No tag selected")
		return
	}
	if !ref.ReadOnly && ref.Source == library.SourceUser {
		a.SetToast("Already in USER")
		return
	}
	promoted, err := a.Store.PromoteToUser(ref)
	if err != nil {
		a.SetToast("Save err!")
		return
	}
	a.refreshSavedFiles()
	a.SetToast("Saved: " + truncate(promoted.Name, 18))
}

func (a *App) cycleProbePack() {
	if len(a.Packs) == 0 {
		a.refreshProbePacks()
		return
	}
	a.PackIdx = (a.PackIdx + 1) % len(a.Packs)
	a.loadSelectedProbePack()
	pack := a.Packs[a.PackIdx]
	a.SetToast(fmt.Sprintf("Pack: %s (%d)", pack.Name, len(a.Probe.Entries)))
}

func (a *App) adjustProbeInterval(delta int) {
	dur := a.Probe.StepInterval(delta)
	a.Settings.ProbeIntervalMs = int(dur.Milliseconds())
	if a.ConfigPath != "" {
		_ = config.Save(a.ConfigPath, a.Settings)
	}
	a.SetToast("Dwell: " + a.Probe.IntervalLabel())
}

func (a *App) canDeleteInActiveProbePack() bool {
	if a.Probe == nil {
		return false
	}
	p := a.Probe.Pack
	return p.Source == library.SourceUser || (p.Source == library.SourceExternal && p.ID != library.PackIDZeroSub)
}

func (a *App) toggleProbeMark() {
	if !a.canDeleteInActiveProbePack() {
		a.SetToast("Read-only pack")
		return
	}
	ref, ok := a.Probe.CurrentEntry()
	if !ok {
		return
	}
	if a.ProbeSelected == nil {
		a.ProbeSelected = make(map[string]bool)
	}
	if a.ProbeSelected[ref.Name] {
		delete(a.ProbeSelected, ref.Name)
	} else {
		a.ProbeSelected[ref.Name] = true
	}
	a.probeDelUntil = time.Time{}
}

func (a *App) handleProbeDelete() {
	if !a.canDeleteInActiveProbePack() || a.Store == nil {
		a.SetToast("Read-only pack")
		return
	}
	targets := a.selectedProbeEntries()
	if len(targets) == 0 {
		return
	}
	if time.Now().After(a.probeDelUntil) {
		a.probeDelUntil = time.Now().Add(3 * time.Second)
		a.SetToast(fmt.Sprintf("Del %d tag(s)? DEL again", len(targets)))
		return
	}
	a.probeDelUntil = time.Time{}
	removed, ignored, packDir := 0, []string(nil), ""
	for _, ref := range targets {
		var err error
		if ref.Source == library.SourceUser {
			err = a.Store.DeleteUserEntry(ref)
		} else {
			err = a.Store.DeleteDatabaseEntry(ref)
			if err == nil {
				ignored = append(ignored, ref.Name)
				packDir = filepath.Dir(ref.Path)
			}
		}
		if err == nil {
			removed++
		}
	}
	if len(ignored) > 0 && packDir != "" {
		_ = dbsync.MarkIgnored(packDir, ignored)
	}
	clear(a.ProbeSelected)
	a.refreshSavedFiles()
	a.refreshProbePacks()
	a.SetToast(fmt.Sprintf("Deleted %d tag(s)", removed))
}

func (a *App) selectedProbeEntries() []library.EntryRef {
	if len(a.ProbeSelected) == 0 {
		if ref, ok := a.Probe.CurrentEntry(); ok {
			return []library.EntryRef{ref}
		}
		return nil
	}
	var out []library.EntryRef
	for _, ref := range a.Probe.Entries {
		if a.ProbeSelected[ref.Name] {
			out = append(out, ref)
		}
	}
	return out
}

func (a *App) probeNavLabels() [5]string {
	if a.Probe != nil && a.Probe.Status == library.ProbeRunning {
		return [5]string{"STOP", "INT-", "INT+", "PACK", "MODE"}
	}
	startLbl := "AUTO"
	if a.Probe != nil && a.Probe.Status == library.ProbePaused {
		startLbl = "CONT"
	}
	return [5]string{startLbl, "TRY", "SAVE", "PACK", "MODE"}
}

func (a *App) drawProbe(c *Canvas) {
	a.drawProbeHeaderBar(c)
	c.FillRect(6, 34, 308, 106, ColPanel)
	c.DrawRect(6, 34, 308, 106, ColBorder)

	if a.Probe == nil || len(a.Probe.Entries) == 0 {
		c.DrawTextCentered(ScreenW/2, 74, "No .nfc files in this pack", ColMuted, 1)
		c.DrawTextCentered(ScreenW/2, 88, "Add packs in ~/nfc-studio/databases/", ColMuted, 1)
		c.DrawTextCentered(ScreenW/2, 102, "Press 7(PACK) to switch source", ColAmber, 1)
		return
	}
	a.drawProbeRows(c)
}

func (a *App) drawProbeHeaderBar(c *Canvas) {
	packLabel := "USER"
	packCol := ColEmerald
	if a.Probe != nil && a.Probe.Pack.ReadOnly {
		packLabel = "DB:" + truncate(a.Probe.Pack.Name, 10)
		packCol = ColCyan
	}
	total, cur := 0, 0
	if a.Probe != nil {
		total = len(a.Probe.Entries)
		if total > 0 {
			cur = a.Probe.CursorIdx + 1
		}
	}
	c.DrawText(6, 22, fmt.Sprintf("%s [%d/%d]", packLabel, cur, total), packCol, 1)
	if a.Probe != nil {
		c.DrawText(176, 22, "INT:"+a.Probe.IntervalLabel(), ColAmber, 1)
		stLabel, stCol := probeStatusStyle(a.Probe.Status)
		c.FillRect(266, 20, 48, 11, ColPanelAlt)
		c.DrawRect(266, 20, 48, 11, stCol)
		c.DrawTextCentered(290, 22, stLabel, stCol, 1)
	}
}

func probeStatusStyle(st library.ProbeStatus) (string, uint16) {
	switch st {
	case library.ProbeRunning:
		return "RUN", ColEmerald
	case library.ProbePaused:
		return "PAUSE", ColAmber
	case library.ProbeDone:
		return "DONE", ColCyan
	default:
		return "IDLE", ColMuted
	}
}

func (a *App) drawProbeRows(c *Canvas) {
	entries := a.Probe.Entries
	start := max(0, a.Probe.CursorIdx-3)
	visible := min(6, len(entries)-start)
	for row := range visible {
		idx := start + row
		entry := entries[idx]
		y := 38 + row*17
		if idx == a.Probe.CursorIdx {
			c.FillRect(8, y-1, 304, 15, ColNavActive)
			c.DrawRect(8, y-1, 304, 15, ColEmerald)
		}
		glyph, gCol := a.probeRowGlyph(idx)
		if a.ProbeSelected[entry.Name] {
			glyph, gCol = "x", ColAmber
		}
		c.DrawText(11, y+3, glyph, gCol, 1)
		tag := "RW"
		if entry.ReadOnly {
			tag = "RO"
		}
		label := fmt.Sprintf("#%02d %-36s %s", idx+1, truncate(entry.Name, 36), tag)
		c.DrawText(20, y+3, label, ColText, 1)
	}
}

func (a *App) probeRowGlyph(idx int) (string, uint16) {
	if idx == a.Probe.LastSentIdx {
		return ">", ColEmerald
	}
	if a.Probe.IsRecentSent(idx) {
		return "*", ColAmber
	}
	return " ", ColMuted
}
