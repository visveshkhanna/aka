package detect

import (
	"context"
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// listeners shells out to lsof, which ships with every macOS install and
// needs no privileges for the user's own processes. libproc would avoid the
// subprocess but requires cgo, which would break the static build.
func listeners(pids []int) ([]Listener, error) {
	ps := make([]string, len(pids))
	for i, p := range pids {
		ps[i] = strconv.Itoa(p)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	// -a ANDs the selections; -n/-P skip DNS and port-name lookups; -w
	// silences warnings about inaccessible filesystems.
	cmd := exec.CommandContext(ctx, "lsof", "-nP", "-w", "-a",
		"-iTCP", "-sTCP:LISTEN", "-p", strings.Join(ps, ","), "-F", "ptn")
	out, err := cmd.Output()
	if err != nil {
		// lsof exits 1 when nothing matched, which is the common case early on.
		var ee *exec.ExitError
		if errors.As(err, &ee) && ee.ExitCode() == 1 {
			return parseLsof(string(out)), nil
		}
		return nil, err
	}
	return parseLsof(string(out)), nil
}
