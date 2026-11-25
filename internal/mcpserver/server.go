package mcpserver

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"nfc-studio/internal/config"
	"nfc-studio/internal/dbsync"
	"nfc-studio/internal/library"
	"nfc-studio/internal/nfcml"
)

const (
	protocolVersion = "2024-11-05"
	serverName      = "nfc-studio"
	serverVersion   = "0.4.1"

	errCodeParseError     = -32700
	errCodeMethodNotFound = -32601
	errCodeInvalidParams  = -32602

	defaultEmuPollInterval = 20 * time.Millisecond
	stdioInitBufSize       = 64 * 1024
	stdioMaxBufSize        = 1024 * 1024
)

// Server implements a pure-Go MCP JSON-RPC 2.0 server over stdio.
type Server struct {
	backend         NFCBackend
	tagsDir         string
	databasesDir    string
	sourcesPath     string
	extraDirs       []string
	store           *library.Store
	syncer          *dbsync.Syncer
	settings        config.Settings
	inductive       *nfcml.InductiveSensor
	hwMu            sync.Mutex
	writeMu         sync.Mutex
	emuPollInterval time.Duration
}

// New creates a new MCP Server. backend may be nil to operate in simulator mode.
func New(backend NFCBackend, tagsDir string, settings config.Settings) *Server {
	var extra []string
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		extra = append(extra, filepath.Join(home, "zerosub", "nfc"))
	}
	dbDir := library.DefaultDatabasesDir()
	return &Server{
		backend:         backend,
		tagsDir:         tagsDir,
		databasesDir:    dbDir,
		sourcesPath:     dbsync.DefaultSourcesPath(),
		extraDirs:       extra,
		store:           library.NewStore(tagsDir, dbDir, extra),
		syncer:          dbsync.NewSyncer(dbDir),
		settings:        settings,
		inductive:       nfcml.NewInductiveSensor(),
		emuPollInterval: defaultEmuPollInterval,
	}
}

// SetDatabaseConfig overrides databasesDir, sourcesPath, and optional Syncer (for runner/tests).
func (s *Server) SetDatabaseConfig(dbDir, sourcesPath string, syncer *dbsync.Syncer) {
	if dbDir != "" {
		s.databasesDir = dbDir
	}
	if sourcesPath != "" {
		s.sourcesPath = sourcesPath
	}
	s.store = library.NewStore(s.tagsDir, s.databasesDir, s.extraDirs)
	if syncer != nil {
		s.syncer = syncer
	} else {
		s.syncer = dbsync.NewSyncer(s.databasesDir)
	}
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type toolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type toolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

type toolContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type toolResult struct {
	Content []toolContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

type ndefDTO struct {
	Type    string `json:"type"`
	Payload string `json:"payload"`
}

// ServeStdio reads newline-delimited JSON-RPC 2.0 messages from r and writes responses to w.
func (s *Server) ServeStdio(ctx context.Context, r io.Reader, w io.Writer) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, stdioInitBufSize), stdioMaxBufSize)

	for scanner.Scan() {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		resp := s.handleRequest(ctx, []byte(line))
		if resp == nil {
			continue
		}
		if err := s.writeResponse(w, resp); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func (s *Server) writeResponse(w io.Writer, resp *rpcResponse) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	raw, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	_, err = w.Write(raw)
	return err
}

func (s *Server) handleRequest(ctx context.Context, line []byte) *rpcResponse {
	var req rpcRequest
	if err := json.Unmarshal(line, &req); err != nil {
		return rpcFail(json.RawMessage("null"), errCodeParseError, "Parse error: "+err.Error())
	}
	if req.Method == "notifications/initialized" || (len(req.ID) == 0 && strings.HasPrefix(req.Method, "notifications/")) {
		return nil
	}
	id := req.ID
	if len(id) == 0 {
		id = json.RawMessage("null")
	}

	switch req.Method {
	case "initialize":
		return rpcOK(id, initializeResult())
	case "ping":
		return rpcOK(id, map[string]any{})
	case "tools/list":
		return rpcOK(id, map[string]any{"tools": allToolDefinitions()})
	case "tools/call":
		return s.dispatchTool(ctx, id, req.Params)
	default:
		return rpcFail(id, errCodeMethodNotFound, fmt.Sprintf("Method not found: %s", req.Method))
	}
}

func initializeResult() map[string]any {
	return map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities":    map[string]any{"tools": map[string]any{}},
		"serverInfo": map[string]any{
			"name":    serverName,
			"version": serverVersion,
		},
	}
}

