#!/usr/bin/env bash
# Builds the release archives: one static binary per platform, packed with
# the README and licence notices, plus SHA-256 checksums.
#
#   .github/scripts/dist.sh v1.2.3 [out-dir]
set -euo pipefail
version=${1:?usage: dist.sh VERSION [OUT]}
out=${2:-dist}
module=$(go list -m)
targets="darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 linux/arm windows/amd64 windows/arm64"

rm -rf "$out"
mkdir -p "$out"
for t in $targets; do
  os=${t%/*} arch=${t#*/}
  name="aka_${version#v}_${os}_${arch}"
  [ "$arch" = arm ] && name+="v7"
  dir="$out/$name"
  mkdir -p "$dir"
  exe=aka
  [ "$os" = windows ] && exe=aka.exe
  echo "building $name"
  CGO_ENABLED=0 GOOS=$os GOARCH=$arch GOARM=7 go build -trimpath \
    -ldflags "-s -w -X $module/internal/cli.Version=$version" \
    -o "$dir/$exe" ./cmd/aka
  cp README.md LICENSE NOTICE "$dir/"
  mkdir -p "$dir/third_party/portless"
  cp third_party/portless/LICENSE "$dir/third_party/portless/"
  if [ "$os" = windows ]; then
    (cd "$out" && zip -qr "$name.zip" "$name")
  else
    tar -C "$out" -czf "$out/$name.tar.gz" "$name"
  fi
  rm -rf "$dir"
done
sha=(sha256sum)
command -v sha256sum >/dev/null || sha=(shasum -a 256) # macOS
(cd "$out" && "${sha[@]}" ./*.tar.gz ./*.zip | sed 's# \./# #' > checksums.txt)
cat "$out/checksums.txt"
