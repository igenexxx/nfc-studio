package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"nfc-studio/internal/nfcfile"
	"nfc-studio/internal/st25r3916"
)

// NFCWriter is an optional capability interface implemented by write-capable backends.
type NFCWriter interface {
	WriteType2Pages(ctx context.Context, ops []st25r3916.WritePageOp, opts st25r3916.WriteOptions) (st25r3916.WriteReport, error)
	CloneMagicUID(ctx context.Context, gen st25r3916.MagicCardGen, uid []byte, atqa [2]byte, sak uint8, keyA [6]byte) (st25r3916.CloneReport, error)
}

func (s *Server) toolWriteNTAG(ctx context.Context, raw json.RawMessage) (toolResult, *rpcError) {
	var in struct {
		StartPage        int      `json:"start_page"`
		PagesHex         []string `json:"pages_hex"`
		AllowSystemPages bool     `json:"allow_system_pages"`
		Verify           *bool    `json:"verify"`
		ConfirmWrite     bool     `json:"confirm_write"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return toolResult{}, &rpcError{Code: errCodeInvalidParams, Message: "Invalid arguments: " + err.Error()}
	}
	if !in.ConfirmWrite {
		return errResult("confirm_write must be true to write NTAG pages"), nil
	}
	ops, err := buildWriteOps(in.StartPage, in.PagesHex)
	if err != nil {
		return errResult(err.Error()), nil
	}
	verify := true
	if in.Verify != nil {
		verify = *in.Verify
	}
	if s.backend == nil {
		return simWriteNTAGResult(ops, in.AllowSystemPages, verify), nil
	}
	writer, ok := s.backend.(NFCWriter)
	if !ok {
		return errResult("active backend does not support NTAG writes"), nil
	}
	s.hwMu.Lock()
	rep, err := writer.WriteType2Pages(ctx, ops, st25r3916.WriteOptions{
		AllowSystemPages: in.AllowSystemPages,
		VerifyReadBack:   verify,
	})
	s.hwMu.Unlock()
	if err != nil {
		return errResult("NTAG write failed: " + err.Error()), nil
	}
	return okJSON(rep), nil
}

func buildWriteOps(startPage int, pagesHex []string) ([]st25r3916.WritePageOp, error) {
	if startPage < 0 || startPage > 255 {
		return nil, errors.New("start_page must be between 0 and 255")
	}
	if len(pagesHex) == 0 {
		return nil, errors.New("pages_hex must be non-empty")
	}
	ops := make([]st25r3916.WritePageOp, 0, len(pagesHex))
	for i, h := range pagesHex {
		pg := startPage + i
		if pg > 255 {
			return nil, errors.New("page index exceeds 255")
		}
		b, err := parseHexFlex(h)
		if err != nil || len(b) != 4 {
			return nil, fmt.Errorf("pages_hex[%d] must be 4 hex bytes", i)
		}
		var data [4]byte
		copy(data[:], b)
		ops = append(ops, st25r3916.WritePageOp{Page: uint8(pg), Data: data})
	}
	return ops, nil
}

func simWriteNTAGResult(ops []st25r3916.WritePageOp, allowSystem, verify bool) toolResult {
	for _, op := range ops {
		if !allowSystem && (op.Page < 4 || op.Page > 129) {
			return errResult(st25r3916.ErrProtectedPage.Error())
		}
	}
	return okJSON(map[string]any{
		"sim_mode":      true,
		"model":         "NTAG215",
		"pages_written": len(ops),
		"verified":      verify,
		"failed_page":   -1,
	})
}

func (s *Server) toolCloneMagicUID(ctx context.Context, raw json.RawMessage) (toolResult, *rpcError) {
	var in struct {
		Gen          string `json:"gen"`
		UIDHex       string `json:"uid_hex"`
		ATQAHex      string `json:"atqa_hex"`
		SAKHex       string `json:"sak_hex"`
		KeyAHex      string `json:"key_a_hex"`
		ConfirmWrite bool   `json:"confirm_write"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return toolResult{}, &rpcError{Code: errCodeInvalidParams, Message: "Invalid arguments: " + err.Error()}
	}
	if !in.ConfirmWrite {
		return errResult("confirm_write must be true to clone a Magic Card UID"), nil
	}
	gen := st25r3916.MagicCardGen(strings.ToLower(strings.TrimSpace(in.Gen)))
	if gen != st25r3916.MagicGen1A && gen != st25r3916.MagicGen2CUID {
		return errResult("gen must be 'gen1a' or 'gen2'"), nil
	}
	uid, atqa, sak, keyA, err := parseMagicCloneArgs(in.UIDHex, in.ATQAHex, in.SAKHex, in.KeyAHex)
	if err != nil {
		return errResult(err.Error()), nil
	}
	if s.backend == nil {
		return okJSON(map[string]any{
			"sim_mode":      true,
			"gen":           gen,
			"target_uid":    nfcfile.FormatHex(uid),
			"verified":      true,
			"read_back_uid": nfcfile.FormatHex(uid),
		}), nil
	}
	writer, ok := s.backend.(NFCWriter)
	if !ok {
		return errResult("active backend does not support Magic UID cloning"), nil
	}
	s.hwMu.Lock()
	rep, err := writer.CloneMagicUID(ctx, gen, uid, atqa, sak, keyA)
	s.hwMu.Unlock()
	if err != nil {
		return errResult("magic clone failed: " + err.Error()), nil
	}
	return okJSON(rep), nil
}

