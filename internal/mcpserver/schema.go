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
			Description: "Emulate an ISO 14443-A (NFC-A) or FeliCa (NFC-F) passive target on ST25R3916 for duration_ms and count external reader hits. Requires confirm_rf=true.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"protocol": map[string]any{
						"type":        "string",
						"enum":        []string{"NFC-A", "NFC-F"},
						"description": "Target protocol: 'NFC-A' (default, 4B/7B UID) or 'NFC-F' (FeliCa 8B IDm).",
						"default":     "NFC-A",
					},
					"uid_hex": map[string]any{
						"type":        "string",
						"description": "4-byte or 7-byte UID for NFC-A, or 8-byte IDm for NFC-F (e.g. '01 23 45 67 89 AB CD EF').",
					},
					"atqa_hex": map[string]any{
						"type":        "string",
						"description": "Optional 2-byte ATQA in hex for NFC-A (e.g. '44 00').",
					},
					"sak_hex": map[string]any{
						"type":        "string",
						"description": "Optional 1-byte SAK in hex for NFC-A (e.g. '00' or '08').",
					},
					"pmm_hex": map[string]any{
						"type":        "string",
						"description": "Optional 8-byte PMm in hex for NFC-F (defaults to FeliCa Lite-S PMm).",
					},
					"system_code_hex": map[string]any{
						"type":        "string",
						"description": "Optional 2-byte System Code in hex for NFC-F (default '88 B4').",
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
		{
			Name:        "nfc_list_databases",
			Description: "List separated .nfc catalogs: external read-only database packs (~/nfc-studio/databases/<pack>, ~/zerosub/nfc) and User Library (~/nfc-studio/tags).",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			Name:        "nfc_promote_to_user",
			Description: "Copy a winning .nfc file from an external database pack into User Library (~/nfc-studio/tags) with collision-safe naming.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"pack": map[string]any{"type": "string", "description": "External pack_id (e.g. db:amiibo or ext:zerosub)"},
					"name": map[string]any{"type": "string", "description": "Filename inside the pack (no path separators)"},
				},
				"required": []string{"pack", "name"},
			},
		},
		{
			Name:        "nfc_list_sources",
			Description: "List configured remote .nfc database sources from sources.json.",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			Name:        "nfc_add_source",
			Description: "Add or update a remote .nfc database source (github via github_url or owner/repo/branch/subpath, or manifest via https url).",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id":         map[string]any{"type": "string", "description": "Unique pack subfolder ID (a-z0-9_-, 1..32 chars)"},
					"type":       map[string]any{"type": "string", "description": "github or manifest"},
					"github_url": map[string]any{"type": "string", "description": "Optional GitHub repo/tree URL"},
					"owner":      map[string]any{"type": "string"},
					"repo":       map[string]any{"type": "string"},
					"branch":     map[string]any{"type": "string"},
					"subpath":    map[string]any{"type": "string"},
					"url":        map[string]any{"type": "string"},
					"prune":      map[string]any{"type": "boolean"},
				},
				"required": []string{"id", "type"},
			},
		},
		{
			Name:        "nfc_remove_source",
			Description: "Remove one or more remote .nfc database sources from sources.json (requires confirm=true; optional delete_files=true removes ~/nfc-studio/databases/<id>/).",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id":           map[string]any{"type": "string"},
					"ids":          map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"confirm":      map[string]any{"type": "boolean"},
					"delete_files": map[string]any{"type": "boolean"},
				},
				"required": []string{"confirm"},
			},
		},
		{
			Name:        "nfc_sync_sources",
			Description: "Run incremental ETag/SHA delta sync for a single source ID or all enabled .nfc sources.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"id": map[string]any{"type": "string"}},
			},
		},
		{
			Name:        "nfc_delete_pack_files",
			Description: "Delete one or more .nfc files inside ~/nfc-studio/databases/<pack>/ (requires confirm=true; marks deleted files ignored on sync by default).",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"pack":           map[string]any{"type": "string"},
					"names":          map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"ignore_on_sync": map[string]any{"type": "boolean"},
					"confirm":        map[string]any{"type": "boolean"},
				},
				"required": []string{"pack", "names", "confirm"},
			},
		},
		{
			Name:        "nfc_analyze_tag",
			Description: "Run on-device nfcml Dump Profiler (Shannon entropy, fill/ASCII ratio, BLANK/NDEF/PROPRIETARY/ENCRYPTED class) and 8D tag embedding on a saved .nfc file or hex pages.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name":      map[string]any{"type": "string", "description": "Optional saved .nfc filename"},
					"pages_hex": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				},
			},
		},
		{
			Name:        "nfc_match_fingerprint",
			Description: "Match a tag's 8D structural fingerprint (UID/ATQA/SAK/pages/entropy) against saved .nfc tags using cosine k-NN.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"uid_hex":        map[string]any{"type": "string"},
					"atqa_hex":       map[string]any{"type": "string"},
					"sak_hex":        map[string]any{"type": "string"},
					"pages_hex":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"min_similarity": map[string]any{"type": "number"},
					"top_k":          map[string]any{"type": "integer"},
				},
				"required": []string{"uid_hex"},
			},
		},
		{
			Name:        "nfc_measure_field",
			Description: "Measure ST25R3916 13.56 MHz RF field amplitude & phase via ADC and classify antenna coupling (IDLE, WEAK, SWEET, METAL) using EWMA + Z-score baseline.",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		},
	}
}

func allToolDefinitions() []toolDefinition {
	defs := toolDefinitions()
	return append(defs, upgradeToolDefinitions()...)
}