func (s *Server) dispatchTool(ctx context.Context, id json.RawMessage, rawParams json.RawMessage) *rpcResponse {
	if len(rawParams) == 0 {
		return rpcFail(id, errCodeInvalidParams, "Invalid params: missing params object")
	}
	var params toolCallParams
	if err := json.Unmarshal(rawParams, &params); err != nil || params.Name == "" {
		return rpcFail(id, errCodeInvalidParams, "Invalid params: expected {name, arguments}")
	}
	args := params.Arguments
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	res, rpcErr := s.invokeTool(ctx, params.Name, args)
	if rpcErr != nil {
		return &rpcResponse{JSONRPC: "2.0", ID: id, Error: rpcErr}
	}
	return rpcOK(id, res)
}

func (s *Server) invokeTool(ctx context.Context, name string, args json.RawMessage) (toolResult, *rpcError) {
	switch name {
	case "nfc_get_status":
		return s.toolGetStatus(), nil
	case "nfc_scan_tag":
		return s.toolScanTag(ctx, args)
	case "nfc_parse_ndef":
		return s.toolParseNDEF(args)
	case "nfc_emulate_tag":
		return s.toolEmulateTag(ctx, args)
	case "nfc_list_saved":
		return s.toolListSaved(), nil
	case "nfc_read_saved":
		return s.toolReadSaved(args)
	case "nfc_save_tag":
		return s.toolSaveTag(args)
	case "nfc_delete_saved":
		return s.toolDeleteSaved(args)
	case "nfc_list_databases":
		return s.toolListDatabases(), nil
	case "nfc_promote_to_user":
		return s.toolPromoteToUser(args)
	case "nfc_list_sources":
		return s.toolListSources(), nil
	case "nfc_add_source":
		return s.toolAddSource(args)
	case "nfc_remove_source":
		return s.toolRemoveSource(args)
	case "nfc_sync_sources":
		return s.toolSyncSources(args)
	case "nfc_delete_pack_files":
		return s.toolDeletePackFiles(args)
	case "nfc_analyze_tag":
		return s.toolAnalyzeTag(args)
	case "nfc_match_fingerprint":
		return s.toolMatchFingerprint(args)
	case "nfc_measure_field":
		return s.toolMeasureField(), nil
	case "nfc_write_ntag":
		return s.toolWriteNTAG(ctx, args)
	case "nfc_clone_magic_uid":
		return s.toolCloneMagicUID(ctx, args)
	case "nfc_write_saved_tag":
		return s.toolWriteSavedTag(ctx, args)
	case "nfc_probe_blank":
		return s.toolProbeBlank(ctx, args)
	case "nfc_write_mifare_gen1a":
		return s.toolWriteMifareGen1A(ctx, args)
	case "nfc_compose_ndef":
		return s.toolComposeNDEF(ctx, args)
	case "nfc_dict_scan":
		return s.toolDictScan(ctx, args)
	case "nfc_inspect_emv":
		return s.toolInspectEMV(ctx)
	case "nfc_scan_multiprotocol":
		return s.toolScanMultiProtocol(ctx, args)
	case "nfc_write_nfcv":
		return s.toolWriteNFCV(ctx, args)
	case "nfc_inductive_sense":
		return s.toolInductiveSense(args)
	case "nfc_epaper_push":
		return s.toolEPaperPush(ctx, args)
	case "nfc_model_info":
		return s.toolModelInfo(), nil
	default:
		return toolResult{}, &rpcError{
			Code:    errCodeInvalidParams,
			Message: fmt.Sprintf("Unknown tool: %s", name),
		}
	}
}

func rpcOK(id json.RawMessage, result any) *rpcResponse {
	return &rpcResponse{JSONRPC: "2.0", ID: id, Result: result}
}

func rpcFail(id json.RawMessage, code int, msg string) *rpcResponse {
	return &rpcResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &rpcError{Code: code, Message: msg},
	}
}

func okJSON(v any) toolResult {
	b, err := json.Marshal(v)
	if err != nil {
		return errResult("marshal output: " + err.Error())
	}
	return toolResult{
		Content: []toolContent{{Type: "text", Text: string(b)}},
	}
}

func errResult(msg string) toolResult {
	b, _ := json.Marshal(map[string]string{"error": msg})
	return toolResult{
		Content: []toolContent{{Type: "text", Text: string(b)}},
		IsError: true,
	}
}
