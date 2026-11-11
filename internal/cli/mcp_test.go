package cli

import (
	"bufio"
	"encoding/json"
	"io"
	"log"
	"os"
	"strings"
	"testing"

	"aka/internal/daemon"
)

func TestMCPTools(t *testing.T) {
	dir, err := os.MkdirTemp("", "akam")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	t.Setenv("AKA_STATE_DIR", dir)
	t.Setenv("AKA_HTTPS", "0")
	s, err := daemon.Start(daemon.Config{StateDir: dir, ProxyHosts: []string{"127.0.0.1"}, Log: log.New(io.Discard, "", 0)})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	c, _ := daemon.Dial(dir)
	defer c.Close()
	c.Call(daemon.Request{Op: daemon.OpRegister, Hostname: "api.localhost", PID: os.Getpid(), Command: "uvicorn main:app"})
	c.Call(daemon.Request{Op: daemon.OpUpdate, Port: 4321, DialHost: "127.0.0.1"})

	call := func(name, args string) (string, bool) {
		in := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"` + name + `","arguments":` + args + `}}` + "\n"
		var out strings.Builder
		mcpServer(dir).Serve(strings.NewReader(in), &out)
		var resp struct {
			Result struct {
				Content []struct{ Text string } `json:"content"`
				IsError bool                     `json:"isError"`
			} `json:"result"`
		}
		sc := bufio.NewScanner(strings.NewReader(out.String()))
		sc.Scan()
		if err := json.Unmarshal(sc.Bytes(), &resp); err != nil || len(resp.Result.Content) == 0 {
			t.Fatalf("%s: bad reply %q", name, out.String())
		}
		return resp.Result.Content[0].Text, resp.Result.IsError
	}
	port := ":" + itoa(s.ProxyPort())
	if text, isErr := call("list_apps", "{}"); isErr || !strings.Contains(text, `"url": "http://api.localhost`+port+`"`) || !strings.Contains(text, `"port": 4321`) {
		t.Errorf("list_apps: %v %s", isErr, text)
	}
	if text, isErr := call("get_url", `{"name":"API"}`); isErr || text != "http://api.localhost"+port {
		t.Errorf("get_url: %v %q", isErr, text)
	}
	if text, isErr := call("get_url", `{"name":"web"}`); !isErr || !strings.Contains(text, "running: api.localhost") {
		t.Errorf("get_url missing: %v %q", isErr, text)
	}
	if text, isErr := call("add_alias", `{"name":"db","port":5432}`); isErr || text != "http://db.localhost"+port+" -> localhost:5432" {
		t.Errorf("add_alias: %v %q", isErr, text)
	}
	if text, isErr := call("add_alias", `{"name":"db","port":1.5}`); !isErr {
		t.Errorf("fractional port accepted: %q", text)
	}
	if text, isErr := call("remove_alias", `{"name":"db"}`); isErr || text != "Removed db.localhost." {
		t.Errorf("remove_alias: %v %q", isErr, text)
	}
	if text, isErr := call("doctor", `{}`); isErr || !strings.Contains(text, "doctor") || !strings.Contains(text, "api.localhost") {
		t.Errorf("doctor: %v %q", isErr, text)
	}
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }
