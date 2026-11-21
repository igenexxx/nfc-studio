package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"nfc-studio/internal/config"
	"nfc-studio/internal/runner"
)

func main() {
	cfg := runner.Config{}
	flag.BoolVar(&cfg.SimMode, "sim", false, "Force NFC simulator mode without ST25R3916 hardware")
	flag.BoolVar(&cfg.ProbeOnly, "probe", false, "Probe ST25R3916 and poll once for an NFC tag on stdout, then exit")
	flag.BoolVar(&cfg.MCPMode, "mcp", false, "Run MCP JSON-RPC 2.0 server over stdio (requires opt-in in Settings)")
	flag.StringVar(&cfg.ConfigPath, "config", config.DefaultPath(), "Path to NFC Studio settings JSON file")
	flag.StringVar(&cfg.FBPath, "fb", "/dev/fb0", "Linux framebuffer device path")
	flag.StringVar(&cfg.SPIPath, "spi", "/dev/spidev0.2", "ST25R3916 spidev path")
	flag.StringVar(&cfg.TagsDir, "tags", runner.DefaultTagsDir(), "Directory for saved .nfc files")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := runner.Run(ctx, cfg, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "nfc-studio: %v\n", err)
		os.Exit(1)
	}
}
