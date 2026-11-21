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
)

const (
	protocolVersion = "2024-11-05"
	serverName      = "nfc-studio"
	serverVersion   = "0.1.0"

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
	extraDirs       []string
	settings        config.Settings
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
	return &Server{
		backend:         backend,
		tagsDir:         tagsDir,
		extraDirs:       extra,
		settings:        settings,
		emuPollInterval: defaultEmuPollInterval,
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
		return rpcOK(id, map[string]any{"tools": toolDefinitions()})
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
