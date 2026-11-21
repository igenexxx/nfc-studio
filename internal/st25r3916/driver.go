package st25r3916

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

const (
	spiIOCWrMode     = 0x40016B01
	spiIOCWrBits     = 0x40016B03
	spiIOCWrMaxSpeed = 0x40046B04
	spiIOCMessage1   = 0x40206B00
)

type spiIOCTransfer struct {
	txBuf       uint64
	rxBuf       uint64
	len         uint32
	speedHz     uint32
	delayUsecs  uint16
	bitsPerWord uint8
	csChange    uint8
	txNBits     uint8
	rxNBits     uint8
	wordDelay   uint8
	pad         uint8
}

// LinuxTransport implements ST25R3916 SPI Mode 1 access over /dev/spidev0.2.
type LinuxTransport struct {
	spiFile *os.File
	speedHz uint32
}

// OpenLinuxTransport powers the Cap 5V rail, ensures the SPI CS2 overlay is loaded,
// and opens /dev/spidev0.2 in SPI Mode 1.
func OpenLinuxTransport(spiPath string, speedHz uint32) (*LinuxTransport, error) {
	if err := enableCapPower(); err != nil {
		return nil, err
	}
	if err := ensureSPIDevice(spiPath); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(spiPath, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", spiPath, err)
	}
	mode := uint8(1) // ST25R3916 requires SPI Mode 1 (CPOL=0, CPHA=1)
	bits := uint8(8)
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), spiIOCWrMode, uintptr(unsafe.Pointer(&mode))); errno != 0 {
		_ = f.Close()
		return nil, fmt.Errorf("spi set mode 1: %w", errno)
	}
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), spiIOCWrBits, uintptr(unsafe.Pointer(&bits))); errno != 0 {
		_ = f.Close()
		return nil, fmt.Errorf("spi set bits: %w", errno)
	}
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), spiIOCWrMaxSpeed, uintptr(unsafe.Pointer(&speedHz))); errno != 0 {
		_ = f.Close()
		return nil, fmt.Errorf("spi set speed: %w", errno)
	}
	return &LinuxTransport{spiFile: f, speedHz: speedHz}, nil
}

func (lt *LinuxTransport) Transfer(tx []byte) ([]byte, error) {
	if len(tx) == 0 {
		return nil, nil
	}
	rx := make([]byte, len(tx))
	tr := spiIOCTransfer{
		txBuf:       uint64(uintptr(unsafe.Pointer(&tx[0]))),
		rxBuf:       uint64(uintptr(unsafe.Pointer(&rx[0]))),
		len:         uint32(len(tx)),
		speedHz:     lt.speedHz,
		bitsPerWord: 8,
	}
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, lt.spiFile.Fd(), spiIOCMessage1, uintptr(unsafe.Pointer(&tr)))
	if errno != 0 {
		return nil, fmt.Errorf("spi transfer: %w", errno)
	}
	return rx, nil
}

func (lt *LinuxTransport) Close() error {
	if lt.spiFile != nil {
		return lt.spiFile.Close()
	}
	return nil
}

func ensureSPIDevice(spiPath string) error {
	if f, err := os.OpenFile(spiPath, os.O_RDWR, 0); err == nil {
		_ = f.Close()
		return nil
	}
	if spiPath != "/dev/spidev0.2" {
		return nil
	}
	// CardputerZero exposes ST25R3916 CS2 on GPIO22 via spi0-spidev2-gpio22-overlay.
	if _, err := os.Stat(spiPath); err != nil {
		if err := exec.Command("dtoverlay", "spi0-spidev2-gpio22-overlay").Run(); err != nil {
			_ = exec.Command("sudo", "-n", "dtoverlay", "spi0-spidev2-gpio22-overlay").Run()
		}
	}
	// Wait for udev to apply group=spi (0660) permissions on the newly created node.
	for range 40 {
		if f, err := os.OpenFile(spiPath, os.O_RDWR, 0); err == nil {
			_ = f.Close()
			return nil
		}
		time.Sleep(25 * time.Millisecond)
	}
	return nil
}

func enableCapPower() error {
	_ = exec.Command("pinctrl", "set", "26", "op", "dh").Run()
	const ledPath = "/sys/class/leds/ext_5v_out/brightness"
	data, err := os.ReadFile(ledPath)
	if err != nil {
		time.Sleep(25 * time.Millisecond)
		return nil
	}
	val, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	if val <= 0 {
		if err := os.WriteFile(ledPath, []byte("1"), 0o644); err != nil {
			return fmt.Errorf("enable ext_5v_out: %w", err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	return nil
}
