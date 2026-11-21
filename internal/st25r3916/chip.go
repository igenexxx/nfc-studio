package st25r3916

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// Transport abstracts SPI Mode 1 transfers for testing without hardware.
type Transport interface {
	Transfer(tx []byte) ([]byte, error)
	Close() error
}

// Chip controls an ST25R3916 NFC transceiver.
type Chip struct {
	mu       sync.Mutex
	tr       Transport
	info     ChipInfo
	pollStep time.Duration
	guardT   time.Duration
}

// Open initializes the ST25R3916 over Transport and verifies its identity.
func Open(tr Transport) (*Chip, error) {
	if tr == nil {
		return nil, errors.New("st25r3916: nil transport")
	}
	c := &Chip{
		tr:       tr,
		pollStep: 500 * time.Microsecond,
		guardT:   6 * time.Millisecond,
	}
	if err := c.initialize(); err != nil {
		return nil, err
	}
	return c, nil
}

// Info returns the chip's identity and supply voltage.
func (c *Chip) Info() ChipInfo {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.info
}

// Close stops active RF operations and closes the transport.
func (c *Chip) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.command(cmdStopAll)
	_ = c.writeReg(regOpControl, 0x00)
	return c.tr.Close()
}

func (c *Chip) readReg(reg uint8) (uint8, error) {
	rx, err := c.tr.Transfer([]byte{opReadReg | (reg & 0x3F), 0x00})
	if err != nil || len(rx) < 2 {
		if err == nil {
			err = errors.New("st25r3916: short SPI read")
		}
		return 0, err
	}
	return rx[1], nil
}

func (c *Chip) readRegs(startReg uint8, count int) ([]byte, error) {
	tx := make([]byte, count+1)
	tx[0] = opReadReg | (startReg & 0x3F)
	rx, err := c.tr.Transfer(tx)
	if err != nil || len(rx) < count+1 {
		if err == nil {
			err = errors.New("st25r3916: short SPI burst read")
		}
		return nil, err
	}
	return rx[1 : count+1], nil
}

func (c *Chip) writeReg(reg, val uint8) error {
	_, err := c.tr.Transfer([]byte{reg & 0x3F, val})
	return err
}

func (c *Chip) writeRegB(reg, val uint8) error {
	_, err := c.tr.Transfer([]byte{cmdSpaceBAccess, reg & 0x3F, val})
	return err
}

func (c *Chip) command(cmd uint8) error {
	_, err := c.tr.Transfer([]byte{cmd})
	return err
}

func (c *Chip) clearInterrupts() {
	_, _ = c.readRegs(regInterrupt, 4)
}

func (c *Chip) readInterrupts() (mainIRQ, timerIRQ, errWupIRQ, targetIRQ uint8, err error) {
	buf, err := c.readRegs(regInterrupt, 4)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	return buf[0], buf[1], buf[2], buf[3], nil
}

func (c *Chip) setNoResponseTimer(timeoutMs uint32) error {
	ticks := NoResponseTicks(timeoutMs)
	if err := c.writeReg(regTimerEMVControl, 0x00); err != nil {
		return err
	}
	if err := c.writeReg(regNoRespTimer1, uint8(ticks>>8)); err != nil {
		return err
	}
	return c.writeReg(regNoRespTimer2, uint8(ticks&0xFF))
}

func (c *Chip) setTxBytes(n int) error {
	if err := c.writeReg(regTxBytes1, uint8((n>>5)&0xFF)); err != nil {
		return err
	}
	return c.writeReg(regTxBytes2, uint8((n&0x1F)<<3))
}

func (c *Chip) writeFIFO(data []byte) error {
	frame := make([]byte, len(data)+1)
	frame[0] = opLoadFIFO
	copy(frame[1:], data)
	_, err := c.tr.Transfer(frame)
	return err
}

const (
	maxFIFOSize     = 512
	idProbeRetries  = 5
	regValIOConfig1 = 0x07
	regValIOConfig2 = 0x3C
	regValTxDriver  = 0xD0
	supply3VMinMV   = 2400
	supply3VMaxMV   = 4000
	supplySettle    = 2 * time.Millisecond
)

func (c *Chip) readFIFO() ([]byte, error) {
	st, err := c.readRegs(regFIFOStatus1, 2)
	if err != nil {
		return nil, err
	}
	count := min(int(st[0])|((int(st[1]&0xC0)>>6)<<8), maxFIFOSize)
	if count == 0 {
		return nil, nil
	}
	tx := make([]byte, count+1)
	tx[0] = opReadFIFO
	rx, err := c.tr.Transfer(tx)
	if err != nil || len(rx) < count+1 {
		if err == nil {
			err = errors.New("st25r3916: short FIFO read")
		}
		return nil, err
	}
	return rx[1 : count+1], nil
}

