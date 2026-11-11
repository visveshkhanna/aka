# Changelog

All notable changes to aka are recorded here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow [semantic versioning](https://semver.org).

## [Unreleased]

## [0.1.2] - 2026-11-11

### Added

- Documentation at [aka.visvesh.dev](https://aka.visvesh.dev), with search, and plain-text versions for coding agents at `/llms.txt`.
- A shorter install command: `curl -fsSL https://aka.visvesh.dev/install.sh | sh`.

### Changed

- aka is licensed under the Apache License 2.0. Release archives include the licence.

## [0.1.1] - 2026-11-11

### Fixed

- `aka --version`, `aka doctor` and the MCP server report the release version for binaries built with `go install`, instead of `dev`.

## [0.1.0] - 2026-11-11

The first release.

### Added

- `aka <name> <command>` gives any dev server a stable `https://<name>.localhost` URL, passing a free port through `PORT`.
- Port detection that follows apps which ignore `PORT`, through their listening sockets and printed URLs.
- Presets that set the port for about 30 dev servers, including Angular, Vite, Django, uvicorn, Rails, Laravel, Spring Boot and ASP.NET, looking through wrappers such as `npx`, `uv run` and `npm run`. Your own go in `~/.aka/presets.json`.
- A bare `aka` that finds and runs the project's dev command across about 15 ecosystems, and infers the app name.
- Worktree names: in a git worktree, the branch becomes a prefix, as in `login.shop.localhost`.
- `aka.toml` and `aka up` to run several apps together, `aka compose` for Docker Compose services, and `aka alias` for ports aka did not start.
- HTTPS with HTTP/2 from a local certificate authority that can only sign `.localhost` and loopback names, with `aka trust` and `aka untrust` for macOS, Linux, Windows and NSS trust stores.
- `sudo aka proxy start` to serve port 443, dropping root after binding.
- `aka doctor`, and hints in the app's terminal when Django, Vite, Rails, webpack-dev-server or ASP.NET reject the `.localhost` host.
- `aka mcp`, a Model Context Protocol server for coding agents.
- `aka service install` to start the proxy at login with launchd, systemd or the Windows Run key.
- `aka list --json` for scripts.
- Static binaries for macOS, Linux and Windows on x64 and arm64, and 32-bit ARM Linux.

[Unreleased]: https://github.com/visveshkhanna/aka/compare/v0.1.2...HEAD
[0.1.2]: https://github.com/visveshkhanna/aka/compare/v0.1.1...v0.1.2
[0.1.1]: https://github.com/visveshkhanna/aka/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/visveshkhanna/aka/releases/tag/v0.1.0
