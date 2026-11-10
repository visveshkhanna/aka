# aka

Give any local dev server a stable named URL, whatever the language or
framework. `aka` works out the port by itself.

```sh
aka myapp python manage.py runserver   # http://myapp.localhost:1355
aka uvicorn main:app --reload          # name inferred from the directory
aka ng serve                           # Angular
aka api -- php -S 127.0.0.1:{port}     # explicit {port} placeholder
```

`aka` is a working name. All user-visible names (binary, `AKA_` env prefix,
`~/.aka` state dir, default port) are constants in
[`internal/brand/brand.go`](internal/brand/brand.go).

> **Status: MVP (build step 1).** HTTP only, on port 1355, macOS and Linux.
> HTTPS, presets, full name inference, Windows, `alias` and `doctor` are
> still to come.

## How it works

1. A background daemon (started on first use) runs a reverse proxy on
   `127.0.0.1:1355` and `[::1]:1355` that routes by `Host` header.
   `*.localhost` resolves to loopback without any DNS setup.
2. `aka <name> <cmd>` registers `<name>.localhost` with the daemon and gets
   a free port P in 4000-4999. It runs `<cmd>` with `PORT=P`,
   `HOST=127.0.0.1` and `AKA_URL=http://<name>.localhost:1355`. Any `{port}`
   in the arguments is replaced with P.
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

So apps that ignore `PORT` and bind a default port (Django 8000, Angular
4200, uvicorn 8000) still get routed correctly. Two such apps with the same
hardcoded port still collide; presets (build step 3) will fix that by
injecting flags.

## Usage

```
aka <name> <command...>        run command, route <name>.localhost to it
aka <command...>               same, name inferred from the directory
aka <name> -- <command...>     explicit name
aka list                       show active routes
aka proxy start|stop|status    manage the proxy daemon

Flags (before the name):
  --name, -n <name>   app name (when it would be mistaken for a command)
  --force, -f         take over a name another aka is using
  --port-hint <port>  route to this port instead of detecting one
```

The first word is the app name if it is not a command on your PATH. Use
`--name` or `--` when an app is named like a command (`aka --name test ...`).

Environment: `AKA_PROXY_PORT` (default 1355), `AKA_STATE_DIR` (default
`~/.aka`), `AKA_NO_PTY=1` (capture output with pipes, not a pseudo-terminal).

## Build and test

```sh
go build -o bin/aka ./cmd/aka
go test -race ./...
```

Requires Go 1.26+. The binary is static (no cgo) and cross-compiles with
`GOOS`/`GOARCH`.

### Testing on Linux

From macOS, cross-compile the test binaries and run them in any Linux
container:

```sh
for p in detect daemon proxy ports runner; do
  GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go test -c -o out/$p.test ./internal/$p
done
docker run --rm -v "$PWD/out:/t:ro" alpine sh -c 'cd /tmp; for t in /t/*.test; do $t; done'
```

## Credits

The design follows [portless](https://github.com/vercel-labs/portless) by
Vercel Labs (Apache-2.0). Parts of the proxy, port picker and process
handling are translated from its TypeScript sources. See [NOTICE](NOTICE) and
[third_party/portless/LICENSE](third_party/portless/LICENSE). This project
has no license of its own yet.
