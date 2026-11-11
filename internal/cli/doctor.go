package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/visveshkhanna/aka/internal/brand"
	"github.com/visveshkhanna/aka/internal/certs"
	"github.com/visveshkhanna/aka/internal/daemon"
	"github.com/visveshkhanna/aka/internal/presets"
	"github.com/visveshkhanna/aka/internal/project"
	"github.com/visveshkhanna/aka/internal/proxy"
	"github.com/visveshkhanna/aka/internal/runner"
	"github.com/visveshkhanna/aka/internal/trust"
)

// report prints doctor results: one line per check, with a fix under any
// warning or failure.
type report struct {
	w     io.Writer
	fails int
	warns int
}

func (r *report) section(title string) { fmt.Fprintf(r.w, "\n%s\n", title) }
func (r *report) ok(format string, a ...any) {
	fmt.Fprintf(r.w, "  ok    %s\n", fmt.Sprintf(format, a...))
}
func (r *report) info(format string, a ...any) {
	fmt.Fprintf(r.w, "  ·     %s\n", fmt.Sprintf(format, a...))
}
func (r *report) warn(msg, fix string) {
	r.warns++
	fmt.Fprintf(r.w, "  warn  %s\n", msg)
	if fix != "" {
		fmt.Fprintf(r.w, "        -> %s\n", fix)
	}
}
func (r *report) fail(msg, fix string) {
	r.fails++
	fmt.Fprintf(r.w, "  FAIL  %s\n", msg)
	if fix != "" {
		fmt.Fprintf(r.w, "        -> %s\n", fix)
	}
}

func cmdDoctor() int {
	cwd, _ := os.Getwd()
	return doctor(os.Stdout, brand.StateDir(), cwd)
}

func doctor(w io.Writer, dir, cwd string) int {
	r := &report{w: w}
	fmt.Fprintf(w, "%s %s doctor\n", brand.Name, version())

	r.section("State")
	checkStateDir(r, dir)
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, brand.EnvPrefix) {
			r.info("%s", kv)
		}
	}

	r.section("Proxy")
	ping, running := checkDaemon(r, dir)

	r.section("HTTPS")
	checkCA(r, dir, ping, running)

	r.section("Name resolution")
	checkResolution(r)

	if running {
		r.section("Routes")
		checkRoutes(r, dir, ping)
	}

	r.section("This project (" + cwd + ")")
	checkProject(r, dir, cwd)

	fmt.Fprintln(w)
	switch {
	case r.fails > 0:
		fmt.Fprintf(w, "%d problem(s), %d warning(s).\n", r.fails, r.warns)
		return 1
	case r.warns > 0:
		fmt.Fprintf(w, "No problems, %d warning(s).\n", r.warns)
	default:
		fmt.Fprintln(w, "Everything looks fine.")
	}
	return 0
}

func checkStateDir(r *report, dir string) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		r.fail("state directory "+dir+" cannot be created: "+err.Error(), "set "+brand.Env("STATE_DIR")+" to a writable directory")
		return
	}
	f, err := os.CreateTemp(dir, ".doctor-*")
	if err != nil {
		r.fail("state directory "+dir+" is not writable: "+err.Error(), "fix its ownership (was it created by sudo?) or set "+brand.Env("STATE_DIR"))
		return
	}
	f.Close()
	os.Remove(f.Name())
	r.ok("state directory %s", dir)
}

func checkDaemon(r *report, dir string) (daemon.Response, bool) {
	ping, err := daemon.Ping(dir)
	if err != nil {
		r.info("the proxy is not running (it starts with the first app, or `%s proxy start`)", brand.Name)
		return ping, false
	}
	r.ok("%s", describeProxy(ping))
	if ping.BuildID != daemon.BuildID() {
		r.warn("the proxy is running a different build of "+brand.Name, "run `"+brand.Name+" proxy stop`; the next app starts the current one")
	}
	if want := httpsEnabled(); ping.TLS != want {
		r.warn(fmt.Sprintf("the proxy has HTTPS %s but %s asks for %s", onOffWord(ping.TLS), brand.Env("HTTPS"), onOffWord(want)),
			"run `"+brand.Name+" proxy stop` to restart it in the new mode")
	}
	if note := daemon.HTTPSNote(ping); note != "" {
		r.info("%s", note)
	}
	for _, host := range []string{"127.0.0.1", "::1"} {
		c, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(ping.ProxyPort)), time.Second)
		if err != nil {
			if host == "::1" {
				r.info("no IPv6 loopback ([::1]:%d): %v", ping.ProxyPort, err)
				continue
			}
			r.fail(fmt.Sprintf("the proxy does not answer on %s:%d: %v", host, ping.ProxyPort, err), "check `"+brand.Name+" proxy status` and "+daemon.LogPath(dir))
			continue
		}
		c.Close()
	}
	return ping, true
}

