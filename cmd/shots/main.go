package main

import (
	"flag"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"

	"nfc-studio/internal/library"
	"nfc-studio/internal/nfcfile"
	"nfc-studio/internal/ui"
)

func main() {
	outDir := flag.String("out", "assets/screenshots", "Output directory")
	scale := flag.Int("scale", 1, "Scale factor (1=320x170, 2=640x340)")
	iconPath := flag.String("icon", "assets/images/nfc-studio-icon.png", "256x256 icon path")
	flag.Parse()

	_ = os.MkdirAll(*outDir, 0o755)
	tmpRoot, _ := os.MkdirTemp("", "nfcshots")
	defer os.RemoveAll(tmpRoot)
	tmpTags := filepath.Join(tmpRoot, "tags")
	tmpDBs := filepath.Join(tmpRoot, "databases")
	_ = os.MkdirAll(tmpTags, 0o755)
	_ = os.MkdirAll(filepath.Join(tmpDBs, "amiibo"), 0o755)

	seedSampleTags(tmpTags)
	seedSampleTags(filepath.Join(tmpDBs, "amiibo"))
	app := ui.NewApp(nil, tmpTags)
	defer app.Close()
	app.SetLibraryStore(library.NewStore(tmpTags, tmpDBs, nil))
	c, _ := ui.NewCanvas("")
	captureAppScreens(app, c, *outDir, *scale)

	if *iconPath != "" {
		_ = os.MkdirAll(filepath.Dir(*iconPath), 0o755)
		generateIcon(*iconPath)
	}
}

func seedSampleTags(dir string) {
	tag1 := &nfcfile.NfcTag{
		FileType: "Flipper NFC device", Version: 4, DeviceType: "NTAG215",
		UID: []byte{0x04, 0xA1, 0xB2, 0xC3, 0xD4, 0xE5, 0xF6}, ATQA: [2]byte{0x44, 0x00}, SAK: 0x00,
	}
	tag2 := &nfcfile.NfcTag{
		FileType: "Flipper NFC device", Version: 4, DeviceType: "MIFARE Classic 1K",
		UID: []byte{0xDE, 0xAD, 0xBE, 0xEF}, ATQA: [2]byte{0x04, 0x00}, SAK: 0x08,
	}
	_ = os.WriteFile(filepath.Join(dir, "office_badge.nfc"), []byte(tag2.Format()), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "cardputer_url.nfc"), []byte(tag1.Format()), 0o644)
}

func captureAppScreens(app *ui.App, c *ui.Canvas, outDir string, scale int) {
	// 1. HOME (Simple Mode)
	app.Settings.ExpertMode = false
	app.SwitchMode(ui.ModeHome)
	app.ToastText = ""
	app.Render(c)
	savePNG(c, filepath.Join(outDir, "home.png"), scale)

	// 2. READ & CLONE (Step 2 captured NTAG215)
	app.SwitchMode(ui.ModeRead)
	app.ReadCaptured = true
	app.ToastText = ""
	app.Render(c)
	savePNG(c, filepath.Join(outDir, "read.png"), scale)

	// 3. MY CARDS (Simple Mode)
	app.SwitchMode(ui.ModeSaved)
	app.ToastText = ""
	app.Render(c)
	savePNG(c, filepath.Join(outDir, "saved.png"), scale)

	// 4. EMULATE CARD (Simple Mode)
	app.SwitchMode(ui.ModeEmu)
	app.HandleKey(ui.KeyNav4)
	for range 25 {
		app.Step()
	}
	app.ToastText = ""
	app.Render(c)
	savePNG(c, filepath.Join(outDir, "emu.png"), scale)

	// 5. WRITE / CLONE TO CARD (Simple Mode, NTAG215 ready)
	app.SwitchMode(ui.ModeHome)
	app.HandleKey(ui.KeyWrite)
	app.ToastText = ""
	app.Render(c)
	savePNG(c, filepath.Join(outDir, "write.png"), scale)

	// 6. SETTINGS (Simple Mode)
	app.SwitchMode(ui.ModeSettings)
	app.SettingsIdx = 0
	app.ToastText = ""
	app.Render(c)
	savePNG(c, filepath.Join(outDir, "settings.png"), scale)

	// Expert Mode screens
	app.Settings.ExpertMode = true
	app.SwitchMode(ui.ModeScan)
	for range 12 {
		app.Step()
	}
	app.ToastText = ""
	app.Render(c)
	savePNG(c, filepath.Join(outDir, "scan.png"), scale)

	app.SwitchMode(ui.ModeDump)
	app.ToastText = ""
	app.Render(c)
	savePNG(c, filepath.Join(outDir, "dump.png"), scale)

	app.SwitchMode(ui.ModeProbe)
	app.Probe.LastSentIdx = 1
	app.Probe.Status = library.ProbePaused
	app.Probe.CursorIdx = 0
	app.ToastText = ""
	app.Render(c)
	savePNG(c, filepath.Join(outDir, "probe.png"), scale)

	app.Settings.ExpertMode = false
	app.SwitchMode(ui.ModeHome)
	app.ToastText = ""
	app.HandleKey(ui.KeyHelp)
	app.Render(c)
	savePNG(c, filepath.Join(outDir, "help.png"), scale)
}

func savePNG(c *ui.Canvas, path string, scale int) {
	img := image.NewRGBA(image.Rect(0, 0, ui.ScreenW*scale, ui.ScreenH*scale))
	for y := range ui.ScreenH {
		for x := range ui.ScreenW {
			rgb := rgb565ToRGBA(c.PixelAt(x, y))
			for dy := range scale {
				for dx := range scale {
					img.SetRGBA(x*scale+dx, y*scale+dy, rgb)
				}
			}
		}
	}
	f, _ := os.Create(path)
	defer f.Close()
	_ = png.Encode(f, img)
}

func generateIcon(path string) {
	const sz = 256
	img := image.NewRGBA(image.Rect(0, 0, sz, sz))
	for y := range sz {
		for x := range sz {
			img.SetRGBA(x, y, color.RGBA{R: 7, G: 13, B: 18, A: 255})
		}
	}
	cx, cy := 128.0, 128.0
	for y := range sz {
		for x := range sz {
			d := math.Hypot(float64(x)-cx, float64(y)-cy)
			if math.Abs(d-92) < 5 || math.Abs(d-66) < 5 {
				img.SetRGBA(x, y, color.RGBA{R: 16, G: 225, B: 145, A: 255})
			} else if math.Abs(d-40) < 5 {
				img.SetRGBA(x, y, color.RGBA{R: 0, G: 220, B: 255, A: 255})
			}
		}
	}
	for y := 110; y < 146; y++ {
		for x := 110; x < 146; x++ {
			img.SetRGBA(x, y, color.RGBA{R: 0, G: 220, B: 255, A: 255})
		}
	}
	f, _ := os.Create(path)
	defer f.Close()
	_ = png.Encode(f, img)
}

func rgb565ToRGBA(px uint16) color.RGBA {
	r := uint8((px >> 11) & 0x1F)
	g := uint8((px >> 5) & 0x3F)
	b := uint8(px & 0x1F)
	return color.RGBA{R: (r << 3) | (r >> 2), G: (g << 2) | (g >> 4), B: (b << 3) | (b >> 2), A: 255}
}
