#!/usr/bin/env bash
# `sudo aka proxy start`: binds port 443 as root, then must drop to the
# user who ran sudo. Checks HTTPS works on 443 with no port in the URL, and
# that neither the daemon nor its files belong to root.
#
#   .github/scripts/sudo-https.sh path/to/aka
set -euo pipefail
AKA=$(cd "$(dirname "$1")" && pwd)/$(basename "$1")
# shellcheck source=.github/scripts/lib.sh
. "$(dirname "$0")/lib.sh"
export AKA_STATE_DIR="${RUNNER_TEMP:-/tmp}/aka-sudo-state"
cleanup() {
  status=$?
  [ "$status" -eq 0 ] || show_log
  "$AKA" proxy stop || true
  exit "$status"
}
trap cleanup EXIT

sudo --preserve-env=AKA_STATE_DIR "$AKA" proxy start
"$AKA" proxy status

pid=$(proxy_pid)
owner=$(ps -o user= -p "$pid" | tr -d ' ')
[ "$owner" = "$(id -un)" ] || fail "daemon runs as $owner, want $(id -un)"
bad=$(find "$AKA_STATE_DIR" -user root | head -n 5)
[ -z "$bad" ] || fail "files owned by root: $bad"

code=$(http_code https://nothing.localhost/ --cacert "$AKA_STATE_DIR/ca.pem")
[ "$code" = 404 ] || fail "https on 443 answered $code, want 404"
echo "OK: port 443 bound as root, daemon runs as $owner"
