package mcpserver

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"nfc-studio/internal/nfcfile"
)

type savedTagSummary struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	DeviceType string `json:"device_type"`
	UIDHex     string `json:"uid_hex"`
	PagesCount int    `json:"pages_count"`
}

func (s *Server) savedSearchDirs() []string {
	dirs := make([]string, 0, 1+len(s.extraDirs))
	seen := make(map[string]bool)
	add := func(d string) {
		if d == "" {
			return
		}
		clean := filepath.Clean(d)
		if !seen[clean] {
			seen[clean] = true
			dirs = append(dirs, clean)
		}
	}
	add(s.tagsDir)
	for _, d := range s.extraDirs {
		add(d)
	}
	return dirs
}

func (s *Server) toolListSaved() toolResult {
	items := make([]savedTagSummary, 0)
	for _, dir := range s.savedSearchDirs() {
		items = append(items, scanSavedDir(dir)...)
	}
	slices.SortFunc(items, func(a, b savedTagSummary) int {
		return cmp.Compare(b.Name, a.Name)
	})
	return okJSON(map[string]any{
		"count": len(items),
		"files": items,
	})
}

func scanSavedDir(dir string) []savedTagSummary {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []savedTagSummary
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".nfc") {
			continue
		}
		full := filepath.Join(dir, e.Name())
		raw, err := os.ReadFile(full)
		if err != nil {
			continue
		}
		tag, err := nfcfile.Parse(string(raw))
		if err != nil {
			continue
		}
		out = append(out, savedTagSummary{
			Name:       e.Name(),
			Path:       full,
			DeviceType: tag.DeviceType,
			UIDHex:     nfcfile.FormatHex(tag.UID),
			PagesCount: len(tag.Pages),
		})
	}
	return out
}

func (s *Server) toolReadSaved(raw json.RawMessage) (toolResult, *rpcError) {
	var in struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return toolResult{}, &rpcError{Code: errCodeInvalidParams, Message: "Invalid arguments: " + err.Error()}
	}
	safeName, err := sanitizeFilename(in.Name)
	if err != nil {
		return errResult(err.Error()), nil
	}
	foundPath, content, ok := s.findSavedTagFile(safeName)
	if !ok {
		return errResult(fmt.Sprintf("saved tag %q not found", safeName)), nil
	}
	tag, err := nfcfile.Parse(string(content))
	if err != nil {
		return errResult("parse .nfc failed: " + err.Error()), nil
	}
	return okJSON(map[string]any{
		"name":        safeName,
		"path":        foundPath,
		"device_type": tag.DeviceType,
		"uid_hex":     nfcfile.FormatHex(tag.UID),
		"atqa":        fmt.Sprintf("%02X %02X", tag.ATQA[0], tag.ATQA[1]),
		"sak":         fmt.Sprintf("%02X", tag.SAK),
		"pages_hex":   pagesToHex(tag.Pages),
		"ndef":        extractNdefDTO(tag.Pages),
	}), nil
}

func (s *Server) findSavedTagFile(safeName string) (string, []byte, bool) {
	for _, dir := range s.savedSearchDirs() {
		candidate := filepath.Join(dir, safeName)
		if data, err := os.ReadFile(candidate); err == nil {
			return candidate, data, true
		}
	}
	return "", nil, false
}

func (s *Server) toolSaveTag(raw json.RawMessage) (toolResult, *rpcError) {
	var in struct {
		Name       string   `json:"name"`
		DeviceType string   `json:"device_type"`
		UIDHex     string   `json:"uid_hex"`
		ATQAHex    string   `json:"atqa_hex"`
		SAKHex     string   `json:"sak_hex"`
		PagesHex   []string `json:"pages_hex"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return toolResult{}, &rpcError{Code: errCodeInvalidParams, Message: "Invalid arguments: " + err.Error()}
	}
	safeName, err := sanitizeFilename(in.Name)
	if err != nil {
		return errResult(err.Error()), nil
	}
	if !strings.HasSuffix(strings.ToLower(safeName), ".nfc") {
		safeName += ".nfc"
	}
	uid, atqa, sak, err := parseTagIdent(in.UIDHex, in.ATQAHex, in.SAKHex, false)
	if err != nil {
		return errResult(err.Error()), nil
	}
	var pages [][]byte
	if len(in.PagesHex) > 0 {
		pages, err = parsePagesHex(in.PagesHex)
		if err != nil {
			return errResult(err.Error()), nil
		}
	}
	return s.writeSavedTag(safeName, in.DeviceType, uid, atqa, sak, pages), nil
}

func (s *Server) writeSavedTag(safeName, deviceType string, uid []byte, atqa [2]byte, sak uint8, pages [][]byte) toolResult {
	dt := strings.TrimSpace(deviceType)
	if dt == "" {
		dt = nfcfile.IdentifyCard(atqa, sak, len(uid))
	}
	tag := &nfcfile.NfcTag{
		FileType:   "Flipper NFC device",
		Version:    4,
		DeviceType: dt,
		UID:        uid,
		ATQA:       atqa,
		SAK:        sak,
		PagesTotal: len(pages),
		Pages:      pages,
	}
	if s.tagsDir == "" {
		return errResult("tags directory is not configured")
	}
	if err := os.MkdirAll(s.tagsDir, 0o755); err != nil {
		return errResult("create tags dir failed: " + err.Error())
	}
	dest := filepath.Join(s.tagsDir, safeName)
	if err := os.WriteFile(dest, []byte(tag.Format()), 0o644); err != nil {
		return errResult("write .nfc failed: " + err.Error())
	}
	return okJSON(map[string]any{
		"saved":       true,
		"name":        safeName,
		"path":        dest,
		"device_type": dt,
		"uid_hex":     nfcfile.FormatHex(uid),
		"pages_count": len(pages),
	})
}

func (s *Server) toolDeleteSaved(raw json.RawMessage) (toolResult, *rpcError) {
	var in struct {
		Name    string `json:"name"`
		Confirm bool   `json:"confirm"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return toolResult{}, &rpcError{Code: errCodeInvalidParams, Message: "Invalid arguments: " + err.Error()}
	}
	if !in.Confirm {
		return errResult("confirm must be true to delete a saved .nfc file"), nil
	}
	safeName, err := sanitizeFilename(in.Name)
	if err != nil {
		return errResult(err.Error()), nil
	}
	if s.tagsDir == "" {
		return errResult("tags directory is not configured"), nil
	}

	cleanDir := filepath.Clean(s.tagsDir)
	target := filepath.Join(cleanDir, safeName)
	if filepath.Dir(target) != cleanDir {
		return errResult("invalid filename: path escapes tags directory"), nil
	}
	if err := os.Remove(target); err != nil {
		return errResult("delete failed: " + err.Error()), nil
	}
	return okJSON(map[string]any{
		"deleted": true,
		"name":    safeName,
		"path":    target,
	}), nil
}

func sanitizeFilename(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("name is required")
	}
	if strings.Contains(name, "..") || strings.ContainsAny(name, `/\`) || strings.ContainsRune(name, 0) {
		return "", fmt.Errorf("invalid filename %q: path traversal not allowed", name)
	}
	if name == "." {
		return "", fmt.Errorf("invalid filename %q", name)
	}
	return name, nil
}
