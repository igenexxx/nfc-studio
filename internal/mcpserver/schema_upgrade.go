package mcpserver

func upgradeToolDefinitions() []toolDefinition {
	return []toolDefinition{
		{
			Name:        "nfc_write_ntag",
			Description: "Write 4-byte pages to an NTAG213/215/216 tag via WRITE (0xA2) with system-page protection and read-back verification. Requires confirm_write=true.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"start_page":         map[string]any{"type": "integer", "minimum": 0, "maximum": 255},
					"pages_hex":          map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"allow_system_pages": map[string]any{"type": "boolean", "default": false},
					"verify":             map[string]any{"type": "boolean", "default": true},
					"confirm_write":      map[string]any{"type": "boolean"},
				},
				"required": []string{"start_page", "pages_hex", "confirm_write"},
			},
		},
		{
			Name:        "nfc_clone_magic_uid",
			Description: "Clone a 4-byte UID with automatic BCC calculation onto a Gen1A (0x40/0x43) or Gen2/CUID Magic Card. Requires confirm_write=true.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"gen":           map[string]any{"type": "string", "enum": []string{"gen1a", "gen2"}},
					"uid_hex":       map[string]any{"type": "string"},
					"atqa_hex":      map[string]any{"type": "string"},
					"sak_hex":       map[string]any{"type": "string"},
					"key_a_hex":     map[string]any{"type": "string"},
					"confirm_write": map[string]any{"type": "boolean"},
				},
				"required": []string{"gen", "uid_hex", "confirm_write"},
			},
		},
		{
			Name:        "nfc_compose_ndef",
			Description: "Compose an NDEF TLV (URI, Text, or Wi-Fi WSC) and pack into 4-byte NTAG pages. Optional save_name or write_to_tag (requires confirm_write=true).",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"kind":           map[string]any{"type": "string", "enum": []string{"uri", "text", "wifi"}},
					"url":            map[string]any{"type": "string"},
					"text":           map[string]any{"type": "string"},
					"lang":           map[string]any{"type": "string"},
					"ssid":           map[string]any{"type": "string"},
					"password":       map[string]any{"type": "string"},
					"auth":           map[string]any{"type": "string", "enum": []string{"OPEN", "WPA2", "WPA3"}},
					"max_user_pages": map[string]any{"type": "integer"},
					"write_to_tag":   map[string]any{"type": "boolean"},
					"confirm_write":  map[string]any{"type": "boolean"},
					"save_name":      map[string]any{"type": "string"},
				},
				"required": []string{"kind"},
			},
		},
		{
			Name:        "nfc_dict_scan",
			Description: "Scan MIFARE Classic 1K/4K sectors using built-in default keys and ~/.config/nfc-studio/mf_keys.dict (known-key dictionary only; no CRYPTO1 cracking).",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"max_keys":  map[string]any{"type": "integer", "minimum": 1, "maximum": 256, "default": 64},
					"dict_path": map[string]any{"type": "string"},
					"save_name": map[string]any{"type": "string"},
				},
			},
		},
		{
			Name:        "nfc_inspect_emv",
			Description: "Strictly read-only ISO-DEP / EMV APDU inspector (RATS + SELECT PPSE + READ RECORD) returning AID, label, expiry, and masked PAN.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			Name:        "nfc_scan_multiprotocol",
			Description: "Sequential ST25R3916 multi-protocol discovery across NFC-A (ISO 14443-A), NFC-B (ISO 14443-B), NFC-F (FeliCa), and NFC-V (ISO 15693).",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"protocols":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"attempts":     map[string]any{"type": "integer", "minimum": 1, "maximum": 10, "default": 2},
					"max_v_blocks": map[string]any{"type": "integer", "minimum": 0, "maximum": 64, "default": 16},
				},
			},
		},
	}
}
