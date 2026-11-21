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
	if err := c.writeReg(regMode, 0x88); err != nil {
		return err
	}
	return c.writeReg(regOpControl, opEnable|opRxEnable)
}

// PollTargetHit checks whether an external reader has woken or selected the emulated card.
func (c *Chip) PollTargetHit() (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, err := c.readReg(regInterruptTarget)
	if err != nil {
		return false, err
	}
	return (v & (irqTargetSDDC | irqTargetWUA)) != 0, nil
}
