package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/visveshkhanna/aka/internal/brand"
	"github.com/visveshkhanna/aka/internal/daemon"
	"github.com/visveshkhanna/aka/internal/mcp"
	"github.com/visveshkhanna/aka/internal/proxy"
)

// cmdMCP serves aka's tools to an MCP client over stdio, e.g.
// `claude mcp add aka -- aka mcp`.
func cmdMCP() int {
	if err := mcpServer(brand.StateDir()).Serve(os.Stdin, os.Stdout); err != nil {
		return fail(err)
	}
	return 0
}

func mcpServer(dir string) *mcp.Server {
	str := map[string]any{"type": "string"}
	return &mcp.Server{
		Name:    brand.Name,
		Version: Version,
		Instructions: "Local dev servers run by " + brand.Name + " have stable URLs such as https://myapp." + brand.DefaultTLD +
			". Use list_apps or get_url to find the URL of an app instead of guessing a localhost port.",
		Tools: []mcp.Tool{
			{
				Name:        "list_apps",
				Title:       "List local apps",
				Description: "List the local apps and aliases routed by " + brand.Name + ", with their URLs, upstream ports and commands.",
				InputSchema: mcp.Object(nil, map[string]any{}),
				Call: func(map[string]any) (string, error) {
					list, ping, err := mcpRoutes(dir)
					if err != nil {
						return "", err
					}
					if len(list) == 0 {
						return "No apps are running.", nil
					}
					b, _ := json.MarshalIndent(toRouteJSON(list, ping), "", "  ")
					return string(b), nil
				},
			},
			{
				Name:        "get_url",
				Title:       "Get an app's URL",
				Description: "Return the URL of a running app or alias by name (for example \"api\" or \"api." + brand.DefaultTLD + "\").",
				InputSchema: mcp.Object([]string{"name"}, map[string]any{"name": str}),
				Call: func(a map[string]any) (string, error) {
					name, _ := a["name"].(string)
					host := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(name)), ".")
					if !strings.HasSuffix(host, "."+brand.DefaultTLD) {
						host += "." + brand.DefaultTLD
					}
					list, ping, err := mcpRoutes(dir)
					if err != nil {
						return "", err
					}
					var names []string
					for _, r := range list {
						if r.Hostname == host {
							if r.Port == 0 {
								return ping.URL(host) + " (the app is starting and has not opened a port yet)", nil
							}
							return ping.URL(host), nil
						}
						names = append(names, r.Hostname)
					}
					if len(names) == 0 {
						return "", fmt.Errorf("no app named %s is running, and no apps are running", host)
					}
					return "", fmt.Errorf("no app named %s is running (running: %s)", host, strings.Join(names, ", "))
				},
			},
			{
				Name:        "add_alias",
				Title:       "Add an alias",
				Description: "Give a local service that " + brand.Name + " did not start (a container, a server run elsewhere) a URL: name -> localhost port.",
				InputSchema: mcp.Object([]string{"name", "port"}, map[string]any{"name": str, "port": map[string]any{"type": "integer", "minimum": 1, "maximum": 65535}}),
				Call: func(a map[string]any) (string, error) {
					name, err := ValidateName(fmt.Sprint(a["name"]))
					if err != nil {
						return "", err
					}
					port, ok := a["port"].(float64)
					if !ok || port < 1 || port > 65535 || port != float64(int(port)) {
						return "", errors.New("port must be an integer from 1 to 65535")
					}
					so, err := startOptions()
					if err != nil {
						return "", err
					}
					so.Warn = func(string) {} // stdout is the protocol stream
					ping, err := daemon.Ensure(so)
					if err != nil {
						return "", err
					}
					r, err := daemon.Do(dir, daemon.Request{Op: daemon.OpAlias, Hostname: Hostname(name), Port: int(port)})
					if err != nil {
						return "", err
					}
					if !r.OK {
						return "", errors.New(r.Error)
					}
					return fmt.Sprintf("%s -> localhost:%d", ping.URL(Hostname(name)), int(port)), nil
				},
			},
			{
				Name:        "remove_alias",
				Title:       "Remove an alias",
				Description: "Remove an alias created with add_alias.",
				InputSchema: mcp.Object([]string{"name"}, map[string]any{"name": str}),
				Call: func(a map[string]any) (string, error) {
					name, err := ValidateName(fmt.Sprint(a["name"]))
					if err != nil {
						return "", err
					}
					r, err := daemon.Do(dir, daemon.Request{Op: daemon.OpAlias, Hostname: Hostname(name), Remove: true})
					if err != nil {
						return "", errors.New("the proxy is not running")
					}
					if !r.OK {
						return "", errors.New(r.Error)
					}
					return "Removed " + Hostname(name) + ".", nil
				},
			},
			{
				Name:        "doctor",
				Title:       "Diagnose the setup",
				Description: "Check " + brand.Name + "'s proxy, certificates and running apps, including apps that reject the proxied host, and explain how to fix problems.",
				InputSchema: mcp.Object(nil, map[string]any{"directory": map[string]any{"type": "string", "description": "project directory to check (default: the server's working directory)"}}),
				Call: func(a map[string]any) (string, error) {
					cwd, _ := os.Getwd()
					if d, ok := a["directory"].(string); ok && d != "" {
						cwd = d
					}
					var b strings.Builder
					doctor(&b, dir, cwd)
					return b.String(), nil
				},
			},
		},
	}
}

func mcpRoutes(dir string) ([]proxy.Route, daemon.Response, error) {
	r, err := daemon.Do(dir, daemon.Request{Op: daemon.OpList})
	if err != nil {
		return nil, daemon.Response{}, errors.New("the " + brand.Name + " proxy is not running, so no apps are routed (start one with `" + brand.Name + " <name> <command>`)")
	}
	ping, _ := daemon.Ping(dir)
	return r.Routes, ping, nil
}
