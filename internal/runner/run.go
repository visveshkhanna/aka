// Package runner implements `aka <name> <command>`: register a route, spawn
// the command with PORT set, find the port it actually listens on, route to
// it, and clean up when it exits.
package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"aka/internal/brand"
	"aka/internal/certs"
	"aka/internal/daemon"
	"aka/internal/detect"
	"aka/internal/presets"
)

// Options configures a run.
type Options struct {
	Hostname string   // full hostname, e.g. myapp.localhost
	Args     []string // command and arguments
	Dir      string   // working directory ("" for the current one)
	Force    bool     // take over a hostname another aka holds
	PortHint int      // pin the upstream port (skip heuristics)
	Daemon   daemon.StartOptions
	// Presets set the port of commands that ignore PORT. Nil disables
	// them; so does a {port} in Args, which means the user chose.
	Presets *presets.Set

	Stdout, Stderr *os.File
	// Notice receives aka's own status lines (default: Stderr).
	Notice io.Writer
}

// Poll intervals for listener detection: fast until the first port shows
// up, then slower for the life of the child to follow restarts.
var (
	pollFast       = 250 * time.Millisecond
	pollSlow       = 1 * time.Second
	waitingNotice  = 20 * time.Second
	shutdownGrace  = 3 * time.Second
	probeTimeout   = 800 * time.Millisecond
	probeRetryTime = 2 * time.Second
)

