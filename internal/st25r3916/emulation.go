package st25r3916

// StartEmulation loads Passive Target A memory and listens for an external reader.
func (c *Chip) StartEmulation(uid []byte, atqa [2]byte, sak uint8) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	mem, err := BuildPTMemoryA(uid, atqa, sak)
	if err != nil {
		return err
	}
	_ = c.command(cmdStopAll)
	c.clearInterrupts()
	frame := append([]byte{opLoadPTMemA}, mem...)
	if _, err := c.tr.Transfer(frame); err != nil {
		return err
	}
	if err := c.writeReg(regMode, modeTargetNFCA); err != nil {
		return err
	}
	return c.writeReg(regOpControl, opEnable|opRxEnable)
}

// StartEmulationF loads Passive Target F (FeliCa 212/424 kbps) memory and listens for an external reader.
func (c *Chip) StartEmulationF(idm, pmm []byte, sysCode [2]byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	mem, err := BuildPTMemoryF(idm, pmm, sysCode)
	if err != nil {
		return err
	}
	_ = c.command(cmdStopAll)
	c.clearInterrupts()
	frame := append([]byte{opLoadPTMemF}, mem...)
	if _, err := c.tr.Transfer(frame); err != nil {
		return err
	}
	if err := c.writeReg(regPassiveTarget, 0x00); err != nil {
		return err
	}
	if err := c.writeReg(regMode, modeTargetNFCF); err != nil {
		return err
	}
	return c.writeReg(regOpControl, opEnable|opRxEnable)
}

// PollTargetHit checks whether an external NFC-A or NFC-F reader has woken or selected the emulated card.
func (c *Chip) PollTargetHit() (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, err := c.readReg(regInterruptTarget)
	if err != nil {
		return false, err
	}
	return (v & (irqTargetSDDC | irqTargetWUA | irqTargetWUF)) != 0, nil
}
