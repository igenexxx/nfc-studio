package library

import (
	"cmp"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"nfc-studio/internal/nfcfile"
)

// SourceKind distinguishes read-write user tags from read-only external .nfc databases.
type SourceKind string

const (
	// SourceUser is the app-owned read-write tags directory (~/nfc-studio/tags).
	SourceUser SourceKind = "user"
	// SourceExternal is an external .nfc database pack (~/nfc-studio/databases/<pack> or ~/zerosub/nfc).
	SourceExternal SourceKind = "external"

	// PackIDUser is the canonical pack ID for the User Library.
	PackIDUser = "user"
	// PackIDZeroSub is the canonical pack ID for legacy ~/zerosub/nfc.
	PackIDZeroSub = "ext:zerosub"
	// PackIDDBRoot is the pack ID for .nfc files directly inside ~/nfc-studio/databases/.
	PackIDDBRoot = "db:root"
)

var (
	// ErrReadOnlySource is returned when mutating a read-only external source.
	ErrReadOnlySource = errors.New("library: external database is read-only")
	// ErrInvalidPath is returned on path traversal or out-of-sandbox access.
	ErrInvalidPath = errors.New("library: invalid path outside sandbox")
	// ErrPackNotFound is returned when a requested pack ID does not exist.
	ErrPackNotFound = errors.New("library: pack not found")
	// ErrEntryNotFound is returned when a requested .nfc entry does not exist.
	ErrEntryNotFound = errors.New("library: entry not found")
)

// PackInfo describes a .nfc catalog (User Library or external DatabasePack).
type PackInfo struct {
	ID       string     `json:"pack_id"`
	Name     string     `json:"name"`
	Source   SourceKind `json:"source"`
	Dir      string     `json:"dir"`
	ReadOnly bool       `json:"read_only"`
	Count    int        `json:"count"`
}

// EntryRef is a lightweight reference to a .nfc file; Tag is loaded lazily via LoadTag.
type EntryRef struct {
	Name     string
	Path     string
	PackID   string
	Source   SourceKind
	ReadOnly bool
	Tag      *nfcfile.NfcTag
}

// Store manages separated read-write UserLibrary and external DatabasePacks for .nfc files.
type Store struct {
	UserDir      string
	DatabasesDir string
	ExtraDirs    []string
}

// NewStore creates a Store with explicit root directories.
func NewStore(userDir, databasesDir string, extraDirs []string) *Store {
	return &Store{
		UserDir:      userDir,
		DatabasesDir: databasesDir,
		ExtraDirs:    extraDirs,
	}
}

// DefaultDatabasesDir returns ~/nfc-studio/databases (or a /tmp fallback).
func DefaultDatabasesDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "/tmp/nfc-studio/databases"
	}
	return filepath.Join(home, "nfc-studio", "databases")
}

// ListPacks returns all discovered external DatabasePacks followed by the User Library.
func (s *Store) ListPacks() []PackInfo {
	var external []PackInfo
	if s.DatabasesDir != "" {
		external = append(external, scanDatabasesRoot(s.DatabasesDir)...)
	}
	for _, extra := range s.ExtraDirs {
		if extra == "" || extra == s.UserDir || extra == s.DatabasesDir {
			continue
		}
		if cnt := countNfcFiles(extra); cnt > 0 {
			external = append(external, PackInfo{
				ID:       PackIDZeroSub,
				Name:     "zerosub",
				Source:   SourceExternal,
				Dir:      extra,
				ReadOnly: true,
				Count:    cnt,
			})
		}
	}
	slices.SortFunc(external, func(a, b PackInfo) int { return cmp.Compare(a.ID, b.ID) })

	userPack := PackInfo{
		ID:       PackIDUser,
		Name:     "USER",
		Source:   SourceUser,
		Dir:      s.UserDir,
		ReadOnly: false,
		Count:    countNfcFiles(s.UserDir),
	}
	return append(external, userPack)
}

