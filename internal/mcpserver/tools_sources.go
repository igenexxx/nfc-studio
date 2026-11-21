package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"nfc-studio/internal/dbsync"
	"nfc-studio/internal/library"
)

const mcpSyncTimeout = 60 * time.Second

func (s *Server) toolListDatabases() toolResult {
	packs := s.store.ListPacks()
	return okJSON(map[string]any{
		"count": len(packs),
		"packs": packs,
	})
}

func (s *Server) toolPromoteToUser(raw json.RawMessage) (toolResult, *rpcError) {
	var in struct {
		Pack string `json:"pack"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return toolResult{}, &rpcError{Code: errCodeInvalidParams, Message: "Invalid arguments: " + err.Error()}
	}
	if in.Pack == "" || in.Pack == library.PackIDUser {
		return errResult("pack must specify an external database pack_id"), nil
	}
	ref, err := s.resolvePackEntry(in.Pack, in.Name)
	if err != nil {
		return errResult(err.Error()), nil
	}
	promoted, err := s.store.PromoteToUser(ref)
	if err != nil {
		return errResult(err.Error()), nil
	}
	return okJSON(map[string]any{
		"promoted": true,
		"name":     promoted.Name,
		"path":     promoted.Path,
		"source":   string(promoted.Source),
	}), nil
}

func (s *Server) toolListSources() toolResult {
	list, err := dbsync.LoadSources(s.sourcesPath)
	if err != nil {
		return errResult(err.Error())
	}
	if list == nil {
		list = []dbsync.DatabaseSource{}
	}
	return okJSON(map[string]any{
		"count":   len(list),
		"sources": list,
	})
}

func (s *Server) toolAddSource(raw json.RawMessage) (toolResult, *rpcError) {
	var in struct {
		ID        string `json:"id"`
		Type      string `json:"type"`
		GitHubURL string `json:"github_url"`
		Owner     string `json:"owner"`
		Repo      string `json:"repo"`
		Branch    string `json:"branch"`
		Subpath   string `json:"subpath"`
		URL       string `json:"url"`
		Prune     bool   `json:"prune"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return toolResult{}, &rpcError{Code: errCodeInvalidParams, Message: "Invalid arguments: " + err.Error()}
	}
	src := dbsync.DatabaseSource{
		ID:      in.ID,
		Type:    dbsync.SourceType(strings.ToLower(strings.TrimSpace(in.Type))),
		Owner:   in.Owner,
		Repo:    in.Repo,
		Branch:  in.Branch,
		Subpath: in.Subpath,
		URL:     in.URL,
		Enabled: true,
		Prune:   in.Prune,
	}
	if src.Type == dbsync.SourceGitHub && in.GitHubURL != "" {
		owner, repo, branch, sub, err := dbsync.ParseGitHubURL(in.GitHubURL)
		if err != nil {
			return errResult(err.Error()), nil
		}
		src.Owner, src.Repo, src.Branch, src.Subpath = owner, repo, branch, sub
	}
	saved, err := dbsync.UpsertSource(s.sourcesPath, src)
	if err != nil {
		return errResult(err.Error()), nil
	}
	return okJSON(map[string]any{
		"added":  true,
		"source": saved,
	}), nil
}