// Run runs the command and returns its exit code. err is non-nil only when
// the command could not be started at all.
func Run(o Options) (int, error) {
	if len(o.Args) == 0 {
		return 2, errors.New("no command given")
	}
	if o.Stdout == nil {
		o.Stdout = os.Stdout
	}
	if o.Stderr == nil {
		o.Stderr = os.Stderr
	}
	if o.Notice == nil {
		o.Notice = o.Stderr
	}
	logf := func(format string, a ...any) {
		fmt.Fprintf(o.Notice, "[%s] %s\n", brand.Name, fmt.Sprintf(format, a...))
	}

	display := strings.Join(o.Args, " ")
	sess := newSession(o.Daemon, o.Hostname, os.Getpid(), display, o.Force, logf)
	if err := sess.open(0, o.Force); err != nil {
		var ce *ConflictError
		if errors.As(err, &ce) {
			return 1, errors.New(conflictHelp(ce, brand.Name))
		}
		return 1, err
	}
	defer sess.close()
	assigned, ping := sess.info()
	url := ping.URL(o.Hostname)
	if note := daemon.HTTPSNote(ping); note != "" && ping.Started {
		logf("%s", note)
	}
	caFile := ""
	if ping.TLS {
		caFile = certs.CertPath(o.Daemon.StateDir)
	}

	args := SubstitutePort(o.Args, assigned)
	cwd := o.Dir
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	var presetEnv []string
	if o.Presets != nil && !hasPlaceholder(o.Args) {
		r := o.Presets.Apply(args, assigned, cwd, os.Getenv)
		args, presetEnv = r.Args, r.Env
		switch {
		case r.Preset == "":
		case r.Skipped != "" && len(r.Env) == 0:
			logf("preset %s: not applied (%s); detecting the port instead", r.Preset, r.Skipped)
		default:
			var what []string
			if len(r.Added) > 0 {
				what = append(what, strings.Join(r.Added, " "))
			}
			what = append(what, r.Env...)
			logf("preset %s: %s", r.Preset, strings.Join(what, " "))
		}
	}
	path := AugmentPath(os.Getenv("PATH"), cwd)
	name := args[0]
	if strings.ContainsAny(name, `/\`) && !filepath.IsAbs(name) {
		name = filepath.Join(cwd, name) // bin/dev runs relative to the project dir
	}
	bin, err := LookPath(name, path)
	if err != nil {
		return 127, fmt.Errorf("%s: command not found", args[0])
	}

	cmd := exec.Command(bin, args[1:]...)
	cmd.Args[0] = args[0]
	cmd.Dir = cwd
	cmd.Stdin = os.Stdin
	isolate(cmd)
	hints := &detect.Hints{}
	out, err := setupOutput(cmd, o.Stdout, o.Stderr, hints)
	if err != nil {
		return 1, err
	}
	cmd.Env = append(BuildEnv(os.Environ(), assigned, url, path, caFile, out.piped), presetEnv...)

	logf("%s -> %s (PORT=%d)", url, display, assigned)
	sigs := notifySignals() // before Start; see notifySignals
	defer stopSignals(sigs)
	if err := cmd.Start(); err != nil {
		out.abort()
		return 126, err
	}
	out.started()
	childPid := cmd.Process.Pid
	pgid := childGroup(childPid)
	if st, ok := detect.StartTime(childPid); ok {
		sess.setChild(childPid, st, pgid)
	}

	ctx, cancel := context.WithCancel(context.Background())
	w := &watcher{
		childPid: childPid,
		pgid:     pgid,
		assigned: assigned,
		pinned:   o.PortHint,
		hints:    hints,
		sess:     sess,
		logf:     logf,
		url:      url,
		known:    map[int]uint64{},
	}
	watchDone := make(chan struct{})
	go func() { w.run(ctx); close(watchDone) }()

	waitErr := make(chan error, 1)
	go func() { waitErr <- cmd.Wait() }()

	// Forward each signal once: a second Ctrl+C is passed on too (dev
	// servers treat it as "quit now"); a third kills the tree outright.
	var exitErr error
	requests := 0
	forward := func(sig os.Signal) {
		requests++
		if requests >= 3 {
			sig = killSignal
		}
		cmd.Process.Signal(sig) // not yet reaped, so the PID is still ours
		w.signalTree(sig)
	}
loop:
	for {
		select {
		case exitErr = <-waitErr:
			break loop
		case sig := <-sigs:
			forward(sig)
		case pid := <-sess.evicted:
			logf("%s was taken over by pid %d; stopping", o.Hostname, pid)
			forward(termSignal)
		}
	}

	cancel()
	<-watchDone
	sess.close()
	w.reapLeftovers()
	releaseGroup(pgid)
	out.finish()
	return exitCode(exitErr), nil
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		if code := signalExitCode(ee); code > 0 {
			return code
		}
		return ee.ExitCode()
	}
	return 1
}

func hasPlaceholder(args []string) bool {
	for _, a := range args {
		if strings.Contains(a, "{port}") {
			return true
		}
	}
	return false
}

// SubstitutePort replaces every "{port}" in args with port.
func SubstitutePort(args []string, port int) []string {
	out := make([]string, len(args))
	p := strconv.Itoa(port)
	for i, a := range args {
		out[i] = strings.ReplaceAll(a, "{port}", p)
	}
	return out
}

// BuildEnv returns the child's environment: the parent's, with PORT, HOST,
// AKA_URL and PATH set. When output is piped (not a terminal) Python is told
// not to buffer stdout, so its startup URL reaches the scanner promptly.
//
// caFile, if set, is the local CA certificate. Node does not use the OS
// trust store, so NODE_EXTRA_CA_CERTS points it there, letting server-side
// code fetch other apps' https://*.localhost URLs. A value the user already
// set is kept.
func BuildEnv(base []string, port int, url, path, caFile string, piped bool) []string {
	set := map[string]string{
		"PORT":            strconv.Itoa(port),
		"HOST":            "127.0.0.1",
		brand.Env("URL"):  url,
		brand.Env("PORT"): strconv.Itoa(port),
		"PATH":            path,
	}
	out := make([]string, 0, len(base)+len(set)+1)
	hasUnbuffered, hasNodeCA := false, false
	for _, kv := range base {
		k, _, _ := strings.Cut(kv, "=")
		if _, override := set[k]; override {
			continue
		}
		switch k {
		case "PYTHONUNBUFFERED":
			hasUnbuffered = true
		case "NODE_EXTRA_CA_CERTS":
			hasNodeCA = true
		}
		out = append(out, kv)
	}
	for _, k := range []string{"PORT", "HOST", brand.Env("URL"), brand.Env("PORT"), "PATH"} {
		out = append(out, k+"="+set[k])
	}
	if piped && !hasUnbuffered {
		out = append(out, "PYTHONUNBUFFERED=1")
	}
	if caFile != "" && !hasNodeCA {
		out = append(out, "NODE_EXTRA_CA_CERTS="+caFile)
	}
	return out
}

// AugmentPath prepends every node_modules/.bin from cwd up to the root, so
// project-local binaries (ng, vite, next) resolve the way `npx` would.
func AugmentPath(path, cwd string) string {
	var bins []string
	for dir := cwd; dir != ""; {
		bin := filepath.Join(dir, "node_modules", ".bin")
		if fi, err := os.Stat(bin); err == nil && fi.IsDir() {
			bins = append(bins, bin)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	if len(bins) == 0 {
		return path
	}
	if path != "" {
		bins = append(bins, path)
	}
	return strings.Join(bins, string(os.PathListSeparator))
}

// LookPath resolves name against path (not the current process's PATH,
// which lacks node_modules/.bin).
func LookPath(name, path string) (string, error) {
	if strings.ContainsRune(name, os.PathSeparator) || strings.Contains(name, "/") {
		return exec.LookPath(name)
	}
	for _, dir := range filepath.SplitList(path) {
		if dir == "" {
			dir = "."
		}
		if p, err := exec.LookPath(filepath.Join(dir, name)); err == nil {
			return p, nil
		}
	}
	return "", exec.ErrNotFound
}

// watcher polls the child tree for listeners and keeps the route pointed at
// the right port.
type watcher struct {
	childPid, pgid int
	assigned       int
	pinned         int
	hints          *detect.Hints
	sess           *session
	logf           func(string, ...any)
	url            string

	mu sync.Mutex
	// known maps every PID seen in the child tree to its start time, so
	// cleanup never signals an unrelated process that reused a PID.
	known   map[int]uint64
	current detect.Listener
	routed  bool
}

func (w *watcher) run(ctx context.Context) {
	cache := &detect.ProbeCache{TTL: probeRetryTime, Probe: func(l detect.Listener) bool { return detect.ProbeHTTP(l, probeTimeout) }}
	started := time.Now()
	warned := false
	lastSeq := -1
	interval := pollFast
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		ls := w.scan()
		cache.Retain(ls)
		w.mu.Lock()
		cur := w.current.Port
		w.mu.Unlock()
		choice, ok := detect.Choose(detect.Input{
			Assigned:  w.assigned,
			Pinned:    w.pinned,
			Listeners: ls,
			Hints:     w.hints.Ports(),
			Current:   cur,
			Probe:     cache.Check,
		})
		if ctx.Err() != nil {
			return
		}
		switch {
		case ok:
			w.route(choice)
			interval = pollSlow
		case w.isRouted():
			// The port went away: the app is restarting or crashed. Show the
			// "starting" page until it comes back.
			w.unroute()
			interval = pollFast
		case !warned && time.Since(started) > waitingNotice:
			warned = true
			if len(ls) == 0 {
				w.logf("still waiting for the app to listen on a port (PORT=%d). If it uses a fixed port, pass --port-hint <port>.", w.assigned)
			} else {
				w.logf("still waiting: the app listens on %v but none answered HTTP. Pass --port-hint <port> to choose one.", detect.Ports(ls))
			}
		}
		// A freshly printed URL is worth checking right away.
		if seq := w.hints.Seq(); seq != lastSeq && lastSeq != -1 {
			interval = pollFast
		} else {
			lastSeq = seq
		}
		timer.Reset(interval)
	}
}

// scan returns the listeners of the child tree and records its PIDs.
func (w *watcher) scan() []detect.Listener {
	procs, err := detect.ListProcs()
	if err != nil {
		return nil
	}
	tree := detect.Tree(procs, w.childPid, w.pgid, os.Getpid())
	w.remember(procs, tree)
	ls, _ := detect.Listeners(tree)
	return ls
}

func (w *watcher) remember(procs []detect.Proc, tree []int) {
	start := make(map[int]uint64, len(procs))
	for _, p := range procs {
		start[p.PID] = p.Start
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, pid := range tree {
		if st, ok := start[pid]; ok {
			w.known[pid] = st
		}
	}
}

func (w *watcher) isRouted() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.routed
}

func (w *watcher) route(c detect.Choice) {
	w.mu.Lock()
	prev, wasRouted := w.current, w.routed
	same := wasRouted && prev.Port == c.Listener.Port && prev.DialHost() == c.Listener.DialHost()
	w.current, w.routed = c.Listener, true
	w.mu.Unlock()
	if same {
		return
	}
	dial := c.Listener.IP // "" (wildcard) lets the proxy try both loopbacks
	w.sess.setUpstream(c.Listener.Port, dial)
	if wasRouted || prev.Port == c.Listener.Port {
		if prev.Port != c.Listener.Port {
			w.logf("app moved to port %d; %s follows it", c.Listener.Port, w.url)
		}
		return
	}
	msg := fmt.Sprintf("ready: %s -> %s (%s)", w.url, net.JoinHostPort(c.Listener.DialHost(), strconv.Itoa(c.Listener.Port)), c.Reason)
	if c.Listener.Port != w.assigned && w.pinned == 0 {
		msg += fmt.Sprintf("; the app ignored PORT=%d", w.assigned)
	}
	w.logf("%s", msg)
	if c.Listener.IP == "" {
		w.logf("note: the app listens on all interfaces, so port %d is also reachable from your network", c.Listener.Port)
	}
}

func (w *watcher) unroute() {
	w.mu.Lock()
	w.routed = false
	w.mu.Unlock()
	w.sess.setUpstream(0, "")
}

// signalTree sends sig to the child's process group and to every process
// seen in its tree (some leave the group by calling setsid). Only call it
// while the child is unreaped: its PID, and so its group ID, are then still
// reserved.
func (w *watcher) signalTree(sig os.Signal) {
	signalGroup(w.pgid, sig)
	for pid := range w.liveSet() {
		if p, err := os.FindProcess(pid); err == nil {
			p.Signal(sig)
		}
	}
}

// liveSet returns tree members (current and previously seen) that are still
// running, keyed by PID with their start time.
func (w *watcher) liveSet() map[int]uint64 {
	procs, _ := detect.ListProcs()
	w.remember(procs, detect.Tree(procs, w.childPid, w.pgid, os.Getpid()))
	w.mu.Lock()
	defer w.mu.Unlock()
	out := map[int]uint64{}
	for _, p := range procs {
		if st, ok := w.known[p.PID]; ok && st == p.Start && p.PID != os.Getpid() {
			out[p.PID] = st
		}
	}
	return out
}

// reapLeftovers terminates processes from the child's tree that outlived it
// (a reloader's worker, a server that ignored the signal), escalating to
// SIGKILL after a grace period. The set is frozen right after the child
// exits and matched by PID and start time, so nothing that starts later,
// even with a reused PID or group ID, is ever signalled.
func (w *watcher) reapLeftovers() {
	frozen := w.liveSet()
	alive := func() []int {
		procs, _ := detect.ListProcs()
		var out []int
		for _, p := range procs {
			if st, ok := frozen[p.PID]; ok && st == p.Start {
				out = append(out, p.PID)
			}
		}
		return out
	}
	signal := func(pids []int, sig os.Signal) {
		for _, pid := range pids {
			if p, err := os.FindProcess(pid); err == nil {
				p.Signal(sig)
			}
		}
	}
	left := alive()
	if len(left) == 0 {
		return
	}
	signal(left, termSignal)
	deadline := time.Now().Add(shutdownGrace)
	for time.Now().Before(deadline) {
		if left = alive(); len(left) == 0 {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	signal(alive(), killSignal)
}
