# aka

Stable, named `.localhost` URLs for every local dev server, in any language or framework. aka works out the port by itself.

<p>
  <a href="https://github.com/visveshkhanna/aka/actions/workflows/ci.yml"><img alt="CI" src="https://img.shields.io/github/actions/workflow/status/visveshkhanna/aka/ci.yml?branch=main&amp;style=for-the-badge&amp;labelColor=000000&amp;label=CI" height="28"></a>
  <a href="https://github.com/visveshkhanna/aka/releases/latest"><img alt="Latest release" src="https://img.shields.io/github/v/release/visveshkhanna/aka?style=for-the-badge&amp;labelColor=000000&amp;sort=semver" height="28"></a>
  <a href="go.mod"><img alt="Go version" src="https://img.shields.io/github/go-mod/go-version/visveshkhanna/aka?style=for-the-badge&amp;labelColor=000000" height="28"></a>
  <img alt="Platforms: macOS, Linux, Windows" src="https://img.shields.io/badge/platforms-macOS%20%7C%20Linux%20%7C%20Windows-0a0a0a?style=for-the-badge&amp;labelColor=000000" height="28">
</p>

```diff
- python manage.py runserver          # http://127.0.0.1:8000
+ aka python manage.py runserver      # https://myapp.localhost:1355
```

aka runs a small reverse proxy on your machine and gives each dev server a name. Your app gets a free port, and you open `https://<name>.localhost` instead of remembering which one it took. The URL stays the same across restarts, so bookmarks, cookies, OAuth redirects and test scripts keep pointing at the right app, and two Django or two Angular projects run side by side instead of fighting over 8000 or 4200.

It is a single static binary with no runtime dependencies, and it works with any stack: Node, Python, Ruby, PHP, Go, Rust, Java, .NET, Elixir and more.

## Install

macOS and Linux:

```bash
curl -fsSL https://raw.githubusercontent.com/visveshkhanna/aka/main/install.sh | sh
```

