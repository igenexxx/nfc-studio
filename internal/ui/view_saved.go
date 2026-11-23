package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"nfc-studio/internal/library"
	"nfc-studio/internal/nfcfile"
	"nfc-studio/internal/nfcml"
)

const savedVisibleRows = 6

func (a *App) handleSavedKey(act KeyAction) {
	switch act {
	case KeyUp:
		if a.SavedIdx > 0 {
			a.SavedIdx--
		}
	case KeyDown:
		if a.SavedIdx < len(a.SavedFiles)-1 {
			a.SavedIdx++
		}
	case KeySpace, KeyMark:
		if a.Settings.ExpertMode {
			a.toggleSavedMark()
		}
	case KeyNav4, KeyEnter:
		if len(a.SavedFiles) > 0 {
			a.CurrentTag = a.SavedFiles[a.SavedIdx].Tag
			a.updateNDEF()
			a.SwitchMode(ModeEmu)
			a.startEmulation()
		}
	case KeyNav5:
		if len(a.SavedFiles) > 0 {
			a.CurrentTag = a.SavedFiles[a.SavedIdx].Tag
			a.updateNDEF()
			a.DumpScroll = 0
			a.ShowNdefTab = !a.Settings.ExpertMode && a.CurrentNDEF != nil
			a.SwitchMode(ModeDump)
		}
	case KeyNav6:
		if !a.Settings.ExpertMode {
			a.enterReadWizard()
			return
		}
		a.handleSavedDelete()
	case KeyDelete:
		a.handleSavedDelete()
	case KeyNav7:
		if !a.Settings.ExpertMode {
			a.handleSavedDelete()
			return
		}
		a.refreshSavedFiles()
		a.SetToast("Refreshed")
	}
}

func (a *App) toggleSavedMark() {
	if len(a.SavedFiles) == 0 {
		return
	}
	if a.SavedSelected == nil {
		a.SavedSelected = make(map[string]bool)
	}
	name := a.SavedFiles[a.SavedIdx].Name
	if a.SavedSelected[name] {
		delete(a.SavedSelected, name)
	} else {
		a.SavedSelected[name] = true
	}
	a.savedDelUntil = time.Time{}
}

func (a *App) handleSavedDelete() {
	if len(a.SavedFiles) == 0 || a.Store == nil {
		return
	}
	targets := a.selectedSavedEntries()
	if len(targets) > 1 && time.Now().After(a.savedDelUntil) {
		a.savedDelUntil = time.Now().Add(3 * time.Second)
		a.SetToast(fmt.Sprintf("Del %d tags? DEL again", len(targets)))
		return
	}
	a.savedDelUntil = time.Time{}
	removed := 0
	for _, entry := range targets {
		ref := library.EntryRef{Name: entry.Name, Path: entry.Path, PackID: library.PackIDUser, Source: library.SourceUser}
		if a.Store.DeleteUserEntry(ref) == nil {
			removed++
			delete(a.SavedSelected, entry.Name)
		}
	}
	a.refreshSavedFiles()
	if len(targets) == 1 && removed == 1 {
		a.SetToast("Deleted " + targets[0].Name)
		return
	}
	a.SetToast(fmt.Sprintf("Deleted %d tag(s)", removed))
}

func (a *App) selectedSavedEntries() []SavedTag {
	if len(a.SavedSelected) == 0 {
		return []SavedTag{a.SavedFiles[a.SavedIdx]}
	}
	var out []SavedTag
	for _, e := range a.SavedFiles {
		if a.SavedSelected[e.Name] {
			out = append(out, e)
		}
	}
	if len(out) == 0 {
		return []SavedTag{a.SavedFiles[a.SavedIdx]}
	}
	return out
}

