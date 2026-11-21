package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"nfc-studio/internal/config"
	"nfc-studio/internal/dbsync"
	"nfc-studio/internal/library"
	"nfc-studio/internal/mcpserver"
	"nfc-studio/internal/st25r3916"
	"nfc-studio/internal/ui"
)

// Config holds runtime parameters for nfc-studio.
type Config struct {
	SimMode      bool
	ProbeOnly    bool
	MCPMode      bool
	SyncOnly     bool
	ConfigPath   string
	SourcesPath  string
	FBPath       string
	SPIPath      string
	TagsDir      string
	DatabasesDir string
	Stdin        io.Reader // Optional stdio input for MCP mode; defaults to os.Stdin.
}

// DefaultTagsDir returns the default directory for Flipper-compatible .nfc files.
func DefaultTagsDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "/tmp/nfc-studio/tags"
	}
	return filepath.Join(home, "nfc-studio", "tags")
}

// Run executes hardware probe, DB sync, MCP stdio server, or the interactive framebuffer UI loop.
func Run(ctx context.Context, cfg Config, stdout, stderr io.Writer) error {
	if cfg.ProbeOnly {
		return runProbe(cfg.SPIPath, stdout)
	}
	if cfg.SyncOnly {
		return runSync(ctx, cfg, stdout)
	}
	if cfg.MCPMode {
		return runMCP(ctx, cfg, stdout, stderr)
	}
	return runUI(ctx, cfg, stderr)
}

func runSync(ctx context.Context, cfg Config, out io.Writer) error {
	sources, err := dbsync.LoadSources(cfg.SourcesPath)
	if err != nil {
		return err
	}
	if len(sources) == 0 {
		fmt.Fprintln(out, "nfc-studio: no remote DB sources configured")
		return nil
	}
	dbDir := cfg.DatabasesDir
	if dbDir == "" {
		dbDir = library.DefaultDatabasesDir()
	}
	reports := dbsync.NewSyncer(dbDir).SyncAll(ctx, sources)
	for _, r := range reports {
		if r.Err != "" {
			fmt.Fprintf(out, "source %-14s ERROR: %s\n", r.SourceID, r.Err)
			continue
		}
		if r.NotModified {
			fmt.Fprintf(out, "source %-14s UP-TO-DATE (304, skipped=%d)\n", r.SourceID, r.Skipped)
			continue
		}
		fmt.Fprintf(out, "source %-14s added=%d updated=%d skipped=%d pruned=%d failed=%d\n",
			r.SourceID, r.Added, r.Updated, r.Skipped, r.Pruned, r.Failed)
	}
	return nil
}

func runProbe(spiPath string, out io.Writer) error {
	info, tag, err := st25r3916.Probe(spiPath, 5, 150*time.Millisecond)
	if info.Identity == 0 && err != nil {
		return fmt.Errorf("probe failed: %w", err)
	}
	fmt.Fprintf(out, "ST25R3916 OK: id=0x%02X rev=%d supply=%dmV\n", info.Identity, info.Revision, info.SupplyMV)
	if err != nil {
		fmt.Fprintf(out, "No ISO14443-A tag detected: %v\n", err)
		return nil
	}
	fmt.Fprintf(out, "TAG FOUND: UID=%X ATQA=%02X%02X SAK=%02X Pages=%d\n",
		tag.UID, tag.ATQA[0], tag.ATQA[1], tag.SAK, len(tag.Pages))
	return nil
}

