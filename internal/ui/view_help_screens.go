package ui

func helpForHome(expert bool) ScreenHelp {
	summary := [2]string{
		"Main launcher for NFC Studio (Simple, 5 tools).",
		"Press 5 EXP to unlock +3 engineering labs.",
	}
	if expert {
		summary = [2]string{
			"Main launcher in Expert Mode (all 8 tools).",
			"Includes Radar Lab, Hex Dump & Auto-Emu Probe.",
		}
	}
	return ScreenHelp{
		ScreenTitle: "HOME MENU",
		Summary:     summary,
		Keys: []HelpKeyHint{
			{"F/X", "Move selection up / down", false},
			{"4/Ent", "Open highlighted NFC tool", true},
			{"5 EXP", "Toggle Simple / Expert Mode (saved)", true},
			{"6 READ", "Quick-open Read & Clone wizard", true},
			{"7 EMU", "Quick-open Emulate Card screen", true},
			{"W", "Jump to Write / Clone to Card", false},
			{"Esc/8", "Exit prompt (Enter/Esc confirms)", true},
			{"Q", "Quit NFC Studio & turn RF field OFF", false},
			{"H", "Show or hide this Help window", false},
		},
	}
}

func helpForRead(captured, expert bool) ScreenHelp {
	if !captured {
		keys := []HelpKeyHint{
			{"4/Spc", "Pause / resume 13.56 MHz polling", true},
			{"Enter", "Pause/resume poll (or read SIM tag)", false},
		}
		if expert {
			keys = append(keys, HelpKeyHint{"7 LAB", "Jump to Expert NFC Radar Lab", true})
		}
		keys = append(keys, HelpKeyHint{"Esc/8", "Return to Home Menu", true})
		return ScreenHelp{
			ScreenTitle: "READ & CLONE (LISTEN)",
			Summary: [2]string{
				"Hold any ISO 14443-A/B, FeliCa, or NFC-V tag",
				"near Cap NFC coil; auto-freezes on read.",
			},
			Keys: keys,
		}
	}
	return ScreenHelp{
		ScreenTitle: "READ & CLONE (CAPTURED)",
		Summary: [2]string{
			"Card read! Emulate, save .nfc, write to blank,",
			"or inspect memory pages & NDEF payload.",
		},
		Keys: []HelpKeyHint{
			{"4/Ent", "Emulate card via ST25R3916 (NFC-A/F)", true},
			{"5 SAVE", "Save .nfc & open My Cards library", true},
			{"6/W", "Write / clone dump to blank card", true},
			{"7 INFO", "Inspect NDEF / Hex memory dump", true},
			{"Space", "Discard & listen for next card", false},
			{"Esc/8", "Return to Home Menu", true},
		},
	}
}

func helpForSaved(expert bool) ScreenHelp {
	if !expert {
		return ScreenHelp{
			ScreenTitle: "MY CARDS",
			Summary: [2]string{
				"Personal .nfc cards in ~/nfc-studio/tags/.",
				"Emulate, write to blank, inspect, or delete.",
			},
			Keys: []HelpKeyHint{
				{"F/X", "Select saved .nfc card", false},
				{"4/Ent", "Emulate selected card immediately", true},
				{"5/W", "Write / clone selected card to blank", true},
				{"6 INFO", "View card NDEF / memory details", true},
				{"7/Del", "Delete selected .nfc card", true},
				{"Esc/8", "Return to Home Menu", true},
			},
		}
	}
	return ScreenHelp{
		ScreenTitle: "MY CARDS (EXPERT)",
		Summary: [2]string{
			"Manage read-write User .nfc library safely.",
			"Supports [x] multi-select & 2-step batch del.",
		},
		Keys: []HelpKeyHint{
			{"F/X", "Select saved .nfc card", false},
			{"4/Ent", "Emulate selected card via ST25R3916", true},
			{"5/W", "Write / clone selected card to blank", true},
			{"6 DUMP", "Open color-coded Hex / NDEF Dump", true},
			{"7/Del", "Delete selected (2-step if batch)", true},
			{"Spc/D", "Mark/unmark [x] for batch delete", false},
			{"8/Tab", "Next Expert tab (Esc = Home)", true},
		},
	}
}