The script downloads the latest [release](https://github.com/visveshkhanna/aka/releases), checks its SHA-256 checksum and installs `aka` into `~/.local/bin`. Set `AKA_INSTALL_DIR` to choose another directory, or `AKA_VERSION=v0.1.0` to pin a version.

Windows: download `aka_<version>_windows_amd64.zip` (or `arm64`) from the [latest release](https://github.com/visveshkhanna/aka/releases/latest) and put `aka.exe` on your `PATH`.

With Go 1.26 or later, on any platform:

```bash
go install github.com/visveshkhanna/aka/cmd/aka@latest
```

Then, once, trust the local certificate authority so browsers accept the HTTPS URLs, and check the setup:

```bash
aka trust
aka doctor
```

## Quick start

From a project directory, run aka with no arguments. It finds the dev command (`package.json` scripts, `manage.py`, `Procfile`, `bin/dev`, `mix.exs`, Spring Boot, `*.csproj` and [more](docs/usage.md#bare-aka)) and runs it:

```bash
aka
# -> https://<project>.localhost:1355
```

Or give a name and any command:

```bash
aka myapp npm run dev                 # https://myapp.localhost:1355
aka api uvicorn main:app --reload     # https://api.localhost:1355
aka ng serve                          # name inferred from the project
aka web -- php -S 127.0.0.1:{port}    # {port} is replaced with the assigned port
```

URLs carry `:1355` until the proxy can bind port 443. Start it once with `sudo` to drop the port (aka binds 443, then gives up root):

```bash
sudo aka proxy start                  # https://myapp.localhost
```

- The name comes from the nearest `package.json`, `pyproject.toml`, `Cargo.toml`, `go.mod` and similar, then the repository or directory name.
- A name with dots is a subdomain: `aka api.shop npm start` serves `https://api.shop.localhost`.
- In a `git worktree` on a branch, the branch becomes a prefix, so `login.shop.localhost` and `shop.localhost` run side by side.

## How it works

The proxy starts on first use and listens only on loopback (`127.0.0.1` and `::1`). `*.localhost` names resolve to loopback without any DNS setup.

Each app gets a free port from 4000 to 4999 through the `PORT` environment variable. Most frameworks read it. For those that don't, such as Vite, Angular, Django, uvicorn and Rails, aka adds the matching port flag or variable ([presets](docs/presets.md)). Apps that ignore both are still found: aka watches the process tree for listening sockets and the output for printed URLs, and routes to the port the app actually uses, for as long as it runs.

When the command exits, the route goes away. If aka itself is killed, the proxy notices and stops the app it left behind. More detail is in [How it works](docs/how-it-works.md).

## Several apps

List them in an `aka.toml` at the project root:

```toml
[apps]
web = "npm run dev"
api = "uvicorn main:app --reload"

[apps.worker]
cmd = "python -m worker"
dir = "services/worker"
```

`aka up` runs them all, each with its own URL (`web.localhost`, `api.localhost`) and its output prefixed by name. Ctrl+C stops them all. For Docker Compose, `aka compose` runs `docker compose up` and gives each published service a URL like `web.shop.localhost`. See [Usage](docs/usage.md#several-apps-akatoml-and-aka-up).

## For AI agents

`aka mcp` is a Model Context Protocol server over stdio, so a coding agent can look up the URL of the app it is working on instead of guessing a port:

```bash
claude mcp add aka -- aka mcp
```

```json
{ "mcpServers": { "aka": { "command": "aka", "args": ["mcp"] } } }
```

Tools: `list_apps`, `get_url`, `add_alias`, `remove_alias` and `doctor`.

## Commands

Run `aka --help` for the full list.

| Group | Commands | Docs |
|---|---|---|
| Run apps | `aka`, `aka <name> <cmd>`, `aka <cmd>`, `aka up` | [Usage](docs/usage.md) |
| Routes | `list`, `alias`, `compose` | [Aliases](docs/usage.md#aliases-and-list), [Docker Compose](docs/usage.md#docker-compose) |
| Proxy | `proxy start`, `proxy stop`, `proxy status`, `service install`, `service status`, `service uninstall` | [Proxy and login service](docs/usage.md#the-proxy-and-the-login-service) |
| HTTPS | `trust`, `untrust` | [HTTPS](docs/https.md) |
| Diagnose | `doctor` | [Doctor](docs/usage.md#doctor) |
| Agents | `mcp` | [For AI agents](#for-ai-agents) |

Run `aka doctor` first when a URL does not load. It checks the proxy, the certificate authority, name resolution and every route, and says what to fix. When a framework rejects the `.localhost` host (Django `ALLOWED_HOSTS`, Vite `allowedHosts`, Rails `config.hosts` and others), aka prints the setting to change in the app's terminal.

## Platforms

aka runs on macOS, Linux and Windows 10 1803 or later, on x64 and arm64. Every change is tested on all six combinations, and the trust store integration on Ubuntu, Debian, Fedora, Rocky Linux, Alpine, Arch and openSUSE. Platform notes, including what differs on Windows, are in [Platforms](docs/platforms.md).

## Documentation

- [Usage](docs/usage.md): running apps, names, `aka.toml`, Docker Compose, aliases, the proxy, environment variables
- [Presets](docs/presets.md): how aka sets the port for each framework, and how to add your own
- [HTTPS](docs/https.md): the local certificate authority, trust, port 443, framework settings
- [Platforms](docs/platforms.md): macOS, Linux and Windows specifics
- [How it works](docs/how-it-works.md): the proxy, port detection and process handling

## Development

```bash
go build -o bin/aka ./cmd/aka
go test -race ./...
go test -tags e2e ./e2e       # end-to-end, with real Node and Python servers
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for the test setup, CI and releases.

## Credits

aka follows the design of [portless](https://github.com/vercel-labs/portless) by Vercel Labs, and parts of its proxy, port picker and process handling are translated from portless's TypeScript sources (Apache-2.0). See [NOTICE](NOTICE).

## License

aka does not have a license of its own yet. Until it does, the code is not licensed for reuse. The portless-derived parts remain under the Apache License 2.0, as described in [NOTICE](NOTICE).
