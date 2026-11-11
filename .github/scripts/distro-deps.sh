#!/bin/sh
# Installs what linux-trust.sh needs (curl, the CA bundle tools) with the
# container's own package manager.
set -eu
if command -v apt-get >/dev/null; then
  apt-get update -qq
  DEBIAN_FRONTEND=noninteractive apt-get install -y -qq --no-install-recommends curl ca-certificates >/dev/null
elif command -v dnf >/dev/null; then
  dnf install -y -q ca-certificates
  command -v curl >/dev/null || dnf install -y -q curl
elif command -v apk >/dev/null; then
  apk add -q curl ca-certificates
elif command -v pacman >/dev/null; then
  pacman -Syu --noconfirm --needed curl ca-certificates >/dev/null
elif command -v zypper >/dev/null; then
  zypper -n -q in curl ca-certificates
else
  echo "no known package manager" >&2
  exit 1
fi
