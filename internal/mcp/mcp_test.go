package mcp

import (
	"bufio"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func session(t *testing.T, s *Server, lines ...string) []map[string]any {
	t.Helper()
	var out strings.Builder
	if err := s.Serve(strings.NewReader(strings.Join(lines, "\n")+"\n"), &out); err != nil {
		t.Fatal(err)
	}
	var res []map[string]any
	sc := bufio.NewScanner(strings.NewReader(out.String()))
	for sc.Scan() {
		var m map[string]any
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			t.Fatalf("bad output line %q: %v", sc.Text(), err)
		}
		res = append(res, m)
	}
	return res
}

func testServer() *Server {
	return &Server{Name: "aka", Version: "test", Instructions: "use me", Tools: []Tool{
		{Name: "echo", Description: "echo", InputSchema: Object([]string{"text"}, map[string]any{"text": map[string]any{"type": "string"}}),
			Call: func(a map[string]any) (string, error) { return "you said " + a["text"].(string), nil }},
		{Name: "boom", Description: "fails", InputSchema: Object(nil, nil),
			Call: func(map[string]any) (string, error) { return "", errors.New("no proxy") }},
	}}
}

func TestProtocol(t *testing.T) {
	res := session(t, testServer(),
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":"three","method":"tools/call","params":{"name":"echo","arguments":{"text":"hi"}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"boom"}}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"echo","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"nope"}}`,
		`{"jsonrpc":"2.0","id":7,"method":"resources/list"}`,
		`{"jsonrpc":"2.0","id":8,"method":"ping"}`,
		`not json`,
		`{"jsonrpc":"2.0","id":9,"method":"initialize","params":{"protocolVersion":"1999-01-01"}}`,
	)
	if len(res) != 10 { // the notification gets no reply
		t.Fatalf("%d replies: %v", len(res), res)
	}
	init := res[0]["result"].(map[string]any)
	if init["protocolVersion"] != "2025-06-18" || init["instructions"] != "use me" {
		t.Errorf("initialize: %v", init)
	}
	if init["capabilities"].(map[string]any)["tools"] == nil {
		t.Error("tools capability missing")
	}
	tools := res[1]["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 2 || tools[0].(map[string]any)["inputSchema"] == nil {
		t.Errorf("tools/list: %v", tools)
	}
	text := func(i int) (string, bool) {
		r := res[i]["result"].(map[string]any)
		return r["content"].([]any)[0].(map[string]any)["text"].(string), r["isError"].(bool)
	}
	if s, isErr := text(2); s != "you said hi" || isErr || res[2]["id"] != "three" {
		t.Errorf("echo: %q %v id %v", s, isErr, res[2]["id"])
	}
	if s, isErr := text(3); s != "no proxy" || !isErr {
		t.Errorf("boom: %q %v", s, isErr)
	}
	if s, isErr := text(4); !strings.Contains(s, "text") || !isErr {
		t.Errorf("missing arg: %q %v", s, isErr)
	}
	code := func(i int) float64 { return res[i]["error"].(map[string]any)["code"].(float64) }
	if code(5) != errInvalidParams || code(6) != errMethodNotFound || code(8) != errParse {
		t.Errorf("error codes: %v %v %v", res[5], res[6], res[8])
	}
	if res[7]["result"] == nil {
		t.Errorf("ping: %v", res[7])
	}
	if v := res[9]["result"].(map[string]any)["protocolVersion"]; v != Versions[0] {
		t.Errorf("unknown version answered with %v, want %s", v, Versions[0])
	}
}
