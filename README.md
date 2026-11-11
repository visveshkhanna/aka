# aka

Give any local dev server a stable named URL, whatever the language or
framework. `aka` works out the port by itself.

```sh
aka                                    # run this project's dev command
aka myapp python manage.py runserver   # https://myapp.localhost:1355
aka uvicorn main:app --reload          # name inferred from the directory
aka ng serve                           # Angular, name from the directory
aka api -- php -S 127.0.0.1:{port}     # explicit {port} placeholder
sudo aka proxy start                   # once: URLs without a port, https://myapp.localhost
```

`aka` is a working name. All user-visible names (binary, `AKA_` env prefix,
`~/.aka` state dir, default port) are constants in
[`internal/brand/brand.go`](internal/brand/brand.go).

## Install

```sh
go install github.com/visveshkhanna/aka/cmd/aka@latest
```

Or from a checkout:

```sh
git clone https://github.com/visveshkhanna/aka && cd aka
go build -o bin/aka ./cmd/aka
```

It needs Go 1.26 or later to build; the result is one static binary with no
runtime dependencies. Then, once:

```sh
aka trust      # add the local CA to the system trust store, for HTTPS
aka doctor     # check the setup
```

## Status

| Platform | State |
|---|---|
| macOS | Tested: HTTP, HTTPS, presets, `aka up`, compose, the login service |
| Linux | Tested on Ubuntu, Debian, Alpine and Oracle Linux, including `aka trust` and `sudo aka proxy start` |
| Windows | Built and unit-tested, but **not yet run on Windows**; see [Windows](#windows) |

Everything described below is implemented.

## How it works

1. A background daemon (started on first use) runs a reverse proxy on
   `127.0.0.1:1355` and `[::1]:1355` that routes by `Host` header and speaks
   both HTTP and HTTPS on that port. If it may bind port 443, it serves
   HTTPS there too. `*.localhost` resolves to loopback without any DNS
   setup.
2. `aka <name> <cmd>` registers `<name>.localhost` with the daemon and gets
   a free port P in 4000-4999. It runs `<cmd>` with `PORT=P`,
   `HOST=127.0.0.1` and `AKA_URL=https://<name>.localhost[:1355]`. Any
   `{port}` in the arguments is replaced with P.
3. While the command runs, `aka` watches its process tree for TCP sockets in
   LISTEN state (lsof on macOS, `/proc` on Linux) and scans its output for
   URLs like `http://localhost:8000`. The route points at:
   - P, if the app honoured it;
   - else the printed URL's port, if it is listening;
   - else the first listening port that answers HTTP.

   Detection runs for the whole life of the app, so restarts and port
   changes are followed.
4. When the command exits, the route is removed. If `aka` itself is killed,
   the daemon notices the dropped connection, removes the route, and stops
   the orphaned app.

Before spawning, **presets** add the flags or env vars that make common
dev servers listen on P (see below). So two Django apps, or two Angular
apps, run side by side instead of fighting over 8000 or 4200. Apps with no
preset that ignore `PORT` are still found by detection.

## Bare `aka` and app names

With no command, `aka` looks for the project's dev command, from the current
directory up to the repository root (or, outside a repository, up to just
below your home directory), and runs it in the directory where it found it:

| Found | Runs |
|---|---|
| `package.json` script `dev`, else `serve`, else `start` | `<pm> run <script>` (pm from `packageManager` or the lock file) |
| `angular.json` without scripts | `npx ng serve` |
| `manage.py` | `uv run python` / `poetry run python` / `.venv/bin/python` / `python3` `manage.py runserver` |
| `Procfile.dev` or `Procfile` `web:` | that line, via `sh -c` |
| `Gemfile` + `bin/dev` or `bin/rails` | `bin/dev` / `bin/rails server` |
| `artisan` + `composer.json` | `php artisan serve` |
| `mix.exs` with Phoenix | `mix phx.server` |
| `pom.xml` / `build.gradle` with Spring Boot | `./mvnw spring-boot:run` / `./gradlew bootRun` |
| `*.csproj` | `dotnet watch run` |
| `hugo.toml`, `mkdocs.yml` | `hugo server`, `mkdocs serve` |
| `Cargo.toml`, `go.mod` with `package main` | `cargo run`, `go run .` |
| `Makefile` with `dev`, `run` or `serve` | `make dev` |

If several match (say a Django backend with a `package.json` for assets),
`aka` asks once in a terminal and remembers the answer in
`~/.aka/projects.json`. Nothing is written into your repository. Use
`aka --choose` to pick again. A non-interactive run takes the first one and
lists the others.

The app name comes from the same walk: the nearest of `package.json`
(`@scope/` dropped), `pyproject.toml`, `Cargo.toml`, `go.mod` (last path
segment, `/v2` dropped), `composer.json`, `pom.xml`, `settings.gradle`,
`mix.exs`, `*.csproj` and `angular.json`. Failing those, it is the repository
root's name, then the directory's.

## Several apps: `aka.toml` and `aka up`

```toml
# aka.toml, at the project root
[apps]
web = "npm run dev"
api = "uvicorn main:app --reload"

[apps.worker]            # a table for more settings
cmd = "python -m worker"
dir = "services/worker"  # relative to aka.toml
port_hint = 9000         # optional, like --port-hint
no_preset = false        # optional, like --no-preset
```

`aka up` (or a bare `aka` anywhere in the project) runs every app at once,
each with its own URL (`web.localhost`, `api.localhost`), preset and
detection, and their output prefixed by name. `aka up web api` runs some
of them. Ctrl+C stops them all. A command that uses shell syntax
(`a && b`, `$VAR`, `>`) runs through `sh -c`, or `cmd /c` on Windows.

## Docker Compose

```sh
aka compose            # docker compose up, plus a URL per published service
aka compose -f dev.yml up --build
aka compose up -d      # containers in the background; aka routes until Ctrl+C
```

Every running service with a published TCP port gets
`<service>.<project>.localhost`, for example `web.shop.localhost` to the
published port of `web`. With several ports, the first one that answers
HTTP is used. Routes follow services as they start and stop.

## AI coding agents (MCP)

`aka mcp` is a Model Context Protocol server on stdio. It lets an agent find
the URL of the app it is working on instead of guessing a port:

```sh
claude mcp add aka -- aka mcp
```

Tools: `list_apps`, `get_url`, `add_alias`, `remove_alias`, `doctor`.

## Start at login

```sh
aka service install    # launchd agent (macOS), systemd user unit (Linux), Run key (Windows)
aka service status
aka service uninstall
```

No administrator rights are needed. The service restarts the proxy only
if it crashes, so `aka proxy stop` holds until the next login. It runs as
you, so on macOS it serves HTTPS on 1355. For port 443 use
`sudo aka proxy start` instead.

## Worktrees

In a checkout made with `git worktree add`, on a branch other than `main` or
`master`, the inferred name gets the branch's last segment as a prefix:
`feature/login` in a worktree of `shop` becomes `login.shop.localhost`. Two
checkouts can then run at once. The main checkout never gets a prefix.
Explicit names (`aka web ...`) are used as given. `AKA_PREFIX=staging` sets
the prefix yourself; `AKA_PREFIX=` turns it off.

## Aliases, list and doctor

```sh
aka alias db 5432            # https://db.localhost -> a port aka did not start
aka alias api 127.0.0.1:8080 # or a loopback address
aka alias                    # list aliases
aka alias rm db
aka list                     # running apps and aliases (--json for scripts)
aka doctor                   # check everything and say what to fix
```

Aliases are saved in `~/.aka/aliases.json` and restored with the proxy. A
running app cannot take an alias's name, even with `--force`.

`aka doctor` checks:
- the state directory;
- the proxy: running, current build, HTTPS mode, port 443, reachability on
  both loopbacks;
- the CA: constraints, expiry, system trust, and Firefox/NSS;
- `*.localhost` resolution;
- every route, through the proxy;
- the current project: name, dev command and the preset it would use.

It exits 1 if something fails.

**Host rejections.** When an app answers 400 or 403 because the framework
rejects the `*.localhost` host or origin, the aka running that app prints
the setting to change. This covers Django `ALLOWED_HOSTS` and CSRF origins,
Vite `allowedHosts`, webpack-dev-server, Rails `config.hosts` and ASP.NET
`AllowedHosts`. `doctor` reports the same.

**Stopping the proxy.** `aka proxy stop` stops routing, but running apps
keep running. Each one says so and re-registers when the proxy starts again
(`aka proxy start` or the next app). If the proxy crashes instead, apps
restart it right away.

## Presets

[`internal/presets/presets.json`](internal/presets/presets.json) maps
commands to what sets their port:

| Ecosystem | Commands | Added |
|---|---|---|
| Angular | `ng serve` | `--port P --host 127.0.0.1` |
| Vite family | `vite`, `vitepress`, `astro dev`, `react-router dev`, `rsbuild dev` | `--port P [--strictPort] --host 127.0.0.1` |
| Others (JS) | `wrangler dev`, `expo start`, `react-native start` | `--port P` + host flag |
| Django | `manage.py runserver`, `django-admin runserver` | `127.0.0.1:P` |
| uvicorn, FastAPI | `uvicorn app:app`, `fastapi dev` | `--port P --host 127.0.0.1` |
| gunicorn | `gunicorn app:app` | `-b 127.0.0.1:P` |
| Flask, Streamlit, Gradio | `flask run`, `streamlit run`, any python script | `FLASK_RUN_PORT`, `STREAMLIT_SERVER_PORT`, `GRADIO_SERVER_PORT` |
| Jupyter, MkDocs, stdlib | `jupyter lab`, `mkdocs serve`, `python -m http.server` | `--port=P`, `-a 127.0.0.1:P`, `P --bind 127.0.0.1` |
| Rails, Jekyll | `rails s`, `jekyll serve` | `-p P -b 127.0.0.1`, `--port P --host 127.0.0.1` |
| PHP, Laravel | `php -S`, `php artisan serve` | `127.0.0.1:P`, `--port=P --host=127.0.0.1` |
| Spring Boot, Quarkus, ASP.NET | `mvn`, `gradle(w)`, `java -jar`, `quarkus dev`, `dotnet run/watch` | `SERVER_PORT`, `QUARKUS_HTTP_PORT`, `ASPNETCORE_URLS` |
| Hugo | `hugo server` | `--port P --bind 127.0.0.1` |

Next.js, Nuxt, Express, Phoenix, Go, Rust, Deno and Bun apps read `PORT`
and need nothing.

The rules:
- Wrappers are looked past first: `npx`, `bunx`, `pnpm exec/dlx`,
  `yarn exec/dlx`, `npm exec`, `poetry/uv/pipenv/pdm/hatch/rye run`, `uvx`,
  `pipx run`, `bundle exec`, `cross-env`, `python -m mod`,
  `python script.py`, `php artisan`.
- `npm|pnpm|yarn|bun run <script>` looks the script up in `package.json`
  and forwards the flags (`npm run dev -- --port P`). Compound scripts
  (`a && b`, `a & b`, `a | b`, a trailing comment) are left alone.
- Only server subcommands get flags: never `vite build`, `ng test` or
  `manage.py migrate`. A flag you already passed (`--port 5000`) is not
  added again. Env vars you have set win over the preset's.
- A `{port}` in the command, or `--no-preset`, turns presets off for that run.

Add or override presets in `~/.aka/presets.json`, in the same format. A
user preset replaces a built-in one with the same `name`.

## HTTPS

On first use, `aka` creates a local certificate authority in `~/.aka`
(`ca.pem`, `ca-key.pem`). During the TLS handshake it issues a certificate
for each hostname. The first HTTPS run in a terminal offers to add the CA to
the system trust store. macOS asks you to confirm; Linux runs sudo. You can
also do this yourself with `aka trust`, and undo it with `aka untrust`.

The CA is **name-constrained**: it can only sign `localhost` and
`*.localhost` names and loopback IPs. Even if its key leaked, it could not
be used to impersonate other sites.

| Where | How the CA is trusted |
|---|---|
| macOS | login keychain (Safari, Chrome, curl, Go) |
| Linux | distro anchor dir + `update-ca-certificates` / `update-ca-trust` (Debian, Ubuntu, Alpine, Fedora, RHEL, Arch, openSUSE) |
| Firefox, Chrome on Linux | NSS databases, if `certutil` is installed |
| Node | `NODE_EXTRA_CA_CERTS` is set for the app, so server-side `fetch` of other `*.localhost` apps works |

**Port 443.** Binding 443 needs privileges on macOS (for loopback
addresses) and on Linux. Without them, HTTPS is served on port 1355 and the
URL carries the port. To get `https://myapp.localhost` with no port, run:

```sh
sudo aka proxy start     # binds 443, then drops root and runs as you
```

On Linux you can instead let the binary bind it:
`sudo setcap cap_net_bind_service=+ep $(command -v aka)`.

`AKA_HTTPS=0` turns HTTPS off (plain `http://<name>.localhost:1355`).

**Django** builds CSRF origins from the scheme it sees, which is http behind
the proxy, so POSTs (the admin login, for one) fail with "Origin checking
failed". Add this to the development settings:

```python
SECURE_PROXY_SSL_HEADER = ("HTTP_X_FORWARDED_PROTO", "https")
```

uvicorn/Starlette, Rails and Express (with `trust proxy`) read
`X-Forwarded-Proto` themselves.

## Windows

Windows 10 1803 or later (for the Unix-socket control channel). What differs:

- **Detection** reads the process table from a Toolhelp32 snapshot and
  LISTEN sockets from `GetExtendedTcpTable`.
- **Stopping apps:** Ctrl+C reaches the app directly, as without aka, since
  it shares the console. aka puts the app in a job object, so a takeover, a
  third Ctrl+C, or aka itself dying stops the whole tree.
- **Trust:** the CA goes into the current user's Root store (no admin rights;
  Windows asks you to confirm). `aka untrust` removes it.
