package ui

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
)

type KeyAction int

const (
	KeyNone KeyAction = iota
	KeyQuit
	KeyBack
	KeyTab
	KeyEnter
	KeySpace
	KeyUp
	KeyDown
	KeyLeft
	KeyRight
	KeyNav4
	KeyNav5
	KeyNav6
	KeyNav7
	KeyNav8
	KeyHelp
	KeyDelete
	KeyMark
)

const (
	evKey         uint16 = 0x01
	keyValPress   int32  = 1
	keyValRepeat  int32  = 2
	codeEsc       uint16 = 1
	code1         uint16 = 2
	code4         uint16 = 5
	code5         uint16 = 6
	code6         uint16 = 7
	code7         uint16 = 8
	code8         uint16 = 9
	codeBackspace uint16 = 14
	codeTab       uint16 = 15
	codeQ         uint16 = 16
	codeEnter     uint16 = 28
	codeD         uint16 = 32
	codeF         uint16 = 33
	codeH         uint16 = 35
	codeZ         uint16 = 44
	codeX         uint16 = 45
	codeC         uint16 = 46
	codeSpace     uint16 = 57
	codeUp        uint16 = 103
	codeLeft      uint16 = 105
	codeRight     uint16 = 106
	codeDown      uint16 = 108
	codeDelete    uint16 = 111
)

// MapLinuxKeyCode converts a Linux evdev keycode into a KeyAction.
func MapLinuxKeyCode(code uint16) KeyAction {
	switch code {
	case codeEsc:
		return KeyBack
	case codeQ:
		return KeyQuit
	case codeTab:
		return KeyTab
	case codeEnter:
		return KeyEnter
	case codeSpace:
		return KeySpace
	case codeUp, codeF:
		return KeyUp
	case codeDown, codeX:
		return KeyDown
	case codeLeft, codeZ:
		return KeyLeft
	case codeRight, codeC:
		return KeyRight
	case code4:
		return KeyNav4
	case code5:
		return KeyNav5
	case code6:
		return KeyNav6
	case code7:
		return KeyNav7
	case code8:
		return KeyNav8
	case codeH:
		return KeyHelp
	case codeDelete, codeBackspace:
		return KeyDelete
	case code1, codeD:
		return KeyMark
	default:
		return KeyNone
	}
}

const (
	evdevEventSize = 24
	evdevChanBuf   = 32
)

// StartEvdevReader opens CardputerZero keyboard evdev nodes and emits KeyActions.
func StartEvdevReader() (<-chan KeyAction, func()) {
	ch := make(chan KeyAction, evdevChanBuf)
	done := make(chan struct{})
	var files []*os.File
	for _, p := range findKeyboardEvdevPaths() {
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		files = append(files, f)
		go readEvdevLoop(f, ch, done)
	}
	return ch, func() {
		close(done)
		for _, f := range files {
			_ = f.Close()
		}
	}
}

func readEvdevLoop(dev *os.File, ch chan<- KeyAction, done <-chan struct{}) {
	buf := make([]byte, evdevEventSize)
	for {
		select {
		case <-done:
			return
		default:
		}
		n, err := dev.Read(buf)
		if err != nil || n < evdevEventSize {
			return
		}
		evType := binary.LittleEndian.Uint16(buf[16:18])
		evCode := binary.LittleEndian.Uint16(buf[18:20])
		evVal := int32(binary.LittleEndian.Uint32(buf[20:24]))
		if evType == evKey && (evVal == keyValPress || evVal == keyValRepeat) {
			if act := MapLinuxKeyCode(evCode); act != KeyNone {
				select {
				case ch <- act:
				default:
				}
			}
		}
	}
}

func findKeyboardEvdevPaths() []string {
	var out []string
	byPath, _ := filepath.Glob("/dev/input/by-path/*i2c*-event")
	out = append(out, byPath...)
	if len(out) == 0 {
		all, _ := filepath.Glob("/dev/input/event*")
		for _, p := range all {
			if !strings.Contains(p, "mouse") {
				out = append(out, p)
			}
		}
	}
	return out
}