func scanDatabasesRoot(root string) []PackInfo {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var packs []PackInfo
	rootCount := 0
	for _, e := range entries {
		if e.IsDir() {
			subDir := filepath.Join(root, e.Name())
			if cnt := countNfcFiles(subDir); cnt > 0 {
				packs = append(packs, PackInfo{
					ID:       "db:" + e.Name(),
					Name:     e.Name(),
					Source:   SourceExternal,
					Dir:      subDir,
					ReadOnly: true,
					Count:    cnt,
				})
			}
		} else if isNfcFilename(e.Name()) {
			rootCount++
		}
	}
	if rootCount > 0 {
		packs = append(packs, PackInfo{
			ID:       PackIDDBRoot,
			Name:     "databases",
			Source:   SourceExternal,
			Dir:      root,
			ReadOnly: true,
			Count:    rootCount,
		})
	}
	return packs
}

// FindPack looks up a pack by its canonical ID.
func (s *Store) FindPack(packID string) (PackInfo, error) {
	for _, p := range s.ListPacks() {
		if p.ID == packID {
			return p, nil
		}
	}
	return PackInfo{}, fmt.Errorf("%w: %q", ErrPackNotFound, packID)
}

// ListEntries returns lightweight EntryRefs for packID without parsing .nfc bodies.
func (s *Store) ListEntries(packID string) ([]EntryRef, error) {
	pack, err := s.FindPack(packID)
	if err != nil {
		return nil, err
	}
	return readPackEntries(pack), nil
}

// ListUserEntries returns parsed EntryRefs strictly from ~/nfc-studio/tags sorted newest-name first.
func (s *Store) ListUserEntries() []EntryRef {
	pack := PackInfo{ID: PackIDUser, Name: "USER", Source: SourceUser, Dir: s.UserDir}
	raw := readPackEntries(pack)
	parsed := make([]EntryRef, 0, len(raw))
	for _, ref := range raw {
		if tag, err := LoadTag(ref); err == nil {
			ref.Tag = tag
			parsed = append(parsed, ref)
		}
	}
	slices.SortFunc(parsed, func(a, b EntryRef) int { return cmp.Compare(b.Name, a.Name) })
	return parsed
}

func readPackEntries(pack PackInfo) []EntryRef {
	if pack.Dir == "" {
		return nil
	}
	dirEntries, err := os.ReadDir(pack.Dir)
	if err != nil {
		return nil
	}
	out := make([]EntryRef, 0, len(dirEntries))
	for _, e := range dirEntries {
		if e.IsDir() || !isNfcFilename(e.Name()) {
			continue
		}
		out = append(out, EntryRef{
			Name:     e.Name(),
			Path:     filepath.Join(pack.Dir, e.Name()),
			PackID:   pack.ID,
			Source:   pack.Source,
			ReadOnly: pack.ReadOnly,
		})
	}
	slices.SortFunc(out, func(a, b EntryRef) int { return cmp.Compare(a.Name, b.Name) })
	return out
}

// LoadTag parses the .nfc file referenced by ref (returning cached Tag if already loaded).
func LoadTag(ref EntryRef) (*nfcfile.NfcTag, error) {
	if ref.Tag != nil {
		return ref.Tag, nil
	}
	raw, err := os.ReadFile(ref.Path)
	if err != nil {
		return nil, fmt.Errorf("library: read %s: %w", ref.Name, err)
	}
	tag, err := nfcfile.Parse(string(raw))
	if err != nil {
		return nil, fmt.Errorf("library: parse %s: %w", ref.Name, err)
	}
	return tag, nil
}

// PromoteToUser copies a .nfc entry from an external DatabasePack into UserDir with collision-safe naming.
func (s *Store) PromoteToUser(ref EntryRef) (EntryRef, error) {
	if s.UserDir == "" {
		return EntryRef{}, errors.New("library: user directory is not configured")
	}
	cleanName, err := SanitizeNfcName(ref.Name)
	if err != nil {
		return EntryRef{}, err
	}
	raw, err := os.ReadFile(ref.Path)
	if err != nil {
		return EntryRef{}, fmt.Errorf("library: read source %s: %w", ref.Name, err)
	}
	tag, err := nfcfile.Parse(string(raw))
	if err != nil {
		return EntryRef{}, fmt.Errorf("library: invalid .nfc %s: %w", ref.Name, err)
	}
	if err := os.MkdirAll(s.UserDir, 0o755); err != nil {
		return EntryRef{}, fmt.Errorf("library: mkdir user dir: %w", err)
	}
	dstName, dstPath := resolveUniqueUserPath(s.UserDir, cleanName)
	if err := os.WriteFile(dstPath, raw, 0o644); err != nil {
		return EntryRef{}, fmt.Errorf("library: write %s: %w", dstName, err)
	}
	return EntryRef{Name: dstName, Path: dstPath, PackID: PackIDUser, Source: SourceUser, Tag: tag}, nil
}