func helpForEmu(expert bool) ScreenHelp {
	summary := [2]string{
		"ST25R3916 Passive Target A (NFC-A) & F (FeliCa).",
		"Live HITS counter & external reader RF radar.",
	}
	if !expert {
		return ScreenHelp{
			ScreenTitle: "EMULATE CARD",
			Summary:     summary,
			Keys: []HelpKeyHint{
				{"4/Spc", "Start / stop 13.56 MHz emulation", true},
				{"5 CARD", "Switch to My Cards to pick a tag", true},
				{"7 RST", "Reset external reader HITS counter", true},
				{"Esc/8", "Stop emulation (RF OFF) -> Home", true},
			},
		}
	}
	return ScreenHelp{
		ScreenTitle: "EMULATE CARD",
		Summary:     summary,
		Keys: []HelpKeyHint{
			{"4/Spc", "Start / stop 13.56 MHz emulation", true},
			{"5 RND", "Generate random UID / FeliCa IDm", true},
			{"6 TYPE", "Cycle NTAG 7B / MFC1K 4B / FeliCa", true},
			{"7 RST", "Reset external reader HITS counter", true},
			{"8/Tab", "Stop EMU & next Expert tab (Esc=Home)", true},
		},
	}
}

func helpForWrite(busy, armed bool) ScreenHelp {
	if busy {
		return ScreenHelp{
			ScreenTitle: "WRITE TO CARD (WRITING)",
			Summary: [2]string{
				"Writing pages/blocks & verifying read-back.",
				"Keep blank card flat & steady on Cap NFC coil.",
			},
			Keys: []HelpKeyHint{
				{"4 STOP", "Abort write & turn RF field OFF", true},
				{"Esc/8", "Cancel write & turn RF field OFF", true},
			},
		}
	}
	if armed {
		return ScreenHelp{
			ScreenTitle: "WRITE TO CARD (ARMED)",
			Summary: [2]string{
				"2-step write safety armed (5-second window).",
				"Hold blank card flat on coil & confirm write.",
			},
			Keys: []HelpKeyHint{
				{"4/Ent", "WRITE NOW to blank card (verified)", true},
				{"Space", "Disarm & probe blank compatibility", false},
				{"5/6/7", "Disarm & change mode/trailer/subtab", false},
				{"Esc/8", "Disarm & return to previous screen", true},
			},
		}
	}
	return ScreenHelp{
		ScreenTitle: "WRITE TO CARD",
		Summary: [2]string{
			"AUTO blank detect: NTAG, Gen1A, Gen2/MF & NFC-V.",
			"2-step arming (4 ARM -> 4 WRITE) + verify.",
		},
		Keys: []HelpKeyHint{
			{"4/Ent", "Arm 5s write window -> press again", true},
			{"Space", "Probe blank card & compatibility", false},
			{"5 MODE", "Cycle write mode or NDEF kind", true},
			{"6 SAFE", "Toggle MIFARE trailers / Save NDEF", true},
			{"7/Z/C", "Switch DUMP <-> QUICK NDEF writer", true},
			{"F/X", "Cycle Quick NDEF presets (NDEF tab)", false},
			{"Esc/8", "Return to previous screen", true},
		},
	}
}

func helpForScan() ScreenHelp {
	return ScreenHelp{
		ScreenTitle: "NFC RADAR LAB",
		Summary: [2]string{
			"13.56 MHz multi-protocol poll & ADC RF radar.",
			"Shows coupling %, metal detune & DumpNet ML.",
		},
		Keys: []HelpKeyHint{
			{"4/Spc", "Pause / resume live 13.56 MHz scan", true},
			{"5/Ent", "Emulate detected tag (NFC-A/FeliCa)", true},
			{"6 SAVE", "Save tag as Flipper v4 .nfc file", true},
			{"7 DUMP", "Inspect color-coded Hex / NDEF dump", true},
			{"8/Tab", "Next Expert tab (Esc = Home)", true},
		},
	}
}

func helpForDump(ndefTab, expert bool) ScreenHelp {
	k8 := HelpKeyHint{"Esc/8", "Return to Home Menu", true}
	if expert {
		k8 = HelpKeyHint{"8/Tab", "Next Expert tab (Esc = Home)", true}
	}
	if ndefTab {
		return ScreenHelp{
			ScreenTitle: "CARD MEMORY (NDEF)",
			Summary: [2]string{
				"Decoded NDEF URI / Text TLV (0x03) summary.",
				"Shows card family, UID & parsed record data.",
			},
			Keys: []HelpKeyHint{
				{"4/Spc", "Switch to color-coded HEX page dump", true},
				{"5 EMU", "Emulate current card via ST25R3916", true},
				{"6 SAVE", "Save card as Flipper v4 .nfc file", true},
				{"W", "Write / clone card to blank target", false},
				k8,
			},
		}
	}
	return ScreenHelp{
		ScreenTitle: "CARD MEMORY (HEX)",
		Summary: [2]string{
			"Color-coded pages: Cyan UID, Amber CC/Trlr,",
			"Emerald User/NDEF, Gray locked MF blocks.",
		},
		Keys: []HelpKeyHint{
			{"F/X", "Scroll memory pages / blocks up/down", false},
			{"Z/C", "Cycle MF view [0..7]/[8..15]/[ASCII]", false},
			{"4/Spc", "Switch to decoded NDEF summary view", true},
			{"5 EMU", "Emulate current card via ST25R3916", true},
			{"6 SAVE", "Save card as Flipper v4 .nfc file", true},
			{"7 TOP", "Jump scroll back to Page / Block 0", true},
			{"W", "Write / clone card to blank target", false},
			k8,
		},
	}
}