func parseMagicCloneArgs(uidHex, atqaHex, sakHex, keyAHex string) ([]byte, [2]byte, uint8, [6]byte, error) {
	uid, atqa, sak, err := parseTagIdent(uidHex, atqaHex, sakHex, false)
	if err != nil {
		return nil, [2]byte{}, 0, [6]byte{}, err
	}
	if _, err := st25r3916.BuildBlock0(uid, sak, atqa); err != nil {
		return nil, [2]byte{}, 0, [6]byte{}, err
	}
	keyA := [6]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
	if strings.TrimSpace(keyAHex) != "" {
		kb, err := parseHexFlex(keyAHex)
		if err != nil || len(kb) != 6 {
			return nil, [2]byte{}, 0, [6]byte{}, errors.New("key_a_hex must be 6 hex bytes")
		}
		copy(keyA[:], kb)
	}
	return uid, atqa, sak, keyA, nil
}

func (s *Server) toolComposeNDEF(ctx context.Context, raw json.RawMessage) (toolResult, *rpcError) {
	var in struct {
		Kind         string `json:"kind"`
		URL          string `json:"url"`
		Text         string `json:"text"`
		Lang         string `json:"lang"`
		SSID         string `json:"ssid"`
		Password     string `json:"password"`
		Auth         string `json:"auth"`
		MaxUserPages int    `json:"max_user_pages"`
		WriteToTag   bool   `json:"write_to_tag"`
		ConfirmWrite bool   `json:"confirm_write"`
		SaveName     string `json:"save_name"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return toolResult{}, &rpcError{Code: errCodeInvalidParams, Message: "Invalid arguments: " + err.Error()}
	}
	if in.WriteToTag && !in.ConfirmWrite {
		return errResult("confirm_write must be true when write_to_tag is true"), nil
	}
	tlv, err := encodeNDEFByKind(in.Kind, in.URL, in.Lang, in.Text, in.SSID, in.Password, in.Auth)
	if err != nil {
		return errResult(err.Error()), nil
	}
	maxPages := in.MaxUserPages
	if maxPages == 0 {
		maxPages = 126 // NTAG215 default user pages (4..129)
	}
	packed, err := nfcfile.PackNDEFPages(tlv, maxPages)
	if err != nil {
		return errResult(err.Error()), nil
	}
	pagesHex := make([]string, len(packed))
	ops := make([]st25r3916.WritePageOp, len(packed))
	for i, pg := range packed {
		pagesHex[i] = nfcfile.FormatHex(pg[:])
		ops[i] = st25r3916.WritePageOp{Page: uint8(4 + i), Data: pg}
	}
	out := map[string]any{
		"kind":        strings.ToLower(in.Kind),
		"tlv_bytes":   len(tlv),
		"pages_count": len(packed),
		"pages_hex":   pagesHex,
	}
	if in.SaveName != "" {
		safe, err := sanitizeFilename(in.SaveName)
		if err != nil {
			return errResult(err.Error()), nil
		}
		if !strings.HasSuffix(strings.ToLower(safe), ".nfc") {
			safe += ".nfc"
		}
		fullPages := buildComposedTagPages(packed)
		saveRes := s.writeSavedTag(safe, "NTAG215", []byte{0x04, 0xA1, 0xB2, 0xC3, 0xD4, 0xE5, 0xF6}, [2]byte{0x44, 0x00}, 0x00, fullPages)
		if saveRes.IsError {
			return saveRes, nil
		}
		out["saved_name"] = safe
	}
	if in.WriteToTag {
		writeRes, err := s.applyComposedWrite(ctx, ops)
		if err != nil {
			return errResult(err.Error()), nil
		}
		out["write_report"] = writeRes
	}
	return okJSON(out), nil
}

func encodeNDEFByKind(kind, url, lang, text, ssid, password, auth string) ([]byte, error) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "uri", "url":
		return nfcfile.ComposeURL(url)
	case "text":
		return nfcfile.ComposeText(lang, text)
	case "wifi":
		var authVal nfcfile.WiFiAuth
		switch strings.ToUpper(strings.TrimSpace(auth)) {
		case "OPEN":
			authVal = nfcfile.WiFiAuthOpen
		case "WPA3":
			authVal = nfcfile.WiFiAuthWPA3
		default:
			authVal = nfcfile.WiFiAuthWPA2
		}
		return nfcfile.ComposeWiFi(nfcfile.WiFiConfig{SSID: ssid, Password: password, Auth: authVal})
	default:
		return nil, errors.New("kind must be 'uri', 'text', or 'wifi'")
	}
}

func buildComposedTagPages(userPages [][4]byte) [][]byte {
	pages := [][]byte{
		{0x04, 0xA1, 0xB2, 0x77},
		{0xC3, 0xD4, 0xE5, 0xF6},
		{0x11, 0x48, 0x00, 0x00},
		{0xE1, 0x10, 0x3E, 0x00},
	}
	for _, p := range userPages {
		pages = append(pages, append([]byte(nil), p[:]...))
	}
	return pages
}

func (s *Server) applyComposedWrite(ctx context.Context, ops []st25r3916.WritePageOp) (any, error) {
	if s.backend == nil {
		return map[string]any{"sim_mode": true, "model": "NTAG215", "pages_written": len(ops), "verified": true}, nil
	}
	writer, ok := s.backend.(NFCWriter)
	if !ok {
		return nil, errors.New("active backend does not support NTAG writes")
	}
	s.hwMu.Lock()
	defer s.hwMu.Unlock()
	return writer.WriteType2Pages(ctx, ops, st25r3916.WriteOptions{VerifyReadBack: true})
}
