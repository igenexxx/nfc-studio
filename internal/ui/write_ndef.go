package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"nfc-studio/internal/nfcfile"
)

type ndefQuickPreset struct {
	Kind     string
	Label    string
	URL      string
	Text     string
	SSID     string
	Password string
	Auth     nfcfile.WiFiAuth
}

var ndefPresetsByKind = [3][]ndefQuickPreset{
	{
		{Kind: "uri", Label: "cardputer.cc", URL: "https://cardputer.cc"},
		{Kind: "uri", Label: "docs.m5stack.com", URL: "https://docs.m5stack.com"},
		{Kind: "uri", Label: "github.com/igenexxx", URL: "https://github.com/igenexxx"},
	},
	{
		{Kind: "text", Label: "CardputerZero NFC", Text: "CardputerZero NFC Studio"},
		{Kind: "text", Label: "Lab Bench Tag", Text: "Verified on ST25R3916"},
		{Kind: "text", Label: "Contact Card", Text: "Owner: CardputerZero Lab"},
	},
	{
		{Kind: "wifi", Label: "CardputerLab (WPA2)", SSID: "CardputerLab", Password: "ChangeMe123", Auth: nfcfile.WiFiAuthWPA2},
		{Kind: "wifi", Label: "GuestWiFi (OPEN)", SSID: "GuestWiFi", Auth: nfcfile.WiFiAuthOpen},
	},
}

func (a *App) currentNDEFPreset() ndefQuickPreset {
	kind := a.WriteNDEFKind % len(ndefPresetsByKind)
	list := ndefPresetsByKind[kind]
	return list[a.WriteNDEFPreset%len(list)]
}

func (a *App) buildNDEFQuickPlan() (nfcfile.WritePlan, error) {
	p := a.currentNDEFPreset()
	var tlv []byte
	var err error
	switch p.Kind {
	case "uri":
		tlv, err = nfcfile.EncodeNDEFURI(p.URL)
	case "text":
		tlv, err = nfcfile.EncodeNDEFText("en", p.Text)
	default:
		tlv, err = nfcfile.EncodeNDEFWiFi(nfcfile.WiFiConfig{SSID: p.SSID, Password: p.Password, Auth: p.Auth})
	}
	if err != nil {
		return nfcfile.WritePlan{Strategy: nfcfile.StrategyUnsupported, Reason: err.Error()}, nil
	}
	pages, err := nfcfile.PackNDEFPages(tlv, 126)
	if err != nil {
		return nfcfile.WritePlan{Strategy: nfcfile.StrategyUnsupported, Reason: err.Error()}, nil
	}
	return nfcfile.WritePlan{
		Strategy:      nfcfile.StrategyNTAGPages,
		DeviceType:    "NTAG21x NDEF",
		Summary:       fmt.Sprintf("NDEF %s -> P04..P%02d (%d pg)", p.Kind, 4+len(pages)-1, len(pages)),
		Notice:        "Writes NDEF TLV to NTAG213/215/216 user memory",
		NTAGStartPage: 4,
		NTAGPages:     pages,
	}, nil
}

func (a *App) saveQuickNDEFTag() {
	plan, err := a.buildNDEFQuickPlan()
	if err != nil || plan.Strategy != nfcfile.StrategyNTAGPages {
		a.SetToast("NDEF encode err")
		return
	}
	pages := [][]byte{
		{0x04, 0xA1, 0xB2, 0x77},
		{0xC3, 0xD4, 0xE5, 0xF6},
		{0x11, 0x48, 0x00, 0x00},
		{0xE1, 0x10, 0x3E, 0x00},
	}
	for _, pg := range plan.NTAGPages {
		pages = append(pages, append([]byte(nil), pg[:]...))
	}
	tag := &nfcfile.NfcTag{
		FileType:   "Flipper NFC device",
		Version:    4,
		DeviceType: "NTAG215",
		UID:        []byte{0x04, 0xA1, 0xB2, 0xC3, 0xD4, 0xE5, 0xF6},
		ATQA:       [2]byte{0x44, 0x00},
		SAK:        0x00,
		PagesTotal: len(pages),
		Pages:      pages,
	}
	if err := os.MkdirAll(a.TagsDir, 0o755); err != nil {
		a.SetToast("Save err!")
		return
	}
	name := fmt.Sprintf("ndef_%s.nfc", time.Now().Format("150405"))
	if err := os.WriteFile(filepath.Join(a.TagsDir, name), []byte(tag.Format()), 0o644); err != nil {
		a.SetToast("Write err!")
		return
	}
	a.CurrentTag = tag
	a.updateNDEF()
	a.refreshSavedFiles()
	a.SetToast("Saved " + name)
}