func helpForProbe(running bool) ScreenHelp {
	if running {
		return ScreenHelp{
			ScreenTitle: "AUTO-EMU PROBE (RUNNING)",
			Summary: [2]string{
				"Auto-emulating .nfc cards from active pack.",
				"Stop immediately on hit, then rewind >/*.",
			},
			Keys: []HelpKeyHint{
				{"4/Spc", "STOP immediately & pause session", true},
				{"Enter", "STOP immediately (same as 4/Space)", false},
				{"5/Z", "Faster dwell interval (down to 0.5s)", true},
				{"6/C", "Slower dwell interval (up to 8.0s)", true},
				{"7 PACK", "Stop & switch to next .nfc pack", true},
				{"8/Tab", "Stop & next Expert tab (Esc = Home)", true},
			},
		}
	}
	return ScreenHelp{
		ScreenTitle: "AUTO-EMU PROBE",
		Summary: [2]string{
			"Step through DB:* packs, zerosub, or USER lib.",
			"Backtrack hits using > (last) & * markers.",
		},
		Keys: []HelpKeyHint{
			{"4/Spc", "Start or resume Auto-Emu session", true},
			{"5/Ent", "Single-emulate highlighted .nfc card", true},
			{"6 SAVE", "Promote winning card to USER library", true},
			{"7 PACK", "Cycle DB:* -> zerosub -> USER packs", true},
			{"F/X", "Step cursor up/down (rewind >/*)", false},
			{"Z/C", "Adjust dwell interval (0.5s - 8.0s)", false},
			{"D/Del", "Mark [x] / delete DB file (ignored)", false},
			{"8/Tab", "Next Expert tab (Esc = Home)", true},
		},
	}
}

func helpForSettings(expert bool) ScreenHelp {
	if !expert {
		return ScreenHelp{
			ScreenTitle: "SETTINGS (SIMPLE)",
			Summary: [2]string{
				"Configure Expert Mode, Auto Scan, opt-in SSH",
				"MCP Server & remote .nfc DB delta sync.",
			},
			Keys: []HelpKeyHint{
				{"F/X", "Select setting row (4 options)", false},
				{"4/Ent", "Toggle / activate selected setting", true},
				{"5 SAVE", "Save ~/.config/nfc-studio/config.json", true},
				{"6 SYNC", "Run incremental remote DB sync", true},
				{"Esc/8", "Return to Home Menu", true},
			},
		}
	}
	return ScreenHelp{
		ScreenTitle: "SETTINGS (EXPERT)",
		Summary: [2]string{
			"Full NFC preferences, opt-in SSH MCP Server,",
			"DB sync & Manage DB Sources manager.",
		},
		Keys: []HelpKeyHint{
			{"F/X", "Select setting row (5 options)", false},
			{"4/Ent", "Toggle setting or open Manage DBs", true},
			{"5 SAVE", "Save ~/.config/nfc-studio/config.json", true},
			{"6 RST", "Restore safe factory defaults", true},
			{"7 HELP", "Open this context Help modal", true},
			{"Esc/8", "Return to Home Menu (Tab = Scan)", true},
		},
	}
}

func helpForSources() ScreenHelp {
	return ScreenHelp{
		ScreenTitle: "MANAGE DB SOURCES",
		Summary: [2]string{
			"Manage GitHub/HTTP .nfc sources & local packs.",
			"Supports [x] multi-select & 2-step deletion.",
		},
		Keys: []HelpKeyHint{
			{"F/X", "Move cursor through sources/packs", false},
			{"Spc/D", "Mark/unmark [x] for batch actions", false},
			{"4 ON", "Toggle selected source(s) ON/OFF", true},
			{"5 DIR", "Delete local pack folder (press 2x)", true},
			{"6/Del", "Delete source + folder (press 2x)", true},
			{"7 SYNC", "Run incremental delta DB sync now", true},
			{"Esc/8", "Back to Settings & refresh packs", true},
		},
	}
}
