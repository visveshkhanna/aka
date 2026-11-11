// Package compose reads the services Docker Compose publishes, so aka can
// route <service>.<project>.localhost to them. Container ports belong to
// Docker, not to a process aka started, so listener detection cannot see
// them; `docker compose ps` can.
package compose

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net"
	"sort"
	"strings"

	"aka/internal/project"
)

// Service is a running service and the host ports it publishes.
type Service struct {
	Project string
	Service string
	Ports   []int // published TCP ports reachable on loopback, in order
}

// Hostname is <service>.<project>.<tld>.
func (s Service) Hostname(tld string) string {
	return project.SanitizeLabel(s.Service) + "." + project.SanitizeLabel(s.Project) + "." + tld
}

type psEntry struct {
	Project    string `json:"Project"`
	Service    string `json:"Service"`
	State      string `json:"State"`
	Publishers []struct {
		URL           string `json:"URL"`
		TargetPort    int    `json:"TargetPort"`
		PublishedPort int    `json:"PublishedPort"`
		Protocol      string `json:"Protocol"`
	} `json:"Publishers"`
}

// ParsePS decodes `docker compose ps --format json`, which is a JSON array
// in older Compose versions and one JSON object per line in newer ones.
// Several containers of one service (replicas) are merged.
func ParsePS(out []byte) ([]Service, error) {
	var entries []psEntry
	trimmed := bytes.TrimSpace(out)
	if len(trimmed) == 0 {
		return nil, nil
	}
	if trimmed[0] == '[' {
		if err := json.Unmarshal(trimmed, &entries); err != nil {
			return nil, err
		}
	} else {
		sc := bufio.NewScanner(bytes.NewReader(trimmed))
		sc.Buffer(make([]byte, 1<<20), 16<<20)
		for sc.Scan() {
			line := bytes.TrimSpace(sc.Bytes())
			if len(line) == 0 {
				continue
			}
			var e psEntry
			if err := json.Unmarshal(line, &e); err != nil {
				return nil, err
			}
			entries = append(entries, e)
		}
		if err := sc.Err(); err != nil {
			return nil, err
		}
	}

	byKey := map[string]*Service{}
	var order []string
	for _, e := range entries {
		if e.State != "" && e.State != "running" {
			continue
		}
		key := e.Project + "/" + e.Service
		s, ok := byKey[key]
		if !ok {
			s = &Service{Project: e.Project, Service: e.Service}
			byKey[key] = s
			order = append(order, key)
		}
		for _, p := range e.Publishers {
			if p.PublishedPort == 0 || !strings.EqualFold(p.Protocol, "tcp") || !reachable(p.URL) {
				continue
			}
			if !containsInt(s.Ports, p.PublishedPort) {
				s.Ports = append(s.Ports, p.PublishedPort)
			}
		}
	}
	sort.Strings(order)
	var out2 []Service
	for _, k := range order {
		if s := byKey[k]; len(s.Ports) > 0 {
			out2 = append(out2, *s)
		}
	}
	return out2, nil
}

// reachable: published on all interfaces or on loopback, not on another
// host address the proxy should not dial.
func reachable(addr string) bool {
	switch addr {
	case "", "0.0.0.0", "::", "[::]":
		return true
	}
	ip := net.ParseIP(strings.Trim(addr, "[]"))
	return ip != nil && ip.IsLoopback()
}

func containsInt(xs []int, x int) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// psFlags are compose's global flags that take a value; they are passed to
// `docker compose ps` too, so it looks at the same project.
var psFlags = map[string]bool{
	"-f": true, "--file": true, "-p": true, "--project-name": true,
	"--project-directory": true, "--env-file": true, "--profile": true, "--ansi": false,
}

// SplitArgs separates compose's global flags from the subcommand and its
// arguments: `-f a.yml up -d` -> [-f a.yml], [up -d]. No subcommand means up.
func SplitArgs(args []string) (global, rest []string) {
	i := 0
	for i < len(args) && strings.HasPrefix(args[i], "-") {
		a := args[i]
		global = append(global, a)
		i++
		if !strings.Contains(a, "=") && psFlags[a] && i < len(args) {
			global = append(global, args[i])
			i++
		}
	}
	rest = args[i:]
	if len(rest) == 0 {
		rest = []string{"up"}
	}
	return global, rest
}

// Detached reports `up -d` / `up --detach`: compose returns at once and the
// containers keep running.
func Detached(rest []string) bool {
	if len(rest) == 0 || rest[0] != "up" {
		return false
	}
	for _, a := range rest[1:] {
		if a == "-d" || a == "--detach" {
			return true
		}
	}
	return false
}
