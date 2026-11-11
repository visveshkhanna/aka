#!/usr/bin/env bash
# Installs aka's login service (launchd, systemd --user or the Windows Run
# key), checks the proxy it starts serves requests and, where the service
# manager supervises it, comes back after a crash. Then uninstalls it.
#
#   .github/scripts/service.sh path/to/aka
set -euo pipefail
AKA=$1
# shellcheck source=.github/scripts/lib.sh
. "$(dirname "$0")/lib.sh"
export AKA_STATE_DIR="${RUNNER_TEMP:-/tmp}/aka-service-state"
export AKA_PROXY_PORT=18080 AKA_HTTPS_PORT=0
cleanup() {
  status=$?
  [ "$status" -eq 0 ] || show_log
  "$AKA" service uninstall || true
  "$AKA" proxy stop || true
  exit "$status"
}
trap cleanup EXIT

"$AKA" service install
retry 40 "$AKA" proxy status >/dev/null || fail "the service did not start the proxy"
"$AKA" service status
code=$(http_code http://nothing.localhost:18080/)
[ "$code" = 404 ] || fail "proxy answered $code, want 404 for an unknown host"

case "$(uname -s)" in
  Darwin | Linux)
    old=$(proxy_pid)
    echo "Killing the proxy (pid $old) to check the service restarts it"
    kill -9 "$old"
    restarted() { pid=$(proxy_pid 2>/dev/null) && [ -n "$pid" ] && [ "$pid" != "$old" ]; }
    retry 40 restarted || fail "the service did not restart the proxy after a crash"
    echo "Restarted as pid $(proxy_pid)"
    ;;
esac

"$AKA" service uninstall
if "$AKA" service status; then fail "service still installed after uninstall"; fi
echo "OK: service install, start, restart and uninstall"
