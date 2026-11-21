package ui

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"nfc-studio/internal/nfcfile"
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
	case KeyNav4, KeyEnter:
		if len(a.SavedFiles) > 0 {
			a.CurrentTag = a.SavedFiles[a.SavedIdx].Tag
			a.updateNDEF()
			a.Mode = ModeEmu
			a.startEmulation()
		}
	case KeyNav5:
		if len(a.SavedFiles) > 0 {
			a.CurrentTag = a.SavedFiles[a.SavedIdx].Tag
			a.updateNDEF()
			a.DumpScroll = 0
			a.Mode = ModeDump
		}
	case KeyNav6, KeyDelete:
		if len(a.SavedFiles) > 0 {
			entry := a.SavedFiles[a.SavedIdx]
			_ = os.Remove(entry.Path)
			a.refreshSavedFiles()
			a.SetToast("Deleted " + entry.Name)
		}
	case KeyNav7:
		a.refreshSavedFiles()
		a.SetToast("Refreshed")
	}
}

func (a *App) updateNDEF() {
	a.CurrentNDEF = nil
	if a.CurrentTag != nil {
		if rec, ok := nfcfile.ExtractNDEF(a.CurrentTag.Pages); ok {
			a.CurrentNDEF = &rec
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
	dirs := []string{a.TagsDir}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, "zerosub", "nfc"))
	}
	for _, dir := range dirs {
		a.SavedFiles = append(a.SavedFiles, loadSavedTagsFromDir(dir)...)
	}
	slices.SortFunc(a.SavedFiles, func(x, y SavedTag) int {
		return cmp.Compare(y.Name, x.Name)
	})
	if a.SavedIdx >= len(a.SavedFiles) {
		a.SavedIdx = max(0, len(a.SavedFiles)-1)
	}
}

func loadSavedTagsFromDir(dir string) []SavedTag {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []SavedTag
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".nfc") {
			continue
		}
		full := filepath.Join(dir, e.Name())
		raw, err := os.ReadFile(full)
		if err != nil {
			continue
		}
		if tag, err := nfcfile.Parse(string(raw)); err == nil {
			out = append(out, SavedTag{Name: e.Name(), Path: full, Tag: tag})
		}
	}
	return out
}

func (a *App) drawSaved(c *Canvas) {
	c.DrawText(6, 21, fmt.Sprintf("Saved .nfc Library (%d)", len(a.SavedFiles)), ColCyan, 1)
	c.FillRect(6, 32, 308, 108, ColPanel)
	c.DrawRect(6, 32, 308, 108, ColBorder)
	if len(a.SavedFiles) == 0 {
		c.DrawTextCentered(ScreenW/2, 76, "No .nfc files found", ColMuted, 1)
		c.DrawTextCentered(ScreenW/2, 90, "Save a tag from SCAN or DUMP", ColMuted, 1)
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
		label := fmt.Sprintf("%-16s %s", truncate(entry.Name, 16), truncate(entry.Tag.DeviceType, 16))
		c.DrawText(12, y+3, label, ColText, 1)
	}
}
