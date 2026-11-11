# Contributing to aka

Thanks for helping with aka. Bug reports, presets for more frameworks, docs fixes and code are all welcome. This guide covers how to get set up, how to make a change, and what a pull request needs.

Everyone taking part is expected to follow the [code of conduct](CODE_OF_CONDUCT.md). To report a security problem, follow the [security policy](SECURITY.md) instead of opening an issue.

- [Finding something to do](#finding-something-to-do)
- [Setting up](#setting-up)
- [Making a change](#making-a-change)
- [Checklist for pull requests](#checklist-for-pull-requests)
- [Code style](#code-style)
- [Tests](#tests)
- [Documentation](#documentation)
- [Adding a preset](#adding-a-preset)
- [CI](#ci)
- [Releases](#releases)

## Finding something to do

- Issues labelled [`good first issue`](https://github.com/visveshkhanna/aka/labels/good%20first%20issue) and [`help wanted`](https://github.com/visveshkhanna/aka/labels/help%20wanted) are a good start.
- A dev server that aka gets wrong is worth a [preset](#adding-a-preset).
- Running aka on Windows, or on a Linux distribution CI does not cover, and reporting what breaks, helps a lot.

For anything larger than a small fix, open an issue or a [discussion](https://github.com/visveshkhanna/aka/discussions) first, so we can agree on the approach before you spend time on it.

## Setting up

aka is plain Go with no cgo, and needs Go 1.26 or later.

```bash
git clone https://github.com/<you>/aka && cd aka
go build -o bin/aka ./cmd/aka
./bin/aka --version
```

To try a change without touching the proxy you use day to day, give it its own state directory and port:

```bash
AKA_STATE_DIR=/tmp/aka-dev AKA_PROXY_PORT=13550 ./bin/aka web npm run dev
```

### Layout

| Path | What it does |
|---|---|
| `cmd/aka` | the entry point |
| `internal/cli` | commands and flags |
| `internal/runner` | runs an app: environment, output, signals, the port detection loop |
| `internal/daemon` | the proxy daemon and its control socket |
| `internal/proxy` | the reverse proxy, HTTP and HTTPS on one port, error pages |
| `internal/detect` | listening sockets and process trees, per OS |
| `internal/presets` | framework presets (`presets.json`) and command unwrapping |
| `internal/project` | name inference and dev command detection |
| `internal/certs`, `internal/trust` | the local CA and the system trust stores |
| `internal/service` | launchd, systemd and Windows login services |
| `internal/brand` | every user-visible name: the binary, the `AKA_` prefix, `~/.aka`, default ports |
| `e2e` | the end-to-end test of the real binary |
| `website` | the docs site at [aka.visvesh.dev](https://aka.visvesh.dev) |

## Making a change

1. Fork the repository and create a branch from `main`, named after the change: `fix-windows-detection`, `preset-remix`.
2. Make the change, with tests. Keep each commit to one logical change that builds and passes the tests on its own.
3. Add a line to the `Unreleased` section of [CHANGELOG.md](CHANGELOG.md) if users will notice the change, under `Added`, `Changed`, `Fixed` or `Removed`.
4. Update the [docs](#documentation) if behaviour, a command or a flag changed.
5. Run the checks in the [checklist](#checklist-for-pull-requests), push, and open a pull request against `main`.

### Commit messages

- A subject line in the imperative, under about 72 characters, without a trailing period: `Detect listeners of grandchild processes on Windows`.
- A body, after a blank line, that says why the change is needed and anything a reviewer would not see from the diff.
- One logical change per commit. Formatting-only changes go in their own commit.

## Checklist for pull requests

- [ ] `gofmt -l .` prints nothing.
- [ ] `go vet ./...` passes, and for platform code also `GOOS=windows go vet ./...`, `GOOS=linux go vet ./...` and `GOOS=darwin go vet ./...`.
- [ ] `go test -race ./...` passes, with new tests for new behaviour and for fixed bugs.
- [ ] `go test -tags e2e ./e2e` passes, if the change affects running apps, the proxy or HTTPS.
- [ ] User-facing changes have a `CHANGELOG.md` entry and updated docs.
- [ ] Code adapted from portless keeps its attribution in [NOTICE](NOTICE).

CI runs all of these, and more, on every pull request.

## Code style

- Format with `gofmt`. Follow [Effective Go](https://go.dev/doc/effective_go) and the [Go code review comments](https://go.dev/wiki/CodeReviewComments).
- No cgo, and no new dependencies without a good reason: aka ships as one static binary.
- Use the names in `internal/brand` (`brand.Name`, `brand.Env("URL")`, `brand.StateDir()`) instead of writing `aka`, `AKA_` or `~/.aka` in code. Renaming the tool should mean editing that one file.
- Error messages start in lower case, have no trailing period, and say what to do next where possible: `the proxy is running without port 443 and has apps attached; stop them, then run this again`.
- Messages to the user from a running app go through the `notice` helper, so they get the `[aka]` prefix.
- Platform code lives in files with build constraints (`listen_windows.go`, `signals_unix.go`), with an `_other.go` fallback that compiles everywhere.
- Never signal a process aka did not start. Processes are tracked by PID and start time for that reason.
- Comments explain why. Exported names have doc comments.

## Tests

```bash
go test -race ./...           # unit and integration tests
go test -tags e2e ./e2e -v    # the real binary with real Node and Python servers
```

- Tests must not touch your real setup: use `t.TempDir()` and an isolated `AKA_STATE_DIR` and proxy port, as the existing tests do.
- Prefer table-driven tests. No test may need network access beyond loopback.
- The end-to-end test needs `node` and `python3` on `PATH`.

Some tests only run as root on Linux, such as the privilege drop behind `sudo aka proxy start`. From macOS, cross-compile the test binaries and run them in a container:

```bash
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go test -c -o out/ ./...
docker run --rm -v "$PWD/out:/t:ro" alpine sh -c 'cd /tmp; for t in /t/*.test; do $t || exit 1; done'
```

## Documentation

The docs at [aka.visvesh.dev](https://aka.visvesh.dev) are built from [`website/content/docs`](website/content/docs), one MDX file per page. Every merge to `main` deploys them. To work on them locally, with Node.js 20 or later:

```bash
cd website
npm ci
npm run dev           # http://localhost:3000
npm run build && npm run check-links
```

In MDX, write `<name>` and `{port}` inside code spans, or they are read as JSX. The changelog page and `/install.sh` are copied from the repository root at build time; edit `CHANGELOG.md` and `install.sh`, not their copies.

## Adding a preset

A preset tells aka how to set the port of a dev server that ignores `PORT`.

1. Add it to [`internal/presets/presets.json`](internal/presets/presets.json). The [presets page](https://aka.visvesh.dev/docs/presets) explains the format.
2. Add cases to `TestCommands` in [`internal/presets/presets_test.go`](internal/presets/presets_test.go): the server command gets the flags, and a non-server command such as a build is left alone.
3. Add the framework to the table on the [presets page](website/content/docs/presets.mdx).
4. If you can, run it for real with `aka <name> <command>` and check the app answers on its URL.

## CI

Every push and pull request runs [CI](.github/workflows/ci.yml):

- **Lint**: gofmt, `go mod tidy`, `go vet` for Linux, macOS, Windows and FreeBSD, govulncheck, shellcheck and actionlint.
- **Test**: unit tests, with the race detector where Go supports it, and the end-to-end test, on Linux, macOS and Windows, each on x64 and arm64.
- **Docs site**: type check, build and link check.
- **Release build** of every target.
- **Trust store**: `aka trust` and `aka untrust` as root in Ubuntu, Debian, Fedora, Rocky Linux, Alpine, Arch and openSUSE containers.
- **Port 443**: `sudo aka proxy start`, checking that root is dropped.
- **Login service**: install, crash restart and uninstall under launchd, systemd and the Windows Run key.

The scripts behind the last three are in [`.github/scripts`](.github/scripts). Failing tests are posted as annotations on the run's summary page. [CodeQL](.github/workflows/codeql.yml) scans the code on every change and weekly, and Dependabot keeps Go modules, npm packages and actions up to date.

## Releases

Releases follow [semantic versioning](https://semver.org). Maintainers publish one by:

1. Moving the entries under `Unreleased` in [CHANGELOG.md](CHANGELOG.md) to a new version section, such as `## [0.2.0] - 2026-12-01`, and committing.
2. Tagging the commit and pushing the tag:

   ```bash
   git tag -a v0.2.0 -m "aka v0.2.0"
   git push origin v0.2.0
   ```

The [release workflow](.github/workflows/release.yml) tests the tag, builds archives for macOS, Linux and Windows with [`.github/scripts/dist.sh`](.github/scripts/dist.sh), attests their build provenance, and publishes a GitHub release with that version's changelog section as its notes. A tag with a suffix, such as `v0.2.0-rc.1`, is published as a pre-release. Running the workflow by hand builds the archives without publishing.

## License

By contributing, you agree that your contributions are licensed under the [Apache License 2.0](LICENSE).

Parts of aka are translated from [portless](https://github.com/vercel-labs/portless) (Apache-2.0). Code adapted from it must keep its attribution in [NOTICE](NOTICE).
