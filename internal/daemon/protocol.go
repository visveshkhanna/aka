// Package daemon is the long-running process that owns the route table and
// serves the proxy. CLI invocations talk to it over a Unix socket in the
// state directory using newline-delimited JSON.
//
// Route ownership is tied to the connection: an `aka` run opens a session,
// registers its hostname, and keeps the connection open for the life of the
// child. When the connection closes for any reason (clean exit, crash,
// SIGKILL) the daemon drops the route. No PID polling or lock files needed.
package daemon

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"

	"aka/internal/proxy"
)

// Ops understood by the daemon.
const (
	OpPing     = "ping"
	OpList     = "list"
	OpRegister = "register" // starts a session; the connection then owns the route
	OpUpdate   = "update"   // session only: set the detected upstream port
	OpChild    = "child"    // session only: report the spawned child, for orphan cleanup
	OpShutdown = "shutdown"

	// EventEvicted is sent by the daemon to a session whose route was taken
	// over by `aka --force` elsewhere.
	EventEvicted = "evicted"
)

// Error codes.
const (
	CodeConflict = "conflict"
	CodeInvalid  = "invalid"
)

// Request is a message from a client.
type Request struct {
	Op       string `json:"op"`
	Hostname string `json:"hostname,omitempty"`
	PID      int    `json:"pid,omitempty"`
	Command  string `json:"command,omitempty"`
	Force    bool   `json:"force,omitempty"`
	// PreferPort asks register to reuse a port (when re-registering after a
	// daemon restart, so the child keeps its PORT).
	PreferPort int `json:"preferPort,omitempty"`
	// Port and DialHost are the detected upstream, for OpUpdate.
	Port     int    `json:"port,omitempty"`
	DialHost string `json:"dialHost,omitempty"`
	// Child identifies the spawned command, for OpChild. If the session
	// drops while that process is still alive (aka was SIGKILLed), the
	// daemon terminates it so it does not keep holding its port.
	ChildPID   int    `json:"childPid,omitempty"`
	ChildStart uint64 `json:"childStart,omitempty"`
	ChildPGID  int    `json:"childPgid,omitempty"`
}

// Response is a message from the daemon.
type Response struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	Code  string `json:"code,omitempty"`
	// Event is set on unsolicited messages to a session (EventEvicted).
	Event string `json:"event,omitempty"`
	// OwnerPID is the PID holding a conflicting route, or the evictor.
	OwnerPID int `json:"ownerPid,omitempty"`

	// Assigned is the port handed to the app (register).
	Assigned int `json:"assigned,omitempty"`

	Routes []proxy.Route `json:"routes,omitempty"`

	// Ping fields.
	PID       int    `json:"pid,omitempty"`
	ProxyPort int    `json:"proxyPort,omitempty"`
	BuildID   string `json:"buildId,omitempty"`
}

// maxSocketPath is below the smallest sun_path limit (104 bytes on macOS).
const maxSocketPath = 100

// SocketPath is the control socket. It normally lives in the state
// directory; if that path is too long for a Unix socket, a short path in the
// system temp dir derived from the state directory is used instead.
func SocketPath(stateDir string) string {
	p := filepath.Join(stateDir, "daemon.sock")
	if len(p) <= maxSocketPath {
		return p
	}
	sum := sha256.Sum256([]byte(stateDir))
	return filepath.Join(os.TempDir(), "aka-"+hex.EncodeToString(sum[:8])+".sock")
}

// Paths within the state directory.
func LockPath(stateDir string) string { return filepath.Join(stateDir, "daemon.lock") }
func LogPath(stateDir string) string  { return filepath.Join(stateDir, "daemon.log") }

// BuildID identifies the running binary, so a CLI can tell when the daemon
// is running an older build (common while developing aka itself). It is
// computed once at process start: statting the executable later would see a
// newer binary that replaced it on disk.
func BuildID() string { return buildID }

var buildID = computeBuildID()

func computeBuildID() string {
	exe, err := os.Executable()
	if err != nil {
		return "unknown"
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	fi, err := os.Stat(exe)
	if err != nil {
		return exe
	}
	return exe + ":" + strconv.FormatInt(fi.Size(), 10) + ":" + strconv.FormatInt(fi.ModTime().UnixNano(), 10)
}
