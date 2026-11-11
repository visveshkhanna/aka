# Contributing

Thanks for helping with aka. Bug reports, presets for more frameworks and fixes are all welcome. For a larger change, open an issue first to agree on the approach.

## Setup

aka is plain Go with no cgo, and needs Go 1.26 or later.

```bash
git clone https://github.com/visveshkhanna/aka && cd aka
go build -o bin/aka ./cmd/aka
./bin/aka --version
```

To try a change without touching your own proxy, give it a separate state directory and port:

```bash
AKA_STATE_DIR=/tmp/aka-dev AKA_PROXY_PORT=13550 ./bin/aka web npm run dev
```

## Layout

| Path | What it does |
|---|---|
| `cmd/aka` | the entry point |
| `internal/cli` | commands and flags |
| `internal/runner` | runs an app: environment, output, signals, port detection loop |
| `internal/daemon` | the proxy daemon and its control socket |
| `internal/proxy` | the reverse proxy, HTTP/HTTPS sharing one port, error pages |
| `internal/detect` | listening sockets and process trees, per OS |
| `internal/presets` | framework presets (`presets.json`) and command unwrapping |
| `internal/project` | name inference and dev command detection |
| `internal/certs`, `internal/trust` | the local CA and the system trust stores |
| `internal/service` | launchd, systemd and Windows login services |
| `internal/brand` | every user-visible name: binary, `AKA_` prefix, `~/.aka`, ports |
| `e2e` | end-to-end test of the real binary |

`aka` is a working name. Renaming the tool means editing `internal/brand/brand.go` and the `cmd/aka` directory, and nothing else.

## Tests

```bash
go test -race ./...           # unit and integration tests
go test -tags e2e ./e2e -v    # the real binary with real Node and Python servers
```

The end-to-end test needs `node` and `python3` on `PATH`. It uses its own state directory and proxy port, so it does not disturb a proxy you are running.

Some tests only run as root on Linux (the privilege drop behind `sudo aka proxy start`). From macOS, cross-compile the test binaries and run them in a container:

```bash
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go test -c -o out/ ./...
docker run --rm -v "$PWD/out:/t:ro" alpine sh -c 'cd /tmp; for t in /t/*.test; do $t || exit 1; done'
```

Before sending a change, run `gofmt -l .` (it should print nothing) and `go vet ./...`, and for platform code also `GOOS=windows go vet ./...` and `GOOS=linux go vet ./...`.

## CI

Every push and pull request runs [CI](.github/workflows/ci.yml):

- **Lint**: gofmt, `go mod tidy`, `go vet` for Linux, macOS, Windows and FreeBSD, govulncheck, shellcheck and actionlint.
- **Test**: unit tests, with the race detector where Go supports it, and the end-to-end test, on Linux, macOS and Windows, each on x64 and arm64.
- **Release build** of every target.
- **Trust store**: `aka trust` and `aka untrust` as root in Ubuntu, Debian, Fedora, Rocky Linux, Alpine, Arch and openSUSE containers.
- **Port 443**: `sudo aka proxy start`, checking that root is dropped.
- **Login service**: install, crash restart and uninstall under launchd, systemd and the Windows Run key.

The scripts behind the last three are in [`.github/scripts`](.github/scripts). Failing tests are posted as annotations on the run's summary page. [CodeQL](.github/workflows/codeql.yml) scans the code weekly and on every change, and Dependabot keeps Go modules and actions up to date.

## Releases

Releases follow [semantic versioning](https://semver.org). To publish one:

1. Move the entries under `Unreleased` in [CHANGELOG.md](CHANGELOG.md) to a new version section, such as `## [0.2.0] - 2026-12-01`, and commit.
2. Tag the commit and push the tag:

   ```bash
   git tag -a v0.2.0 -m "aka v0.2.0"
   git push origin v0.2.0
   ```

The [release workflow](.github/workflows/release.yml) tests the tag, builds archives for macOS, Linux and Windows with [`.github/scripts/dist.sh`](.github/scripts/dist.sh), attests their build provenance, and publishes a GitHub release with that version's changelog section as its notes. A tag with a suffix, such as `v0.2.0-rc.1`, is published as a pre-release. Running the workflow by hand builds the archives without publishing.

## Credits

Parts of aka are translated from [portless](https://github.com/vercel-labs/portless) (Apache-2.0). Code adapted from it must keep its attribution; see [NOTICE](NOTICE).
