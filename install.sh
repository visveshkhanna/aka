#!/bin/sh
# Installs aka from its GitHub releases on macOS and Linux.
#
#   curl -fsSL https://raw.githubusercontent.com/visveshkhanna/aka/main/install.sh | sh
#
# Settings, as environment variables:
#   AKA_VERSION      a release tag such as v0.1.0 (default: the latest release)
#   AKA_INSTALL_DIR  where to put the binary (default: ~/.local/bin)
set -eu

repo=visveshkhanna/aka
dir=${AKA_INSTALL_DIR:-$HOME/.local/bin}

fail() {
  echo "aka install: $*" >&2
  exit 1
}

need() { command -v "$1" >/dev/null 2>&1 || fail "$1 is required"; }
need curl
need tar
need uname

case $(uname -s) in
  Darwin) os=darwin ;;
  Linux) os=linux ;;
  *) fail "unsupported system $(uname -s); on Windows, download the zip from https://github.com/$repo/releases" ;;
esac
case $(uname -m) in
  x86_64 | amd64) arch=amd64 ;;
  arm64 | aarch64) arch=arm64 ;;
  armv7l | armv7) arch=armv7 ;;
  *) fail "unsupported architecture $(uname -m)" ;;
esac

version=${AKA_VERSION:-}
if [ -z "$version" ]; then
  # The latest release's page redirects to its tag.
  url=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/$repo/releases/latest") ||
    fail "could not find the latest release"
  version=${url##*/}
  case $version in
    v*) ;;
    *) fail "no release found at https://github.com/$repo/releases" ;;
  esac
fi

name="aka_${version#v}_${os}_${arch}"
base="https://github.com/$repo/releases/download/$version"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

echo "Downloading aka $version for $os/$arch"
curl -fsSL -o "$tmp/$name.tar.gz" "$base/$name.tar.gz" || fail "could not download $base/$name.tar.gz"
curl -fsSL -o "$tmp/checksums.txt" "$base/checksums.txt" || fail "could not download the checksums"

want=$(awk -v f="$name.tar.gz" '$2 == f { print $1 }' "$tmp/checksums.txt")
[ -n "$want" ] || fail "$name.tar.gz is not in checksums.txt"
if command -v sha256sum >/dev/null 2>&1; then
  got=$(sha256sum "$tmp/$name.tar.gz" | awk '{ print $1 }')
else
  got=$(shasum -a 256 "$tmp/$name.tar.gz" | awk '{ print $1 }')
fi
[ "$got" = "$want" ] || fail "checksum mismatch for $name.tar.gz"

tar -xzf "$tmp/$name.tar.gz" -C "$tmp"
mkdir -p "$dir"
cp "$tmp/$name/aka" "$dir/aka.new"
chmod 755 "$dir/aka.new"
mv -f "$dir/aka.new" "$dir/aka"
echo "Installed $("$dir/aka" --version) to $dir/aka"

case ":$PATH:" in
  *":$dir:"*) ;;
  *) echo "Add $dir to your PATH, for example: echo 'export PATH=\"$dir:\$PATH\"' >> ~/.profile" ;;
esac
echo "Next: run 'aka trust' once, then 'aka doctor'."
