# Agent Rules

Rules for coding agents working in this repository. People should read [CONTRIBUTING.md](CONTRIBUTING.md), which covers the same ground in more detail.

## Project

aka gives any local dev server a stable `https://<name>.localhost` URL. It is a single static Go binary: a CLI that runs the user's dev command, and a background proxy daemon that routes `*.localhost` to the right port. It runs on macOS, Linux and Windows, on x64 and arm64.

| Path | What it does |
|---|---|
| `cmd/aka` | the entry point |
| `internal/cli` | commands, flags and `aka --help` (`usage` in `cli.go`) |
| `internal/runner` | runs an app: environment, output, signals, the port detection loop |
| `internal/daemon` | the proxy daemon and its control socket protocol |
| `internal/proxy` | the reverse proxy, HTTP and HTTPS on one port, error pages |
| `internal/detect` | listening sockets and process trees, one file per OS |
| `internal/presets` | `presets.json` and command unwrapping |
| `internal/project` | name inference and dev command detection |
| `internal/certs`, `internal/trust` | the local CA and the system trust stores |
| `internal/service` | launchd, systemd and Windows login services |
| `internal/mcp`, `internal/cli/mcp.go` | the MCP server and its tools |
| `internal/brand` | every user-visible name and default |
| `e2e` | end-to-end test of the real binary (build tag `e2e`) |
| `website` | the docs site at https://aka.visvesh.dev (Next.js, Fumadocs, Cloudflare Workers) |
| `.github/scripts` | CI and release scripts |

## Commands

```bash
go build -o bin/aka ./cmd/aka
go test -race ./...
go test -tags e2e ./e2e              # needs node and python3
gofmt -l .                           # must print nothing
for os in linux darwin windows; do GOOS=$os go vet ./...; done
cd website && npm ci && npm run build && npm run check-links
```

Run the `go vet` loop for every change: most bugs that CI catches late are code that builds on one OS and not another.

## Dependencies

- No cgo, ever. The binary must stay static and cross-compile with `GOOS`/`GOARCH`.
- Do not add Go modules without a strong reason; the standard library and `golang.org/x` cover almost everything.
- In `website/`, use `npm` (not pnpm or yarn). Check the latest version with `npm view <package> version` before adding one.

## Brand

`aka` is a working name. Every user-visible name lives in `internal/brand`: use `brand.Name`, `brand.Env("URL")`, `brand.StateDir()`, `brand.DefaultProxyPort` and so on. Never write `"aka"`, `"AKA_"` or `".aka"` as literals in Go code. Docs and the website may use the names directly.

## Safety rules

- Never signal or kill a process aka did not start. Processes are tracked by PID and start time; keep it that way.
- The proxy binds loopback only (`127.0.0.1` and `::1`) and routes only to loopback.
- Nothing runs as root except binding the port in `sudo aka proxy start`, which drops privileges right after.
- The local CA must stay name-constrained to `localhost`, `*.localhost` and loopback addresses.
- Tests must never touch the real setup: use `t.TempDir()`, a separate `AKA_STATE_DIR` and a free proxy port. Never run `aka trust` or `aka service install` against the developer's real system to test something.

## Platforms

- Platform code goes in files with build constraints (`listen_windows.go`, `signals_unix.go`), with an `_other.go` fallback that compiles everywhere.
- Windows has no `sh`, no signals between consoles, and uses job objects to stop process trees. Use `presets.Shell` for shell syntax, and `.exe` names for fake executables in tests.
- On Windows, `os.UserHomeDir` reads `USERPROFILE`, not `HOME`. Tests that fake a home directory set both.
- There is no Windows machine for local testing. Push a branch and let CI run it, then read failures with `gh run view <id> --log-failed`; failing tests are also posted as annotations.
- Tests that need root on Linux: cross-compile with `GOOS=linux CGO_ENABLED=0 go test -c -o out/ ./...` and run them in a container (see CONTRIBUTING.md).

## Docs Updates

When a change affects how people or agents use aka (a command, flag, environment variable, behaviour or file), update all of these in the same change:

1. `aka --help` (`usage` in `internal/cli/cli.go`)
2. The pages in `website/content/docs`: usually the guide for the feature, `commands.mdx` and `configuration.mdx`
3. `README.md`, if it covers the feature
4. The MCP tools in `internal/cli/mcp.go`, if agents need the new behaviour
5. `CHANGELOG.md`, under `Unreleased` (`Added`, `Changed`, `Fixed` or `Removed`)

Do not edit `website/content/docs/changelog.mdx` or `website/public/install.sh`: they are generated from `CHANGELOG.md` and `install.sh` at build time.

In MDX, put `<name>`, `{port}` and anything else with angle brackets or braces inside code spans, or MDX reads it as JSX.

## Presets

A dev server that ignores `PORT` gets a preset in `internal/presets/presets.json`, with cases in `TestCommands` (`internal/presets/presets_test.go`) for both the server command and a non-server command such as a build, and a row in the table in `website/content/docs/presets.mdx`.

## Writing Style

- No emojis anywhere: code, comments, output, docs. Test data that reproduces a tool's real output (such as Vite's `➜`) is the only exception.
- No em dashes, and no `--` used as a dash, in prose, comments or output. Rephrase instead. CLI flags such as `--port` are fine.
- Document boolean environment variables with `0` and `1` only (`AKA_HTTPS=0`, `AKA_NO_PTY=1`), even where the code accepts more.
- Error messages start in lower case, have no trailing period, and say what to do next where possible.
- Plain, direct prose in docs: short sentences, active voice, no marketing language.

## Commits

- One logical change per commit, and every commit builds and passes the tests on its own.
- An imperative subject under about 72 characters, without a trailing period, then a blank line and a body that says why.
- Never add `Co-Authored-By:` trailers, "Generated with" lines or any other AI attribution to commit messages or pull request descriptions.
- Do not push, tag or force-push unless asked.

## Releasing

Releases are cut by the maintainer:

1. Move the `Unreleased` entries in `CHANGELOG.md` to a new section, `## [X.Y.Z] - YYYY-MM-DD`, update the link definitions at the bottom, and commit (`Prepare the X.Y.Z release`).
2. `git tag -a vX.Y.Z -m "aka vX.Y.Z"` and push the tag.

The release workflow fails early if the changelog has no section for the tag. It builds the archives with `.github/scripts/dist.sh`, attests their provenance, and publishes the GitHub release with that changelog section as its notes. The docs site redeploys from `main` on its own.
