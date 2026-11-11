# Usage

- [Running an app](#running-an-app)
- [Bare aka](#bare-aka)
- [App names](#app-names)
- [Worktrees](#worktrees)
- [Several apps: aka.toml and aka up](#several-apps-akatoml-and-aka-up)
- [Docker Compose](#docker-compose)
- [Aliases and list](#aliases-and-list)
- [The proxy and the login service](#the-proxy-and-the-login-service)
- [Doctor](#doctor)
- [Command reference](#command-reference)
- [Environment variables](#environment-variables)

## Running an app

```bash
aka myapp python manage.py runserver   # name, then command
aka uvicorn main:app --reload          # command only: the name is inferred
aka api -- php -S 127.0.0.1:{port}     # explicit name; {port} is replaced
```

The command runs with:

| Variable | Value |
|---|---|
| `PORT` | a free port from 4000 to 4999 |
| `HOST` | `127.0.0.1` |
| `AKA_URL` | the app's public URL, such as `https://myapp.localhost:1355` |

The first word is taken as the app name if it is not a command on your `PATH`. When an app is named like a command, use `--name` or `--`:

```bash
aka --name test npm run test:server
aka test -- npm run test:server
```

Flags go before the name:

| Flag | Meaning |
|---|---|
| `--name`, `-n <name>` | the app name |
| `--force`, `-f` | take over a name another aka is using, and stop that app |
| `--port-hint <port>` | route to this port instead of detecting one |
| `--no-preset` | do not add port flags or variables for known frameworks |
| `--choose` | pick again which dev command a bare `aka` runs |

## Bare aka

With no command, aka looks for the project's dev command from the current directory up to the repository root (outside a repository, up to just below your home directory), and runs it in the directory where it found it:

| Found | Runs |
|---|---|
| `package.json` script `dev`, else `serve`, else `start` | `<pm> run <script>`, with the package manager from `packageManager` or the lock file |
| `angular.json` without scripts | `npx ng serve` |
| `manage.py` | `manage.py runserver` through `uv run`, `poetry run`, the project's `.venv` or `python3` |
| `Procfile.dev` or `Procfile` with a `web:` line | that line, through the shell |
| `Gemfile` with `bin/dev` or `bin/rails` | `bin/dev` or `bin/rails server` |
| `artisan` with `composer.json` | `php artisan serve` |
| `mix.exs` using Phoenix | `mix phx.server` |
| `pom.xml` or `build.gradle` using Spring Boot | `./mvnw spring-boot:run` or `./gradlew bootRun` |
| `*.csproj` | `dotnet watch run` |
| `hugo.toml`, `mkdocs.yml` | `hugo server`, `mkdocs serve` |
| `Cargo.toml`, `go.mod` with `package main` | `cargo run`, `go run .` |
| `Makefile` with a `dev`, `run` or `serve` target | `make <target>` |

If several match, such as a Django backend with a `package.json` for its assets, aka asks once in a terminal and remembers the answer in `~/.aka/projects.json`. Nothing is written into your repository. `aka --choose` asks again. A run without a terminal takes the first match and lists the others.

If the project has an [`aka.toml`](#several-apps-akatoml-and-aka-up), a bare `aka` runs its apps instead.

## App names

An inferred name comes from the same walk up the directory tree: the nearest of `package.json` (without its `@scope/`), `pyproject.toml`, `Cargo.toml`, `go.mod` (the last path segment, without `/v2`), `composer.json`, `pom.xml`, `settings.gradle`, `mix.exs`, `*.csproj` and `angular.json`. Failing those, it is the repository's name, then the directory's.

Inferred names are lowercased, and anything other than letters and digits becomes a dash. A name you give must already be letters, digits and dashes. A name with dots is a subdomain: `aka api.shop npm start` serves `https://api.shop.localhost`.

## Worktrees

In a checkout made with `git worktree add`, on a branch other than `main` or `master`, the inferred name gets the branch's last segment as a prefix. `feature/login` in a worktree of `shop` becomes `login.shop.localhost`, so two checkouts of the same project run at once. The main checkout never gets a prefix, and explicit names are used as given.

`AKA_PREFIX=staging` sets the prefix yourself. `AKA_PREFIX=` (empty) turns it off.

## Several apps: aka.toml and aka up

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

`aka up` runs every app at once, each with its own URL (`web.localhost`, `api.localhost`), preset and port detection, and their output prefixed by name. `aka up web api` runs some of them. Ctrl+C stops them all.

A command that uses shell syntax (`a && b`, `$VAR`, `>`) runs through `sh -c`, or `cmd /c` on Windows. aka looks for `aka.toml` from the current directory up to the repository root. The file accepts strings, integers, booleans, tables and comments, and any other TOML is an error with a line number, so a typo never silently drops an app.

## Docker Compose

```bash
aka compose                         # docker compose up, plus a URL per published service
aka compose -f dev.yml up --build   # arguments go to docker compose
aka compose up -d                   # containers in the background; aka routes until Ctrl+C
```

Every running service with a published TCP port gets `<service>.<project>.localhost`, for example `web.shop.localhost` for the published port of `web`. With several published ports, the first that answers HTTP is used. Routes follow services as they start and stop.

## Aliases and list

An alias routes a name to a port aka did not start, such as a database UI or a server started by another tool:

```bash
aka alias db 5432             # https://db.localhost -> 127.0.0.1:5432
aka alias api 127.0.0.1:8080  # or a loopback address
aka alias                     # list aliases
aka alias rm db
```

Aliases are saved in `~/.aka/aliases.json` and come back when the proxy restarts. A running app cannot take an alias's name, even with `--force`.

`aka list` shows running apps and aliases. `aka list --json` prints them for scripts, with each route's URL, port, process and start time.

## The proxy and the login service

The proxy starts by itself the first time an app runs. To manage it directly:

```bash
aka proxy start     # start it (with sudo: also bind port 443, then drop root)
aka proxy status
aka proxy stop
```

`aka proxy stop` stops routing, but running apps keep running. Each one says so, and registers again when a proxy starts (`aka proxy start`, or the next app). If the proxy crashes instead, apps restart it right away. A proxy left over from an older aka is restarted automatically when it has no routes.

To start the proxy at login:

```bash
aka service install    # a launchd agent (macOS), systemd user unit (Linux), or Run key (Windows)
aka service status
aka service uninstall
```

No administrator rights are needed. launchd and systemd restart the proxy if it crashes, but `aka proxy stop` holds until the next login. Since it runs as you, it cannot bind port 443 on macOS or Linux; use `sudo aka proxy start` for URLs without a port.

## Doctor

`aka doctor` checks:

- the state directory;
- the proxy: running, current build, HTTPS mode, port 443, and reachability on both loopback addresses;
- the certificate authority: constraints, expiry, system trust, and Firefox and other NSS databases;
- `*.localhost` name resolution;
- every route, through the proxy;
- the current project: its name, its dev command and the preset aka would use.

It says how to fix each problem, and exits with status 1 if a check fails.

When an app answers 400 or 403 because its framework rejects the `.localhost` host or origin, the aka running that app prints the setting to change. This covers Django `ALLOWED_HOSTS` and CSRF origins, Vite `allowedHosts`, webpack-dev-server, Rails `config.hosts` and ASP.NET `AllowedHosts`. `doctor` reports the same.

## Command reference

```
aka                            run this project's dev command (or aka.toml's apps)
aka <name> <command...>        run command, route <name>.localhost to it
aka <command...>               same, name inferred from the project
aka <name> -- <command...>     explicit name ({port} in the command is replaced)
aka up [app...]                run the apps listed in aka.toml together
aka compose [args...]          docker compose up, with <service>.<project>.localhost URLs
aka list [--json]              show running apps and aliases
aka alias <name> <port>        route <name>.localhost to a port aka did not start
aka alias [rm <name>]          list or remove aliases
aka proxy start|stop|status    manage the proxy
sudo aka proxy start           also serve port 443 (root is dropped after binding)
aka service install|uninstall|status   start the proxy at login
aka trust | untrust            add or remove the local CA in the system trust store
aka doctor                     check the setup and explain what is wrong
aka mcp                        MCP server on stdio, for AI coding agents
aka --version
```

## Environment variables

| Variable | Default | Meaning |
|---|---|---|
| `AKA_PROXY_PORT` | `1355` | the proxy's port, for HTTP and HTTPS |
| `AKA_HTTPS_PORT` | `443` | the dedicated HTTPS port to try; `0` for none |
| `AKA_HTTPS` | on | `0` serves plain HTTP only |
| `AKA_PREFIX` | from the branch | the worktree prefix; empty turns it off |
| `AKA_STATE_DIR` | `~/.aka` | where the CA, routes, aliases and logs are kept |
| `AKA_NO_PTY` | off | `1` captures app output through pipes instead of a pseudo-terminal |

aka sets `PORT`, `HOST`, `AKA_URL` and `AKA_PORT` for the app, `NODE_EXTRA_CA_CERTS` when HTTPS is on, and `PYTHONUNBUFFERED` when output goes through pipes.
