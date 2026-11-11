# shellcheck shell=sh
# Shared helpers for the CI scripts. Source it; AKA must name the binary.

fail() { echo "FAIL: $*" >&2; exit 1; }

# retry N CMD...: run CMD every half second until it succeeds, N tries.
retry() {
  n=$1; shift
  i=0
  until "$@"; do
    i=$((i + 1))
    [ "$i" -ge "$n" ] && return 1
    sleep 0.5
  done
}

# http_code URL [curl args...]: the status code, or 000 if the request failed.
http_code() {
  url=$1; shift
  curl -s -o /dev/null -w '%{http_code}' --max-time 10 "$@" "$url" || true
}

# proxy_pid: the daemon's pid from `aka proxy status`.
proxy_pid() {
  "$AKA" proxy status | sed -n 's/.*(pid \([0-9]*\)).*/\1/p' | head -n 1
}

show_log() {
  log="${AKA_STATE_DIR:-$HOME/.aka}/daemon.log"
  [ -f "$log" ] && { echo "--- daemon log"; cat "$log"; }
  return 0
}