func (c *Chip) waitRx(timeout time.Duration, checkCRC bool) ([]byte, error) {
	deadline := time.Now().Add(timeout)
	for {
		mainIRQ, timerIRQ, errIRQ, _, err := c.readInterrupts()
		if err != nil {
			return nil, err
		}
		if mainIRQ&irqMainCol != 0 {
			return nil, ErrCollision
		}
		if mainIRQ&irqMainRXE != 0 {
			errMask := irqErrHardFraming | irqErrSoftFraming | irqErrParity
			if checkCRC {
				errMask |= irqErrCRC
			}
			if errIRQ&errMask != 0 {
				return nil, fmt.Errorf("%w (irqErr=0x%02X)", ErrCRC, errIRQ)
			}
			return c.readFIFO()
		}
		if timerIRQ&irqTimerNRE != 0 {
			return nil, ErrNoTag
		}
		if time.Now().After(deadline) {
			return nil, ErrTimeout
		}
		if c.pollStep > 0 {
			time.Sleep(c.pollStep)
		}
	}
}

func (c *Chip) initialize() error {
	id, err := c.detectChipIdentity()
	if err != nil {
		return err
	}
	c.info.Identity = id
	c.info.Revision = id & 0x07

	if err := c.applyDefaultRegisters(); err != nil {
		return err
	}
	c.measureAndConfigureSupply()
	c.clearInterrupts()
	return nil
}

func (c *Chip) detectChipIdentity() (uint8, error) {
	for range idProbeRetries {
		if v, err := c.readReg(regIdentity); err == nil && IsST25R3916(v) {
			return v, nil
		}
	}
	return 0, fmt.Errorf("st25r3916: chip not detected (id=0x%02X)", uint8(0))
}

func (c *Chip) applyDefaultRegisters() error {
	if err := c.command(cmdStopAll); err != nil {
		return err
	}
	if err := c.command(cmdSetDefault); err != nil {
		return err
	}
	// ST erratum: prevent premature overheat protection.
	if _, err := c.tr.Transfer([]byte{cmdTestAccess, 0x04, 0x10}); err != nil {
		return err
	}
	if err := c.writeReg(regIOConfig1, regValIOConfig1); err != nil {
		return err
	}
	if err := c.writeReg(regIOConfig2, regValIOConfig2); err != nil {
		return err
	}
	if err := c.writeReg(regTxDriver, regValTxDriver); err != nil {
		return err
	}
	_ = c.writeRegB(regBResistiveAm, 0x80)
	_ = c.writeRegB(regBResistiveAm, 0x00)
	_ = c.writeReg(regFieldOnThresh, 0x13)
	_ = c.writeReg(regFieldOffThresh, 0x02)
	_ = c.writeReg(regPTModulation, 0x5F)
	_ = c.writeRegB(regBEmdSuppression, 0x40)
	_ = c.writeReg(regAntennaTuning1, 0x82)
	_ = c.writeReg(regAntennaTuning2, 0x82)
	_ = c.command(cmdClearFIFO)
	return nil
}

func (c *Chip) measureAndConfigureSupply() {
	// Enable oscillator so power measurement and subsequent RF ops are ready.
	_ = c.writeReg(regOpControl, opEnable)
	_ = c.writeReg(regRegulatorCtrl, 0x00)
	_ = c.command(cmdMeasurePowerSupp)
	if c.pollStep > 0 {
		time.Sleep(supplySettle)
	}
	if raw, err := c.readReg(regADConverterOut); err == nil {
		c.info.SupplyMV = ADCToSupplyMV(raw)
		if c.info.SupplyMV >= supply3VMinMV && c.info.SupplyMV < supply3VMaxMV {
			_ = c.writeReg(regIOConfig2, regValIOConfig2|ioSupply3V)
		}
	}
}

// FieldOn enables the 13.56 MHz NFC-A reader field.
func (c *Chip) FieldOn() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.fieldOnLocked()
}

func (c *Chip) fieldOnLocked() error {
	if err := c.writeReg(regMode, 0x08); err != nil {
		return err
	}
	if err := c.writeReg(regBitrate, 0x00); err != nil {
		return err
	}
	if err := c.writeReg(regISO14443A, 0x00); err != nil {
		return err
	}
	if err := c.writeReg(regAux, 0x00); err != nil {
		return err
	}
	if err := c.writeReg(regOpControl, opEnable|opRxEnable|opTxEnable); err != nil {
		return err
	}
	if c.guardT > 0 {
		time.Sleep(c.guardT)
	}
	return nil
}

// FieldOff turns off the 13.56 MHz RF field.
func (c *Chip) FieldOff() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.fieldOffLocked()
}

func (c *Chip) fieldOffLocked() error {
	_ = c.command(cmdStopAll)
	return c.writeReg(regOpControl, opEnable)
}
