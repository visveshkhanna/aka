# aka

Give any local dev server a stable named URL, whatever the language or
framework. `aka` works out the port by itself.

```sh
aka myapp python manage.py runserver   # https://myapp.localhost:1355
aka uvicorn main:app --reload          # name inferred from the directory
aka ng serve                           # Angular, name from the directory
aka api -- php -S 127.0.0.1:{port}     # explicit {port} placeholder
sudo aka proxy start                   # once: URLs without a port, https://myapp.localhost
```

`aka` is a working name. All user-visible names (binary, `AKA_` env prefix,
`~/.aka` state dir, default port) are constants in
[`internal/brand/brand.go`](internal/brand/brand.go).

> **Status: build step 2 of 7.** HTTP and HTTPS with a local CA, macOS and
> Linux. Presets, full name inference, Windows, `alias` and `doctor` are
> still to come.

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

So apps that ignore `PORT` and bind a default port (Django 8000, Angular
4200, uvicorn 8000) still get routed correctly. Two such apps with the same
hardcoded port still collide; presets (build step 3) will fix that by
injecting flags.

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

## Usage

```
aka <name> <command...>        run command, route <name>.localhost to it
aka <command...>               same, name inferred from the directory
aka <name> -- <command...>     explicit name
aka list                       show active routes
aka proxy start|stop|status    manage the proxy daemon
sudo aka proxy start           also serve port 443 (root is dropped after binding)
aka trust | untrust            add or remove the local CA in the system trust store

Flags (before the name):
  --name, -n <name>   app name (when it would be mistaken for a command)
  --force, -f         take over a name another aka is using
  --port-hint <port>  route to this port instead of detecting one
```

The first word is the app name if it is not a command on your PATH. Use
`--name` or `--` when an app is named like a command (`aka --name test ...`).

Environment: `AKA_PROXY_PORT` (default 1355), `AKA_HTTPS_PORT` (default 443,
`0` for none), `AKA_HTTPS=0` (plain HTTP only), `AKA_STATE_DIR` (default
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
for p in certs cli daemon detect lockfile ports privdrop proxy runner trust; do
  GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go test -c -o out/$p.test ./internal/$p
done
docker run --rm -v "$PWD/out:/t:ro" alpine sh -c 'cd /tmp; for t in /t/*.test; do $t; done'
```

Run as root (the default in most images), this also exercises the
privilege drop used by `sudo aka proxy start`.

## Credits

The design follows [portless](https://github.com/vercel-labs/portless) by
Vercel Labs (Apache-2.0). Parts of the proxy, port picker and process
handling are translated from its TypeScript sources. See [NOTICE](NOTICE) and
[third_party/portless/LICENSE](third_party/portless/LICENSE). This project
has no license of its own yet.
