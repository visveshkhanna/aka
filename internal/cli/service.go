package cli

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/visveshkhanna/aka/internal/brand"
	"github.com/visveshkhanna/aka/internal/daemon"
	"github.com/visveshkhanna/aka/internal/service"
)

// cmdService manages the login service that starts the proxy:
//
//	aka service install     start the proxy at login (and now)
//	aka service uninstall
//	aka service status
func cmdService(args []string) int {
	sub := "status"
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "install":
		return serviceInstall()
	case "uninstall", "remove":
		if err := service.Uninstall(); err != nil {
			return fail(err)
		}
		fmt.Println("Login service removed. A running proxy keeps running until `" + brand.Name + " proxy stop`.")
		return 0
	case "status":
		st := service.Query()
		if !st.Installed {
			fmt.Printf("Not installed. `%s service install` starts the proxy at login.\n", brand.Name)
			return 1
		}
		fmt.Printf("Installed: %s\n", st.Path)
		if st.Detail != "" {
			fmt.Printf("Service manager: %s\n", st.Detail)
		}
		if r, err := daemon.Ping(brand.StateDir()); err == nil {
			fmt.Println(describeProxy(r))
		} else {
			fmt.Println("The proxy is not running (it starts at the next login, or `" + brand.Name + " proxy start`).")
		}
		return 0
	}
	return fail(fmt.Errorf("usage: %s service install|uninstall|status", brand.Name))
}

func serviceInstall() int {
	so, err := startOptions()
	if err != nil {
		return fail(err)
	}
	exe, err := os.Executable()
	if err != nil {
		return fail(err)
	}
	env := map[string]string{}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok && strings.HasPrefix(k, brand.EnvPrefix) {
			env[k] = v // keep this session's settings (state dir, ports, HTTPS)
		}
	}
	if so.TLS {
		ensureTrust(so.StateDir)
	}
	// Hand over to the service: stop an idle proxy started by hand, so the
	// service's one takes its place.
	running := false
	if _, err := daemon.Ping(so.StateDir); err == nil {
		if daemon.Idle(so.StateDir) {
			daemon.Stop(so.StateDir, 3*time.Second)
		} else {
			running = true
		}
	}
	path, err := service.Install(service.Spec{Exe: exe, Args: so.Args, Env: env, LogPath: daemon.LogPath(so.StateDir)})
	if err != nil {
		return fail(err)
	}
	fmt.Printf("Installed %s.\nThe proxy now starts at login.\n", path)
	if running {
		fmt.Println("A proxy with running apps was left alone; the service's proxy takes over after `" + brand.Name + " proxy stop` or at the next login.")
	}
	return 0
}