- **Port 443** needs no privileges on Windows, so URLs have no port unless
  something else holds 443.
- **Output** goes through pipes, not a pseudo-console, so some tools drop
  colours.
- **Name resolution:** browsers and curl resolve `*.localhost` themselves,
  but the Windows resolver does not. So Node, Python and similar clients
  calling `https://api.localhost` from code need a hosts-file entry.

**Untested:** this code is cross-compiled, vetted and its parsers are
unit-tested on macOS and Linux, but it has not yet been run on Windows.

## Usage

```
aka                            run this project's dev command (or aka.toml's apps)
aka up [app...]                run the apps listed in aka.toml together
aka compose [args...]          docker compose up, with <service>.<project>.localhost URLs
aka <name> <command...>        run command, route <name>.localhost to it
aka <command...>               same, name inferred from the project
aka <name> -- <command...>     explicit name
aka list [--json]              show active routes and aliases
aka alias <name> <port>        route <name>.localhost to a port aka did not start
aka alias [rm <name>]          list or remove aliases
aka doctor                     check the setup and explain what is wrong
aka mcp                        MCP server on stdio, for AI coding agents
aka service install|uninstall|status   start the proxy at login
aka proxy start|stop|status    manage the proxy daemon
sudo aka proxy start           also serve port 443 (root is dropped after binding)
aka trust | untrust            add or remove the local CA in the system trust store

Flags (before the name):
  --name, -n <name>   app name (when it would be mistaken for a command)
  --force, -f         take over a name another aka is using
  --port-hint <port>  route to this port instead of detecting one
  --no-preset         do not add port flags or env for known frameworks
  --choose            pick again which dev command a bare aka runs
```

