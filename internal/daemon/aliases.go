package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/visveshkhanna/aka/internal/brand"
	"github.com/visveshkhanna/aka/internal/proxy"
)

// An alias is a static route to a port aka did not start (a Docker
// container, a server started elsewhere). Aliases are kept in the state
// directory, so they survive daemon restarts.

type aliasFile struct {
	Hostname string `json:"hostname"`
	Port     int    `json:"port"`
	DialHost string `json:"dialHost,omitempty"`
}

// AliasesPath is where aliases are stored.
func AliasesPath(stateDir string) string { return filepath.Join(stateDir, "aliases.json") }

func (s *Server) loadAliases() {
	b, err := os.ReadFile(AliasesPath(s.cfg.StateDir))
	if err != nil {
		return
	}
	var list []aliasFile
	if err := json.Unmarshal(b, &list); err != nil {
		s.log.Printf("ignoring %s: %v", AliasesPath(s.cfg.StateDir), err)
		return
	}
	for _, a := range list {
		if a.Hostname != "" && a.Port > 0 && a.Port < 65536 {
			s.routes[a.Hostname] = aliasEntry(a)
		}
	}
}

func aliasEntry(a aliasFile) *entry {
	return &entry{route: proxy.Route{Hostname: a.Hostname, Port: a.Port, DialHost: a.DialHost, Command: "alias", Since: time.Now()}}
}

// saveAliasesLocked writes the current aliases. Callers hold s.mu.
func (s *Server) saveAliasesLocked() error {
	list := []aliasFile{}
	for _, e := range s.routes {
		if e.isAlias() {
			list = append(list, aliasFile{e.route.Hostname, e.route.Port, e.route.DialHost})
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Hostname < list[j].Hostname })
	b, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	tmp := AliasesPath(s.cfg.StateDir) + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, AliasesPath(s.cfg.StateDir))
}

func (e *entry) isAlias() bool { return e.sess == nil && e.route.PID == 0 }

// alias adds, replaces or removes an alias.
func (s *Server) alias(req Request) Response {
	if req.Hostname == "" {
		return Response{Error: "hostname required", Code: CodeInvalid}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	old, exists := s.routes[req.Hostname]
	if req.Remove {
		if !exists || !old.isAlias() {
			return Response{Error: req.Hostname + " is not an alias", Code: CodeInvalid}
		}
		delete(s.routes, req.Hostname)
		s.log.Printf("alias %s removed", req.Hostname)
		return s.savedResponse()
	}
	if req.Port < 1 || req.Port > 65535 {
		return Response{Error: fmt.Sprintf("invalid port %d", req.Port), Code: CodeInvalid}
	}
	if exists && !old.isAlias() {
		return Response{
			Error:    fmt.Sprintf("%s is in use by a running app (%s, pid %d)", req.Hostname, old.route.Command, old.route.PID),
			Code:     CodeConflict,
			OwnerPID: old.route.PID,
		}
	}
	s.routes[req.Hostname] = aliasEntry(aliasFile{req.Hostname, req.Port, req.DialHost})
	s.log.Printf("alias %s -> port %d", req.Hostname, req.Port)
	return s.savedResponse()
}

func (s *Server) savedResponse() Response {
	if err := s.saveAliasesLocked(); err != nil {
		return Response{Error: "saving aliases: " + err.Error()}
	}
	return Response{OK: true}
}

func aliasConflict(e *entry) Response {
	return Response{
		Error: fmt.Sprintf("%s is an alias for port %d; remove it first with `%s alias rm %s`",
			e.route.Hostname, e.route.Port, brand.Name, trimTLD(e.route.Hostname)),
		Code: CodeConflict,
	}
}

func trimTLD(h string) string {
	if n := len(h) - len(brand.DefaultTLD) - 1; n > 0 && h[n:] == "."+brand.DefaultTLD {
		return h[:n]
	}
	return h
}
