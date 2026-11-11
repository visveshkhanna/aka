#!/usr/bin/env bash
# Prints the release notes for a version: its section of CHANGELOG.md,
# followed by install instructions. Fails if the section is missing, so a
# tag is never released without notes.
#
#   .github/scripts/release-notes.sh v0.1.0 > notes.md
set -euo pipefail
tag=${1:?usage: release-notes.sh TAG}
version=${tag#v}
repo=${GITHUB_REPOSITORY:-visveshkhanna/aka}

notes=$(awk -v v="$version" '
  index($0, "## [" v "]") == 1 { found = 1; next }
  found && /^## \[/ { exit }
  found && /^\[[^]]+\]: / { next }
  found { print }
' CHANGELOG.md)
if [ -z "$(printf '%s' "$notes" | tr -d '[:space:]')" ]; then
  echo "CHANGELOG.md has no section for $version" >&2
  exit 1
fi

printf '%s\n' "$notes" | sed -e :a -e '/^\n*$/{$d;N;ba' -e '}'
cat <<NOTES

## Install

macOS and Linux:

\`\`\`bash
curl -fsSL https://aka.visvesh.dev/install.sh | AKA_VERSION=$tag sh
\`\`\`

Windows: download \`aka_${version}_windows_amd64.zip\` (or \`arm64\`) below and put \`aka.exe\` on your \`PATH\`.

With Go: \`go install github.com/$repo/cmd/aka@$tag\`

Every archive is listed in \`checksums.txt\`, and its build provenance can be checked with \`gh attestation verify <file> --repo $repo\`.
NOTES