func (s *Server) toolRemoveSource(raw json.RawMessage) (toolResult, *rpcError) {
	var in struct {
		ID          string   `json:"id"`
		IDs         []string `json:"ids"`
		Confirm     bool     `json:"confirm"`
		DeleteFiles bool     `json:"delete_files"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return toolResult{}, &rpcError{Code: errCodeInvalidParams, Message: "Invalid arguments: " + err.Error()}
	}
	if !in.Confirm {
		return errResult("removing a database source requires confirm=true"), nil
	}
	targets := in.IDs
	if id := strings.TrimSpace(in.ID); id != "" {
		targets = append([]string{id}, targets...)
	}
	if len(targets) == 0 {
		return errResult("either id or ids must be provided"), nil
	}
	removed := make([]string, 0, len(targets))
	dirsDeleted := 0
	for _, rawID := range targets {
		cleanID := strings.ToLower(strings.TrimSpace(rawID))
		if cleanID == "" {
			continue
		}
		if dbsync.RemoveSource(s.sourcesPath, cleanID) == nil {
			removed = append(removed, cleanID)
		}
		if in.DeleteFiles && s.databasesDir != "" && dbsync.DeletePackDir(s.databasesDir, cleanID) == nil {
			dirsDeleted++
		}
	}
	if len(removed) == 0 && dirsDeleted == 0 {
		return errResult("no matching sources or directories found"), nil
	}
	return okJSON(map[string]any{
		"removed":       true,
		"ids":           removed,
		"deleted_files": dirsDeleted > 0,
		"dirs_deleted":  dirsDeleted,
	}), nil
}

func (s *Server) toolSyncSources(raw json.RawMessage) (toolResult, *rpcError) {
	var in struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return toolResult{}, &rpcError{Code: errCodeInvalidParams, Message: "Invalid arguments: " + err.Error()}
	}
	list, err := dbsync.LoadSources(s.sourcesPath)
	if err != nil {
		return errResult(err.Error()), nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), mcpSyncTimeout)
	defer cancel()

	if id := strings.ToLower(strings.TrimSpace(in.ID)); id != "" {
		for _, src := range list {
			if src.ID == id {
				rep := s.syncer.SyncSource(ctx, src)
				return okJSON(map[string]any{"reports": []dbsync.SyncReport{rep}}), nil
			}
		}
		return errResult(fmt.Sprintf("source %q not found", id)), nil
	}
	reports := s.syncer.SyncAll(ctx, list)
	return okJSON(map[string]any{
		"count":   len(reports),
		"reports": reports,
	}), nil
}

func (s *Server) toolDeletePackFiles(raw json.RawMessage) (toolResult, *rpcError) {
	var in struct {
		Pack         string   `json:"pack"`
		Names        []string `json:"names"`
		IgnoreOnSync *bool    `json:"ignore_on_sync"`
		Confirm      bool     `json:"confirm"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return toolResult{}, &rpcError{Code: errCodeInvalidParams, Message: "Invalid arguments: " + err.Error()}
	}
	if !in.Confirm {
		return errResult("deleting pack files requires confirm=true"), nil
	}
	if len(in.Names) == 0 {
		return errResult("names must not be empty"), nil
	}
	ignoreOnSync := true
	if in.IgnoreOnSync != nil {
		ignoreOnSync = *in.IgnoreOnSync
	}
	deleted := make([]string, 0, len(in.Names))
	var packDir string
	for _, name := range in.Names {
		ref, err := s.resolvePackEntry(in.Pack, name)
		if err != nil {
			continue
		}
		if err := s.store.DeleteDatabaseEntry(ref); err != nil {
			return errResult(err.Error()), nil
		}
		deleted = append(deleted, ref.Name)
		packDir = filepath.Dir(ref.Path)
	}
	if ignoreOnSync && len(deleted) > 0 && packDir != "" {
		_ = dbsync.MarkIgnored(packDir, deleted)
	}
	return okJSON(map[string]any{
		"deleted":         len(deleted),
		"files":           deleted,
		"ignored_on_sync": ignoreOnSync,
	}), nil
}

func (s *Server) resolvePackEntry(packID, name string) (library.EntryRef, error) {
	cleanName, err := library.SanitizeNfcName(name)
	if err != nil {
		return library.EntryRef{}, err
	}
	entries, err := s.store.ListEntries(packID)
	if err != nil {
		return library.EntryRef{}, err
	}
	for _, ref := range entries {
		if ref.Name == cleanName {
			return ref, nil
		}
	}
	return library.EntryRef{}, fmt.Errorf("entry %s not found in pack %s", cleanName, packID)
}
