#!/bin/sh
# Runs as root in a distro container: `aka trust` must make the system
# trust store accept the proxy's certificates (checked with the distro's
# own curl and CA bundle, no --cacert), and `aka untrust` must undo it.
#
#   .github/scripts/linux-trust.sh path/to/aka
set -eu
AKA=$1
# shellcheck source=.github/scripts/lib.sh
. "$(dirname "$0")/lib.sh"
export AKA_STATE_DIR=/tmp/aka-trust-state AKA_PROXY_PORT=18443 AKA_HTTPS_PORT=0
command -v curl >/dev/null || fail "curl is not installed"
# shellcheck disable=SC1091
. /etc/os-release
echo "== $PRETTY_NAME"

"$AKA" proxy start
url=https://nothing.localhost:18443/
# Pin the name: older curl (Ubuntu 22.04) leaves *.localhost to the system
# resolver, which may not know it. This test is about trust, not DNS.
pin=nothing.localhost:18443:127.0.0.1

code=$(http_code "$url" --resolve "$pin")
[ "$code" = 000 ] || fail "an untrusted certificate was accepted ($code)"

"$AKA" trust
code=$(http_code "$url" --resolve "$pin")
[ "$code" = 404 ] || { curl -sv --max-time 10 --resolve "$pin" "$url" >/dev/null || true; fail "after aka trust: $code, want 404"; }
"$AKA" trust | grep -q "already trusted" || fail "a second aka trust did not see the CA as trusted"

"$AKA" untrust
code=$(http_code "$url" --resolve "$pin")
[ "$code" = 000 ] || fail "after aka untrust the certificate is still accepted ($code)"

"$AKA" proxy stop
echo "OK: trust and untrust on $PRETTY_NAME"