func (a *App) updateNDEF() {
	a.CurrentNDEF = nil
	a.HasMatch = false
	a.LastProfile = nfcml.DumpProfile{}
	if a.CurrentTag != nil {
		if rec, ok := nfcfile.ExtractNDEF(a.CurrentTag.Pages); ok {
			a.CurrentNDEF = &rec
		}
		a.LastProfile = nfcml.AnalyzeDump(a.CurrentTag.Pages)
		if a.FPIndex != nil {
			emb := nfcml.ComputeEmbedding(a.CurrentTag)
			if m, ok := a.FPIndex.Match(emb, len(a.CurrentTag.UID), 0.90); ok {
				a.LastMatch = m
				a.HasMatch = true
			}
		}
	}
}

func (a *App) saveCurrentTag() {
	if a.CurrentTag == nil {
		a.SetToast("No tag to save")
		return
	}
	if err := os.MkdirAll(a.TagsDir, 0o755); err != nil {
		a.SetToast("Save err!")
		return
	}
	name := fmt.Sprintf("tag_%s.nfc", time.Now().Format("150405"))
	if err := os.WriteFile(filepath.Join(a.TagsDir, name), []byte(a.CurrentTag.Format()), 0o644); err != nil {
		a.SetToast("Write err!")
		return
	}
	a.SetToast("Saved " + name)
}

func (a *App) refreshSavedFiles() {
	a.SavedFiles = nil
	if a.Store != nil {
		for _, ref := range a.Store.ListUserEntries() {
			a.SavedFiles = append(a.SavedFiles, SavedTag{Name: ref.Name, Path: ref.Path, Tag: ref.Tag})
		}
	}
	if a.SavedIdx >= len(a.SavedFiles) {
		a.SavedIdx = max(0, len(a.SavedFiles)-1)
	}
	a.rebuildFingerprintIndex()
}

func (a *App) rebuildFingerprintIndex() {
	entries := make([]nfcml.FingerprintEntry, 0, len(a.SavedFiles))
	for _, s := range a.SavedFiles {
		if s.Tag == nil {
			continue
		}
		entries = append(entries, nfcml.FingerprintEntry{
			Name:       s.Name,
			Path:       s.Path,
			DeviceType: s.Tag.DeviceType,
			UIDLen:     len(s.Tag.UID),
			Embedding:  nfcml.ComputeEmbedding(s.Tag),
		})
	}
	a.FPIndex = nfcml.NewFingerprintIndex(entries)
}

func (a *App) drawSaved(c *Canvas) {
	title := fmt.Sprintf("User .nfc Library (%d)", len(a.SavedFiles))
	if len(a.SavedSelected) > 0 {
		title = fmt.Sprintf("User .nfc Library (%d, %d sel)", len(a.SavedFiles), len(a.SavedSelected))
	}
	c.DrawText(6, 21, title, ColCyan, 1)
	c.FillRect(6, 32, 308, 108, ColPanel)
	c.DrawRect(6, 32, 308, 108, ColBorder)
	if len(a.SavedFiles) == 0 {
		c.DrawTextCentered(ScreenW/2, 76, "No .nfc cards saved yet", ColMuted, 1)
		c.DrawTextCentered(ScreenW/2, 90, "Press 6 NEW to scan a card", ColMuted, 1)
		return
	}
	start := max(0, a.SavedIdx-3)
	for row := range savedVisibleRows {
		idx := start + row
		if idx >= len(a.SavedFiles) {
			break
		}
		entry := a.SavedFiles[idx]
		y := 36 + row*17
		if idx == a.SavedIdx {
			c.FillRect(8, y-1, 304, 15, ColNavActive)
			c.DrawRect(8, y-1, 304, 15, ColEmerald)
		}
		var label string
		if !a.Settings.ExpertMode {
			cur := " "
			if idx == a.SavedIdx {
				cur = ">"
			}
			label = fmt.Sprintf("%s %-17s %s", cur, truncate(entry.Name, 17), truncate(entry.Tag.DeviceType, 14))
		} else {
			mark := " "
			if a.SavedSelected[entry.Name] {
				mark = "x"
			}
			label = fmt.Sprintf("[%s] %-15s %s", mark, truncate(entry.Name, 15), truncate(entry.Tag.DeviceType, 14))
		}
		c.DrawText(10, y+3, label, ColText, 1)
	}
}