func onOffWord(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

func checkCA(r *report, dir string, ping daemon.Response, running bool) {
	if !httpsEnabled() && (!running || !ping.TLS) {
		r.info("HTTPS is off (%s=0)", brand.Env("HTTPS"))
		return
	}
	ca, err := certs.LoadCA(dir)
	if errors.Is(err, certs.ErrNoCA) {
		r.info("no local CA yet; it is created with the first HTTPS app (or `%s trust`)", brand.Name)
		return
	}
	if err != nil {
		r.fail(err.Error(), "")
		return
	}
	r.ok("local CA %s (%s)", certs.CertPath(dir), ca.CommonName())
	if !ca.Covers(brand.DefaultTLD) {
		r.fail("the CA cannot sign *."+brand.DefaultTLD+" names", "run `"+brand.Name+" untrust`, delete "+certs.CertPath(dir)+" and "+certs.KeyPath(dir)+", then `"+brand.Name+" trust`")
	}
	if left := time.Until(ca.Cert.NotAfter); left < 0 {
		r.fail("the CA expired on "+ca.Cert.NotAfter.Format("2006-01-02"), "run `"+brand.Name+" untrust`, delete the CA files, then `"+brand.Name+" trust`")
	} else if left < 365*24*time.Hour {
		r.warn("the CA expires on "+ca.Cert.NotAfter.Format("2006-01-02"), "replace it before then: untrust, delete the CA files, trust")
	}
	if trust.IsTrusted(ca) {
		r.ok("the system trusts the CA")
	} else {
		r.warn("the system does not trust the CA, so browsers will warn about certificates", "run `"+brand.Name+" trust`")
	}
	if missing := nssWithoutCertutil(); missing {
		r.warn("Firefox (or Chrome on Linux) keeps its own certificate store and certutil is not installed",
			"install certutil (libnss3-tools / nss-tools / brew install nss), then `"+brand.Name+" trust`")
	}
	r.info("Node apps get NODE_EXTRA_CA_CERTS; for Python requests use REQUESTS_CA_BUNDLE=%s only for calls between local apps", certs.CertPath(dir))
}

// nssWithoutCertutil reports NSS databases that cannot be updated.
func nssWithoutCertutil() bool {
	if _, err := exec.LookPath("certutil"); err == nil {
		return false
	}
	home, _ := os.UserHomeDir()
	for _, pat := range []string{
		filepath.Join(home, ".mozilla", "firefox", "*", "cert9.db"),
		filepath.Join(home, ".pki", "nssdb", "cert9.db"),
		filepath.Join(home, "Library", "Application Support", "Firefox", "Profiles", "*", "cert9.db"),
		filepath.Join(home, "AppData", "Roaming", "Mozilla", "Firefox", "Profiles", "*", "cert9.db"),
	} {
		if m, _ := filepath.Glob(pat); len(m) > 0 {
			return true
		}
	}
	return false
}

func checkResolution(r *report) {
	name := brand.Name + "-doctor-check." + brand.DefaultTLD
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupHost(ctx, name)
	loopback := err == nil && len(addrs) > 0
	for _, a := range addrs {
		if ip := net.ParseIP(a); ip == nil || !ip.IsLoopback() {
			loopback = false
		}
	}
	if loopback {
		r.ok("*.%s resolves to loopback (%s)", brand.DefaultTLD, strings.Join(addrs, ", "))
		return
	}
	r.warn("the system resolver does not map *."+brand.DefaultTLD+" to loopback (browsers and curl still do)",
		"code that calls another app's URL (Node fetch, Python requests) needs hosts-file entries such as `127.0.0.1 api."+brand.DefaultTLD+"`")
}

func checkRoutes(r *report, dir string, ping daemon.Response) {
	list, err := daemon.Do(dir, daemon.Request{Op: daemon.OpList})
	if err != nil {
		r.fail("cannot list routes: "+err.Error(), "")
		return
	}
	if len(list.Routes) == 0 {
		r.info("no apps running")
		return
	}
	client := &http.Client{
		Timeout:       5 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	for _, rt := range list.Routes {
		url := ping.URL(rt.Hostname)
		if rt.Port == 0 {
			r.warn(url+": the app has not opened a port yet", "if it never does, give it with --port-hint or {port}")
			continue
		}
		status, hint, err := probeRoute(client, ping.ProxyPort, rt)
		switch {
		case err != nil:
			r.fail(fmt.Sprintf("%s -> port %d: %v", url, rt.Port, err), "")
		case hint != "":
			r.fail(fmt.Sprintf("%s -> port %d answers %d", url, rt.Port, status), hint)
		case status == 502 || status == 503:
			fix := "is the app still running? (`" + brand.Name + " list`)"
			if rt.PID == 0 {
				fix = "nothing listens on port " + strconv.Itoa(rt.Port) + "; start that service or `" + brand.Name + " alias rm " + trimTLD(rt.Hostname) + "`"
			}
			r.warn(fmt.Sprintf("%s -> port %d is not answering (%d)", url, rt.Port, status), fix)
		default:
			kind := "app"
			if rt.PID == 0 {
				kind = "alias"
			}
			r.ok("%s -> port %d (%s, HTTP %d)", url, rt.Port, kind, status)
		}
	}
}

// probeRoute makes one request through the proxy's plain-HTTP side, the
// way a browser would, and diagnoses a host rejection.
func probeRoute(client *http.Client, proxyPort int, rt proxy.Route) (int, string, error) {
	req, err := http.NewRequest("GET", "http://127.0.0.1:"+strconv.Itoa(proxyPort)+"/", nil)
	if err != nil {
		return 0, "", err
	}
	req.Host = rt.Hostname + ":" + strconv.Itoa(proxyPort)
	req.Header.Set("Accept", "text/html")
	resp, err := client.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	return resp.StatusCode, proxy.Diagnose(resp.StatusCode, body), nil
}

func trimTLD(h string) string { return strings.TrimSuffix(h, "."+brand.DefaultTLD) }

func checkProject(r *report, dir, cwd string) {
	n := project.InferName(cwd)
	name := n.Name
	if w, ok := project.WorktreePrefix(cwd); ok {
		name = w.Prefix + "." + name
		r.info("worktree prefix %q (%s)", w.Prefix, w.Source)
	}
	r.ok("name %s (from %s)", Hostname(name), n.Source)

	path := runner.AugmentPath(os.Getenv("PATH"), cwd)
	look := func(c string) bool { _, err := runner.LookPath(c, path); return err == nil }
	pdir, cands := project.FindDevCommands(cwd, project.Env{LookPath: look})
	if saved, ok := project.NewChoices(dir).Get(pdir); ok {
		r.ok("bare `%s` runs %s (your saved choice)", brand.Name, strings.Join(saved, " "))
		cands = []project.Candidate{{Command: saved, Source: "saved choice"}}
	} else if len(cands) == 0 {
		r.info("no dev command found; run apps as `%s <name> <command...>`", brand.Name)
		return
	} else {
		r.ok("bare `%s` runs %s (from %s, in %s)", brand.Name, cands[0], cands[0].Source, pdir)
		for _, c := range cands[1:] {
			r.info("also possible: %s (%s); `%s --choose` to pick", c, c.Source, brand.Name)
		}
	}
	if !look(cands[0].Command[0]) && !strings.ContainsAny(cands[0].Command[0], `/\`) {
		r.warn(cands[0].Command[0]+" is not installed or not on PATH", "install it, or run the app with an explicit command")
	}

	ps, err := presets.Load(filepath.Join(dir, "presets.json"))
	if err != nil {
		r.fail("presets file: "+err.Error(), "fix or remove "+filepath.Join(dir, "presets.json"))
	}
	res := ps.Apply(cands[0].Command, placeholderPort, pdir, os.Getenv)
	switch {
	case res.Preset == "":
		r.info("no preset for this command: the app should read PORT, or detection finds its port")
	case res.Skipped != "" && len(res.Env) == 0:
		r.warn("preset "+res.Preset+" will not be applied: "+res.Skipped, "the port is detected instead; two such apps may collide")
	default:
		added := append(append([]string{}, res.Added...), res.Env...)
		r.ok("preset %s adds %s (with the app's port)", res.Preset, strings.ReplaceAll(strings.Join(added, " "), strconv.Itoa(placeholderPort), "PORT"))
	}
}

// placeholderPort stands for the app's port when showing what a preset
// adds; it is replaced by "PORT" in the output.
const placeholderPort = 65001
