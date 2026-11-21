package mcpserver

func toolDefinitions() []toolDefinition {
	return []toolDefinition{
		{
			Name:        "nfc_get_status",
			Description: "Get ST25R3916 chip info (identity, revision, supply voltage or sim_mode) and current NFC Studio settings.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			Name:        "nfc_scan_tag",
			Description: "Poll the ST25R3916 13.56 MHz field for an ISO 14443-A tag, returning UID, ATQA, SAK, memory pages, and parsed NDEF.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"attempts": map[string]any{
						"type":        "integer",
						"description": "Number of poll attempts (1..10, default 3).",
						"minimum":     1,
						"maximum":     10,
						"default":     3,
					},
				},
			},
		},
		{
			Name:        "nfc_parse_ndef",
			Description: "Decode an NDEF TLV (0x03) URI or Text record from NTAG/Ultralight hex pages.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"pages_hex": map[string]any{
						"type":        "array",
						"description": "List of 4-byte hex page strings starting from Page 0.",
						"items":       map[string]any{"type": "string"},
					},
				},
				"required": []string{"pages_hex"},
			},
		},
		{
			Name:        "nfc_emulate_tag",
			Description: "Emulate an ISO 14443-A passive target UID on ST25R3916 for duration_ms and count external reader hits. Requires confirm_rf=true.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"uid_hex": map[string]any{
						"type":        "string",
						"description": "4-byte or 7-byte UID in hex (e.g. '04 A1 B2 C3 D4 E5 F6').",
					},
					"atqa_hex": map[string]any{
						"type":        "string",
						"description": "Optional 2-byte ATQA in hex (e.g. '44 00').",
					},
					"sak_hex": map[string]any{
						"type":        "string",
						"description": "Optional 1-byte SAK in hex (e.g. '00' or '08').",
					},
					"duration_ms": map[string]any{
						"type":        "integer",
						"description": "Emulation window in milliseconds (500..30000, default 3000).",
						"minimum":     500,
						"maximum":     30000,
						"default":     3000,
					},
					"confirm_rf": map[string]any{
						"type":        "boolean",
						"description": "Must be true to authorize RF target emulation.",
					},
				},
				"required": []string{"uid_hex", "confirm_rf"},
			},
		},
		{
			Name:        "nfc_list_saved",
			Description: "List saved Flipper-compatible .nfc tag files from the tags directory and ~/zerosub/nfc.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			Name:        "nfc_read_saved",
			Description: "Read and parse a saved .nfc file by filename, returning its metadata, hex pages, and decoded NDEF record.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{
						"type":        "string",
						"description": "Filename of the saved .nfc file (no path separators).",
					},
				},
				"required": []string{"name"},
			},
		},
		{
			Name:        "nfc_save_tag",
			Description: "Save a Flipper-compatible .nfc tag file into the tags directory.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{
						"type":        "string",
						"description": "Output filename (no path separators; .nfc suffix appended if omitted).",
					},
					"device_type": map[string]any{
						"type":        "string",
						"description": "Optional card type label (e.g. 'NTAG215', 'MIFARE Classic 1K').",
					},
					"uid_hex": map[string]any{
						"type":        "string",
						"description": "Tag UID in hex.",
					},
					"atqa_hex": map[string]any{
						"type":        "string",
						"description": "Optional 2-byte ATQA in hex.",
					},
					"sak_hex": map[string]any{
						"type":        "string",
						"description": "Optional 1-byte SAK in hex.",
					},
					"pages_hex": map[string]any{
						"type":        "array",
						"description": "Optional memory pages in hex.",
						"items":       map[string]any{"type": "string"},
					},
				},
				"required": []string{"name", "uid_hex"},
			},
		},
		{
			Name:        "nfc_delete_saved",
			Description: "Delete a saved .nfc file from the tags directory. Requires confirm=true.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{
						"type":        "string",
						"description": "Filename inside the tags directory to delete.",
					},
					"confirm": map[string]any{
						"type":        "boolean",
						"description": "Must be true to authorize deletion.",
					},
				},
				"required": []string{"name", "confirm"},
			},
		},
	}
}
