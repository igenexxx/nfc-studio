package ui

import (
	"fmt"
	"strings"
	"time"

	"nfc-studio/internal/library"
)

const (
	helpPage1MaxKeys = 6
	helpPageNMaxKeys = 8
	helpSummaryMaxCh = 48
	helpDescMaxCh    = 41
)

// HelpKeyHint describes one available key or softkey on the active screen.
type HelpKeyHint struct {
	Key     string // Short keycap label (<= 7 chars), e.g. "4/Ent", "Z/C", "W"
	Desc    string // Concise English explanation (<= 41 chars)
	Softkey bool   // True for 4..8 bottom-bar softkeys (highlighted in cyan)
}

// ScreenHelp holds context-aware documentation for the active screen and sub-state.
type ScreenHelp struct {
	ScreenTitle string
	Summary     [2]string
	Keys        []HelpKeyHint
}

// PageCount returns how many modal pages are needed to display s cleanly on 320x170.
func (s ScreenHelp) PageCount() int {
	if len(s.Keys) <= helpPage1MaxKeys {
		return 1
	}
	rem := len(s.Keys) - helpPage1MaxKeys
	return 1 + (rem+helpPageNMaxKeys-1)/helpPageNMaxKeys
}

// ActiveScreenHelp returns exact context-aware help for the currently visible screen.
func (a *App) ActiveScreenHelp() ScreenHelp {
	if a.ShowSources {
		return helpForSources()
	}
	switch a.Mode {
	case ModeHome:
		return helpForHome(a.Settings.ExpertMode)
	case ModeRead:
		return helpForRead(a.ReadCaptured, a.Settings.ExpertMode)
	case ModeSaved:
		return helpForSaved(a.Settings.ExpertMode)
	case ModeEmu:
		return helpForEmu(a.Settings.ExpertMode)
	case ModeWrite:
		armed := !a.WriteBusy && time.Now().Before(a.WriteArmedUntil)
		return helpForWrite(a.WriteBusy, armed)
	case ModeScan:
		return helpForScan()
	case ModeDump:
		return helpForDump(a.ShowNdefTab, a.Settings.ExpertMode)
	case ModeProbe:
		running := a.Probe != nil && a.Probe.Status == library.ProbeRunning
		return helpForProbe(running)
	case ModeSettings:
		return helpForSettings(a.Settings.ExpertMode)
	default:
		return helpForHome(a.Settings.ExpertMode)
	}
}

func (a *App) handleHelpKey(act KeyAction) {
	pages := a.ActiveScreenHelp().PageCount()
	switch act {
	case KeyLeft, KeyUp:
		if pages > 1 {
			a.HelpPage = (a.HelpPage + pages - 1) % pages
		}
	case KeyRight, KeyDown, KeySpace, KeyTab, KeyNav5:
		if pages > 1 {
			a.HelpPage = (a.HelpPage + 1) % pages
		}
	case KeyQuit, KeyBack, KeyEnter, KeyNav4, KeyNav8:
		a.ShowHelp = false
	}
}

func (a *App) drawHelpModal(c *Canvas) {
	h := a.ActiveScreenHelp()
	totalPages := h.PageCount()
	page := a.HelpPage % totalPages

	// Clean full-screen backdrop eliminates half-clipped underlying chrome on 320x170.
	c.Clear(ColBg)

	const (
		mx, my, mw, mh = 4, 4, 312, 162
	)
	c.FillRect(mx, my, mw, mh, ColPanel)
	c.DrawRect(mx, my, mw, mh, ColCyan)
	c.FillRect(mx+1, my+1, mw-2, 14, ColNavActive)

	title := "HELP: " + truncate(h.ScreenTitle, 32)
	c.DrawText(mx+6, my+4, title, ColText, 1)
	pageLbl := fmt.Sprintf("%d/%d", page+1, totalPages)
	c.DrawText(mx+mw-len(pageLbl)*6-6, my+4, pageLbl, ColAmber, 1)

	if page == 0 {
		drawHelpPageOne(c, h, mx, my)
	} else {
		drawHelpOverflowPage(c, h, page, mx, my)
	}

	c.HLine(mx+2, my+mh-17, mw-4, ColBorder)
	footer := "[H / Enter / Esc] Close Help"
	if totalPages > 1 {
		footer = "[Z/C] Page   [H / Enter / Esc] Close"
	}
	c.DrawTextCentered(ScreenW/2, my+mh-12, footer, ColAmber, 1)
}

func drawHelpPageOne(c *Canvas, h ScreenHelp, mx, my int) {
	// Screen summary card with left cyan accent.
	c.FillRect(mx+5, my+18, 302, 26, ColPanelAlt)
	c.FillRect(mx+5, my+18, 2, 26, ColCyan)
	c.DrawText(mx+10, my+21, truncate(h.Summary[0], helpSummaryMaxCh), ColText, 1)
	if strings.TrimSpace(h.Summary[1]) != "" {
		c.DrawText(mx+10, my+32, truncate(h.Summary[1], helpSummaryMaxCh), ColMuted, 1)
	}

	c.DrawText(mx+6, my+48, "AVAILABLE CONTROLS", ColCyan, 1)
	c.HLine(mx+118, my+51, 188, ColBorder)

	end := min(len(h.Keys), helpPage1MaxKeys)
	for i := range end {
		drawHelpKeyRow(c, mx+5, my+59+i*14, h.Keys[i])
	}
}

func drawHelpOverflowPage(c *Canvas, h ScreenHelp, page, mx, my int) {
	c.DrawText(mx+6, my+19, "MORE CONTROLS", ColCyan, 1)
	c.HLine(mx+88, my+22, 218, ColBorder)

	start := helpPage1MaxKeys + (page-1)*helpPageNMaxKeys
	end := min(len(h.Keys), start+helpPageNMaxKeys)
	for i := start; i < end; i++ {
		drawHelpKeyRow(c, mx+5, my+31+(i-start)*14, h.Keys[i])
	}
}

func drawHelpKeyRow(c *Canvas, x, y int, k HelpKeyHint) {
	const badgeW = 46
	borderCol, keyCol := ColAmber, ColAmber
	if k.Softkey {
		borderCol, keyCol = ColCyan, ColCyan
	}
	c.FillRect(x, y, badgeW, 11, ColBg)
	c.DrawRect(x, y, badgeW, 11, borderCol)
	c.DrawTextCentered(x+badgeW/2, y+2, truncate(k.Key, 7), keyCol, 1)
	c.DrawText(x+badgeW+5, y+2, truncate(k.Desc, helpDescMaxCh), ColText, 1)
}