// DeleteUserEntry removes a .nfc file strictly inside UserDir.
func (s *Store) DeleteUserEntry(ref EntryRef) error {
	if ref.ReadOnly || ref.Source != SourceUser {
		return ErrReadOnlySource
	}
	if s.UserDir == "" {
		return errors.New("library: user directory is not configured")
	}
	cleanName, err := SanitizeNfcName(ref.Name)
	if err != nil {
		return err
	}
	absBase, err := filepath.Abs(s.UserDir)
	if err != nil {
		return fmt.Errorf("library: resolve user dir: %w", err)
	}
	target := filepath.Join(absBase, cleanName)
	rel, err := filepath.Rel(absBase, target)
	if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return ErrInvalidPath
	}
	if err := os.Remove(target); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%w: %s", ErrEntryNotFound, cleanName)
		}
		return fmt.Errorf("library: delete %s: %w", cleanName, err)
	}
	return nil
}

// DeleteDatabaseEntry removes a .nfc file strictly inside DatabasesDir (~/nfc-studio/databases/<pack>/).
// Legacy ~/zerosub/nfc (ext:zerosub) and UserDir are rejected.
func (s *Store) DeleteDatabaseEntry(ref EntryRef) error {
	if ref.Source != SourceExternal || ref.PackID == PackIDZeroSub || s.DatabasesDir == "" {
		return ErrReadOnlySource
	}
	if _, err := SanitizeNfcName(ref.Name); err != nil {
		return err
	}
	absBase, err := filepath.Abs(s.DatabasesDir)
	if err != nil {
		return fmt.Errorf("library: resolve databases dir: %w", err)
	}
	absTarget, err := filepath.Abs(ref.Path)
	if err != nil {
		return fmt.Errorf("library: resolve entry path: %w", err)
	}
	rel, err := filepath.Rel(absBase, absTarget)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return ErrInvalidPath
	}
	if err := os.Remove(absTarget); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%w: %s", ErrEntryNotFound, ref.Name)
		}
		return fmt.Errorf("library: delete %s: %w", ref.Name, err)
	}
	return nil
}

// SanitizeNfcName rejects path traversal, separators, and non-.nfc filenames.
func SanitizeNfcName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || name == ".nfc" {
		return "", fmt.Errorf("%w: empty filename", ErrInvalidPath)
	}
	if strings.Contains(name, "..") || strings.ContainsAny(name, `/\`) || strings.ContainsRune(name, 0) {
		return "", fmt.Errorf("%w: %q", ErrInvalidPath, name)
	}
	if !isNfcFilename(name) {
		return "", fmt.Errorf("%w: must end with .nfc", ErrInvalidPath)
	}
	return name, nil
}

func resolveUniqueUserPath(userDir, name string) (string, string) {
	base := strings.TrimSuffix(name, filepath.Ext(name))
	ext := filepath.Ext(name)
	candidate := name
	full := filepath.Join(userDir, candidate)
	for i := 2; i < 1000; i++ {
		if _, err := os.Stat(full); errors.Is(err, os.ErrNotExist) {
			return candidate, full
		}
		candidate = fmt.Sprintf("%s_%d%s", base, i, ext)
		full = filepath.Join(userDir, candidate)
	}
	return candidate, full
}

func countNfcFiles(dir string) int {
	if dir == "" {
		return 0
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() && isNfcFilename(e.Name()) {
			n++
		}
	}
	return n
}

func isNfcFilename(name string) bool {
	return strings.HasSuffix(strings.ToLower(name), ".nfc")
}
