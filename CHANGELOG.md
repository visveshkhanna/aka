# Changelog

All notable changes to aka are recorded here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow [semantic versioning](https://semver.org).

## [Unreleased]

## [0.1.1] - 2026-11-11

### Fixed

- `aka --version`, `aka doctor` and the MCP server report the release version for binaries built with `go install`, instead of `dev`.

## [0.1.0] - 2026-11-11

The first release.

### Run apps

- `aka <name> <command>` gives any dev server a stable `https://<name>.localhost` URL, with a free port passed through `PORT`.
- Port detection follows apps that ignore `PORT`, through their listening sockets and printed URLs, for as long as they run.
- Presets set the port for about 30 dev servers that ignore `PORT`, including Angular, Vite, Django, uvicorn, FastAPI, gunicorn, Flask, Rails, Laravel, Spring Boot and ASP.NET, looking through wrappers such as `npx`, `uv run` and `npm run`. Your own presets go in `~/.aka/presets.json`.
- A bare `aka` finds and runs the project's dev command across about 15 ecosystems, and infers the app name from the project.
- In a git worktree, the branch becomes a prefix: `login.shop.localhost`.

### Several apps

- `aka.toml` and `aka up` run several apps together, each with its own URL.
- `aka compose` runs Docker Compose and gives each published service a URL.
- `aka alias` routes a name to a port aka did not start.

### HTTPS

- HTTPS with HTTP/2 from a local, name-constrained certificate authority that can only sign `*.localhost` and loopback names.
- `aka trust` and `aka untrust` for the macOS keychain, Linux distribution trust stores, the Windows user Root store and NSS databases.
- `sudo aka proxy start` serves port 443 and then drops root.

### Tools

- `aka doctor` checks the setup and explains each problem.
- Apps print the setting to change when Django, Vite, Rails, webpack-dev-server or ASP.NET reject the `.localhost` host.
- `aka mcp`, a Model Context Protocol server for coding agents.
- `aka service install` starts the proxy at login with launchd, systemd or the Windows Run key.
- `aka list --json` for scripts.

### Platforms

- macOS, Linux and Windows on x64 and arm64, and 32-bit ARM Linux, as single static binaries.

[Unreleased]: https://github.com/visveshkhanna/aka/compare/v0.1.1...HEAD
[0.1.1]: https://github.com/visveshkhanna/aka/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/visveshkhanna/aka/releases/tag/v0.1.0
