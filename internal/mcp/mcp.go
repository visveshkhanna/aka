// Package mcp is a minimal Model Context Protocol server over stdio: one
// JSON-RPC 2.0 message per line in each direction. It exposes aka's routes
// to AI coding agents as tools, so an agent can find the URL of the app it
// is working on instead of guessing a port.
package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// Versions this server speaks, newest first.
var Versions = []string{"2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05"}

// Tool is one callable tool.
type Tool struct {
	Name        string         `json:"name"`
	Title       string         `json:"title,omitempty"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	// Call runs the tool. A returned error is reported to the model as a
	// tool error (isError), not as a protocol error.
	Call func(args map[string]any) (string, error) `json:"-"`
}

// Server serves tools.
type Server struct {
	Name, Version string
	Instructions  string
	Tools         []Tool

	wmu sync.Mutex
	enc *json.Encoder
}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// JSON-RPC error codes.
const (
	errParse          = -32700
	errInvalidRequest = -32600
	errMethodNotFound = -32601
	errInvalidParams  = -32602
)

// Serve reads requests from r and writes responses to w until r ends.
func (s *Server) Serve(r io.Reader, w io.Writer) error {
	s.enc = json.NewEncoder(w)
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 8<<20)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var req request
		if err := json.Unmarshal(line, &req); err != nil {
			s.reply(response{ID: json.RawMessage("null"), Error: &rpcError{errParse, "parse error: " + err.Error()}})
			continue
		}
		if req.JSONRPC != "2.0" || req.Method == "" {
			if len(req.ID) > 0 {
				s.reply(response{ID: req.ID, Error: &rpcError{errInvalidRequest, "not a JSON-RPC 2.0 request"}})
			}
			continue
		}
		if len(req.ID) == 0 {
			continue // a notification (notifications/initialized, cancelled): no reply
		}
		result, rerr := s.handle(req)
		s.reply(response{ID: req.ID, Result: result, Error: rerr})
	}
	return sc.Err()
}

func (s *Server) reply(r response) {
	r.JSONRPC = "2.0"
	s.wmu.Lock()
	defer s.wmu.Unlock()
	s.enc.Encode(r)
}

func (s *Server) handle(req request) (any, *rpcError) {
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		json.Unmarshal(req.Params, &p)
		version := Versions[0]
		for _, v := range Versions {
			if v == p.ProtocolVersion {
				version = v
			}
		}
		res := map[string]any{
			"protocolVersion": version,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": s.Name, "version": s.Version},
		}
		if s.Instructions != "" {
			res["instructions"] = s.Instructions
		}
		return res, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": s.Tools}, nil
	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.Name == "" {
			return nil, &rpcError{errInvalidParams, "tools/call needs a tool name"}
		}
		for _, t := range s.Tools {
			if t.Name != p.Name {
				continue
			}
			if p.Arguments == nil {
				p.Arguments = map[string]any{}
			}
			if missing := missingArgs(t, p.Arguments); missing != "" {
				return toolResult("missing required argument: "+missing, true), nil
			}
			text, err := t.Call(p.Arguments)
			if err != nil {
				return toolResult(err.Error(), true), nil
			}
			return toolResult(text, false), nil
		}
		return nil, &rpcError{errInvalidParams, fmt.Sprintf("unknown tool %q", p.Name)}
	}
	return nil, &rpcError{errMethodNotFound, "method not found: " + req.Method}
}

func toolResult(text string, isError bool) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
		"isError": isError,
	}
}

func missingArgs(t Tool, args map[string]any) string {
	req, _ := t.InputSchema["required"].([]string)
	for _, k := range req {
		if _, ok := args[k]; !ok {
			return k
		}
	}
	return ""
}

// Object builds an input schema for an object with the given properties.
func Object(required []string, props map[string]any) map[string]any {
	if required == nil {
		required = []string{}
	}
	return map[string]any{"type": "object", "properties": props, "required": required}
}