func runMCP(ctx context.Context, cfg Config, stdout, stderr io.Writer) error {
	settings, err := config.Load(cfg.ConfigPath)
	if err != nil {
		return fmt.Errorf("load settings: %w", err)
	}
	if !settings.MCPEnabled {
		return errors.New("MCP server is disabled. Enable it in NFC Studio -> SET (Settings) first.")
	}

	var chip *st25r3916.Chip
	if !cfg.SimMode {
		c, err := st25r3916.OpenLinux(cfg.SPIPath)
		if err != nil {
			fmt.Fprintf(stderr, "nfc-studio: ST25R3916 init failed (running MCP in sim mode): %v\n", err)
		} else {
			chip = c
			defer chip.Close()
		}
	}

	tagsDir := cfg.TagsDir
	if tagsDir == "" {
		tagsDir = DefaultTagsDir()
	}
	stdin := cfg.Stdin
	if stdin == nil {
		stdin = os.Stdin
	}

	srv := mcpserver.New(mcpserver.NewChipBackend(chip), tagsDir, settings)
	dbDir := cfg.DatabasesDir
	if dbDir == "" {
		dbDir = library.DefaultDatabasesDir()
	}
	srv.SetDatabaseConfig(dbDir, cfg.SourcesPath, nil)
	return srv.ServeStdio(ctx, stdin, stdout)
}

const uiFrameInterval = 55 * time.Millisecond

func runUI(ctx context.Context, cfg Config, stderr io.Writer) error {
	canvas, err := ui.NewCanvas(cfg.FBPath)
	if err != nil {
		return err
	}
	defer canvas.Close()

	cfgPath, settings := loadUISettings(cfg.ConfigPath, stderr)
	chip, hwErr := openOptionalChip(cfg.SimMode, cfg.SPIPath, stderr)
	if chip != nil {
		defer chip.Close()
	}

	keys, stopInput := ui.StartEvdevReader()
	defer stopInput()

	var reader ui.NFCReader
	if chip != nil {
		reader = chip
	}
	app := ui.NewApp(reader, cfg.TagsDir)
	defer app.Close()
	if cfg.DatabasesDir != "" {
		var extraDirs []string
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			extraDirs = []string{filepath.Join(home, "zerosub", "nfc")}
		}
		app.SetLibraryStore(library.NewStore(cfg.TagsDir, cfg.DatabasesDir, extraDirs))
	}
	if cfg.SourcesPath != "" {
		app.SetSourcesPath(cfg.SourcesPath)
	}
	app.ApplySettings(settings)
	app.SetConfigPath(cfgPath)
	if chip != nil {
		app.StartPoller()
		defer app.StopPoller()
	}
	setStartupToast(app, chip, hwErr)

	return runUILoop(ctx, app, canvas, keys)
}

func loadUISettings(cfgPath string, stderr io.Writer) (string, config.Settings) {
	if cfgPath == "" {
		cfgPath = config.DefaultPath()
	}
	settings, err := config.Load(cfgPath)
	if err != nil {
		fmt.Fprintf(stderr, "nfc-studio: load settings failed (using defaults): %v\n", err)
		settings = config.Default()
	}
	return cfgPath, settings
}

func openOptionalChip(simMode bool, spiPath string, stderr io.Writer) (*st25r3916.Chip, error) {
	if simMode {
		return nil, nil
	}
	chip, err := st25r3916.OpenLinux(spiPath)
	if err != nil {
		fmt.Fprintf(stderr, "nfc-studio: ST25R3916 init failed: %v\n", err)
		return nil, err
	}
	return chip, nil
}

func setStartupToast(app *ui.App, chip *st25r3916.Chip, hwErr error) {
	switch {
	case chip != nil:
		info := chip.Info()
		app.SetToast(fmt.Sprintf("ST25R3916 rev%d (%dmV)", info.Revision, info.SupplyMV))
	case hwErr != nil:
		app.SetToast("SIM Mode (ST25R3916 err)")
	default:
		app.SetToast("SIM Mode")
	}
}

func runUILoop(ctx context.Context, app *ui.App, canvas *ui.Canvas, keys <-chan ui.KeyAction) error {
	ticker := time.NewTicker(uiFrameInterval)
	defer ticker.Stop()

	app.Render(canvas)
	_ = canvas.Flush()

	for {
		select {
		case <-ctx.Done():
			return nil
		case act := <-keys:
			if app.HandleKey(act) {
				return nil
			}
			app.Render(canvas)
			_ = canvas.Flush()
		case <-ticker.C:
			app.Step()
			app.Render(canvas)
			_ = canvas.Flush()
		}
	}
}
