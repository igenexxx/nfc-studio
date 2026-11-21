package st25r3916

import (
	"fmt"
	"time"
)

// DefaultSPISpeedHz is the standard SPI clock rate for ST25R3916 on CardputerZero.
const DefaultSPISpeedHz uint32 = 5_000_000

// OpenLinux powers the Cap 5V rail, ensures /dev/spidev0.2 exists, and initializes ST25R3916.
func OpenLinux(spiPath string) (*Chip, error) {
	tr, err := OpenLinuxTransport(spiPath, DefaultSPISpeedHz)
	if err != nil {
		return nil, fmt.Errorf("transport: %w", err)
	}
	chip, err := Open(tr)
	if err != nil {
		_ = tr.Close()
		return nil, fmt.Errorf("chip init: %w", err)
	}
	return chip, nil
}

// Probe opens the ST25R3916 over Linux SPI and polls for an ISO 14443-A tag up to attempts times.
func Probe(spiPath string, attempts int, delay time.Duration) (ChipInfo, *TagInfo, error) {
	chip, err := OpenLinux(spiPath)
	if err != nil {
		return ChipInfo{}, nil, err
	}
	defer chip.Close()

	info := chip.Info()
	if attempts <= 0 {
		attempts = 1
	}

	var lastErr error
	for i := range attempts {
		tag, err := chip.PollISO14443A()
		if err == nil {
			return info, tag, nil
		}
		lastErr = err
		if i+1 < attempts && delay > 0 {
			time.Sleep(delay)
		}
	}
	return info, nil, lastErr
}