The first word is the app name if it is not a command on your PATH. Use
`--name` or `--` when an app is named like a command (`aka --name test ...`).

Environment: `AKA_PROXY_PORT` (default 1355), `AKA_HTTPS_PORT` (default 443,
`0` for none), `AKA_HTTPS=0` (plain HTTP only), `AKA_PREFIX` (worktree
prefix; empty turns it off), `AKA_STATE_DIR` (default `~/.aka`),
`AKA_NO_PTY=1` (capture output with pipes, not a pseudo-terminal).

## Build and test

```sh
go build -o bin/aka ./cmd/aka
go test -race ./...
```

The binary is static (no cgo) and cross-compiles with `GOOS`/`GOARCH`. Set
the version with
`-ldflags "-X github.com/visveshkhanna/aka/internal/cli.Version=v0.1.0"`.

### Testing on Linux

From macOS, cross-compile the test binaries and run them in any Linux
container:

```sh
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go test -c -o out/ ./...
docker run --rm -v "$PWD/out:/t:ro" alpine sh -c 'cd /tmp; for t in /t/*.test; do $t || exit 1; done'
```

Run as root (the default in most images), this also exercises the
privilege drop used by `sudo aka proxy start`.

## Credits

The design follows [portless](https://github.com/vercel-labs/portless) by
Vercel Labs (Apache-2.0). Parts of the proxy, port picker and process
handling are translated from its TypeScript sources. See [NOTICE](NOTICE) and
[third_party/portless/LICENSE](third_party/portless/LICENSE).

## License

`aka` does not have a license of its own yet. Until it does, the code here
is not licensed for reuse; the portless-derived parts remain under
Apache-2.0 as described in [NOTICE](NOTICE).
