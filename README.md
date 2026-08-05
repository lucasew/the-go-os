# the-go-os

Single multicall Go userspace: PID 1 unit supervisor, coreutils-ish applets, POSIX `sh` (mvdan), `systemctl` over a protobuf Unix socket.

**Product decisions:** see [SPEC.md](SPEC.md).

## Quick start

```bash
# tools + tasks: mise.toml (go, protoc)
mise install
mise run ci
mise run smoke

./bin/the-go-os list-applets
./bin/the-go-os echo hello

# Supervisor (embed units only by default)
export THE_GO_OS_CONTROL_SOCKET=/tmp/the-go-os.sock
./bin/the-go-os init &
./bin/the-go-os systemctl list-units
./bin/the-go-os systemctl status hello.socket
# socket-activate demo service:
nc 127.0.0.1 18080 </dev/null
./bin/the-go-os systemctl status hello.service
```

### Tasks

| Task | What |
|------|------|
| `mise run install` | `go mod tidy` + download |
| `mise run fmt` / `lint` / `test` / `build` | usual |
| `mise run generate` | protobuf via `go generate` + mise `protoc` |
| `mise run symlinks` | multicall links under `bin/applets/` |
| `mise run smoke` | build + list applets + echo |
| `mise run ci` | install → fmt → lint → test → build |
| `mise run clean` | remove `bin/` |

### Unit paths

| Source | When |
|--------|------|
| Embed (`pkg/units/embedunits`) | Always |
| `THE_GO_OS_UNIT_PATH=dir` | Optional extra unit directory |
| `/etc/systemd/system` | Only if `THE_GO_OS_LOAD_SYSTEM_UNITS=1` |

Filesystem units **replace** embed by name (with a warn). Unknown keys are rejected for embed; FS files that fail the allowlist are skipped with a warn.

## Layout

```
cmd/the-go-os/          # main + blank-import profiles
api/control/v1/         # protobuf IDL
pkg/registry/           # multicall + Cobra
pkg/applets/            # coreutils, systemd, shell (register in init)
pkg/supervisor/         # unit engine
pkg/units/              # parse/load + embedunits
pkg/control/            # framed protobuf RPC
```

## Generate

```bash
mise run generate
```

Uses mise-managed `protoc` and `go tool protoc-gen-go` (see `go.mod` tool stanza).
