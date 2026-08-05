# the-go-os — SPEC

Decision log from design grilling. This is the product contract until revised explicitly.

**Status:** frozen (initial)  
**v0 focus:** initful containers  
**North star:** Go userspace + systemd-shaped supervision, expanding toward host-grade parity

---

## 1. Vision

A Linux userspace centered on **one multicall Go binary** (plus symlinks): shell, core utilities, and a systemd-like service manager.

- **Kernel + this binary** is enough to boot a useful environment.
- **Primary early delivery:** initful containers (`FROM` this image; our binary is PID 1).
- **Later:** VMs, diskless initramfs, full host/desktop (endgame).

Not a second u-root. Toolbox inspiration from busybox/u-root; differentiation is **unit-file supervision, socket activation, and admin CLI** on a path toward systemd parity.

---

## 2. Compatibility north star

| Horizon | Target | Meaning |
|---------|--------|---------|
| **Strategy** | Subset now → expand | Allowlisted unit keys use **systemd semantics**. Unknown keys are rejected (not silently ignored). No permanent parallel dialect. |
| **Start (public spine)** | **P2** | Unit runtime for a growing allowlist **plus** admin CLI muscle memory (`systemctl` / later `journalctl`-class). |
| **Endgame** | **P4** | Host/desktop-grade: logind, user instances, generators, security hooks, replace systemd on a real machine. |

### Architecture rules (non-negotiable)

1. The **unit engine must be correct without D-Bus**.
2. Admin CLI talks to PID 1 over a **private control channel** first.
3. D-Bus, varlink façades, logind, etc. are **adapters** on the road to P3/P4 — not load-bearing for PID 1.
4. Every v0 feature should still make sense on the path to P4 (prefer designs that can grow cgroups, sessions, generators later).

### Explicit non-goals for v0

- Full bash drop-in as `/bin/sh`
- D-Bus systemd API
- Persistent journald/SQLite log store
- Unit types beyond the v0 allowlist (until expanded)
- QEMU/VM as an equal-priority deliverable
- logind / user managers / generators (P4 work)

---

## 3. Shell

**Model: S2 (dual)**

| Path | Implementation | Promise |
|------|----------------|---------|
| `/bin/sh` (multicall `sh` / gosh-class) | [`mvdan.cc/sh`](https://github.com/mvdan/sh) inside the binary | POSIX / sh-safe scripts — **not** bash |
| `/bin/bash` | Optional foreign static binary (image profile) | Real bash when the profile ships it |

- Unit default for shell forms: `/bin/sh -c` → mvdan semantics.
- Want bash: unit or image must provide `/bin/bash` explicitly.
- Minimal **container profile** may omit bash; host/dev profile may add it.

---

## 4. Runtime

| Phase | Target |
|-------|--------|
| **v0** | **Initful containers** — our binary is PID 1 |
| **Later** | VMs / diskless initramfs / bare-metal or appliance images |

Same supervisor and unit engine everywhere; container is the product loop, not a fork of the design.

### v0 success sketch

```text
container starts → PID 1 = us → embedded default.target
  → example.socket + example.service (socket activation)
  → unit output appears on init stderr (attributed)
  → systemctl-lite status/start/stop/restart over control socket
  → zombies reaped; restart policies work for allowlisted keys
```

---

## 5. Units

### 5.1 v0 types (U2)

| Included in v0 | Expand later (examples) |
|----------------|-------------------------|
| `.service` | `.timer`, `.path`, `.mount`, `.slice`, … |
| `.socket` | richer socket modes (`Accept=yes`, …) |
| `.target` | generators, dependencies sophistication |

Expansion means **growing the allowlist toward P2/P4**, not inventing incompatible keys.

### 5.2 v0 key allowlist (initial)

Same semantics as systemd for these keys only. Refine in implementation PRs; do not silently ignore unknowns.

**Common `[Unit]` (service / socket / target as applicable):**  
`Description`, `After`, `Before`, `Requires`, `Wants`, `Conflicts`, `BindsTo`, `PartOf`

**`[Install]`:**  
`WantedBy` (enable story may be simplified in v0; still parse)

**`[Service]`:**  
`Type=simple|oneshot`  
`ExecStart`, `ExecStartPre`, `ExecStop`  
`Restart=no|on-failure|always`, `RestartSec`  
`Environment`, `EnvironmentFile`, `WorkingDirectory`  
`SyslogIdentifier` (maps into log attribution)

**`[Socket]`:**  
`ListenStream`, `ListenDatagram`  
`Accept=no` (v0)  
`Service=` (optional unit name binding)

### 5.3 v0 non-goals (units)

- `Type=forking|notify|dbus`
- `Accept=yes` (inetd-style per-connection spawning)
- Full sandbox stack (`PrivateTmp`, `ProtectSystem`, …) — later
- User/Group beyond trivial/root-only stub — later
- Drop-in merge (`foo.service.d/*.conf`) — later
- Generators

### 5.4 Preship and override

| Layer | Role |
|-------|------|
| **Embed** | Factory defaults (`//go:embed`), always available (diskless / single-binary / default container) |
| **Filesystem** | Add units; **replace** same name |
| **On replace** | **Warn** (load-time: which unit, embed vs path) |
| **Merge** | No drop-in merge in v0; replace-by-name only |

**Preferred paths** (systemd-shaped for P2 muscle memory):

```text
embedded:   pkg/units/embedunits/**   (//go:embed)
on disk:    /etc/systemd/system/      (opt-in via THE_GO_OS_LOAD_SYSTEM_UNITS=1)
extra:      THE_GO_OS_UNIT_PATH       (optional directory)
runtime:    /run/systemd/system/      (optional later)
```

v0 default is **embed-only** so a host with a full systemd `/etc` does not pollute the unit set. Containers that ship units under `/etc/systemd/system` should set `THE_GO_OS_LOAD_SYSTEM_UNITS=1`. FS unit files that use keys outside the allowlist are **skipped with a warning** (embed remains strict).

---

## 6. Binary model and applet registry

### 6.1 Always multicall

- One ship binary + **symlinks** generated from the registry.
- Foreign **static** programs may be added to images later; dynamic linking is optional/later.

### 6.2 Dual front-end

| Invocation | Behavior |
|------------|----------|
| Primary binary name + args | **Cobra** CLI (`the-go-os systemctl status …`, help, subcommands) |
| Symlink / `argv0` equals registered applet | **Multicall dispatch** to that handler (BusyBox-style) |

Cobra `RunE` and multicall dispatch call the **same** handler implementation.

### 6.3 Registry

| Piece | Decision |
|-------|----------|
| Location | **`pkg`** (e.g. `pkg/registry`) |
| Registration | Each applet module registers in **`init()`** |
| Wiring | **Blank imports** pull definitions in so `init` runs |
| Uses | Dispatch, Cobra command tree, **symlink generation** for images |

### 6.4 Applet layout by role

```text
pkg/registry/                 # Register, Lookup, Symlinks, Cobra glue
pkg/applets/
  coreutils/                  # ls, cat, … — may reuse upstream (e.g. u-root) behind Register
  systemd/                    # systemctl-lite, init-facing admin tools; journalctl later
  shell/                      # sh / mvdan; bash remains external/optional
  # later: net/, debug/, …
cmd/<main>/
  main.go                     # argv0 multicall vs cobra
  profile_*.go                # blank-import sets (container vs full)
```

**Profiles** = different blank-import sets controlling image size, not a second registry.

### 6.5 Process boundaries (v0)

- PID 1 runs the **supervisor loop** only; do not run arbitrary applets in-process inside PID 1.
- Unit `ExecStart=` is **exec of a path** (symlink or foreign binary), not in-process registry calls.
- Interactive applets run as multicall children (or external).

---

## 7. Control plane

| Piece | Decision |
|-------|----------|
| Transport | **Unix domain socket** (e.g. `/run/the-go-os/control.sock`) |
| IDL | **Protocol Buffers** |
| Wire | Framed protobuf messages on the socket (**not** gRPC as a v0 requirement) |
| Clients | `systemctl` and other admin applets **dial** the socket (including when multicall children of the same binary) |
| Later façades | Optional D-Bus / varlink / gRPC **adapters** over the same engine |

### Tooling

- All codegen and related Go tools are managed with **`go tool`** and invoked via **`go generate`**.
- Control-plane schema lives in `.proto`; generated Go is the boundary — no divergent hand-rolled shadow types.

### v0 RPC surface (minimum)

- `ListUnits`
- `Start` / `Stop` / `Restart`
- `Status`
- `DaemonReload` (re-read units: embed + filesystem)

Subscribe/streaming events can wait until needed.

---

## 8. Logging (v0)

| Piece | Decision |
|-------|----------|
| Persistent store | **None** in v0 |
| Capture | Init/supervisor takes output from **children** (unit stdout/stderr) |
| Emit | Format as journal-style **entries** and write to **init’s stderr** |
| Container logs | Runtime collects PID 1 stderr as usual |
| Later | Same capture path feeds a store + `journalctl` over the control API |

### Entry format (v0)

Stable, human-readable prefixed lines are enough. Exact grammar can be fixed in implementation; must include at least:

- unit name (when known)
- stream (`stdout` / `stderr`)
- payload line

Machine-friendly JSON-lines on stderr is optional later, not required for v0.

### Non-goals v0

- SQLite / journald on-disk store
- Full `journalctl` query UX
- Guaranteed non-interleaved multi-writer semantics beyond “supervisor serializes emit”

---

## 9. Repository conventions

| Area | Convention |
|------|------------|
| Layout | Classic Go: `cmd/` thin main, `pkg/` registry + applets + engine, `internal/` for non-public guts as needed |
| Modules | One main module unless multi-module proves necessary |
| Generate | `go generate` + `go tool` for protobuf and any other codegen |
| Units in-tree | Embeddable defaults under a clear tree (e.g. `embed/units/`) |
| Spec changes | Update this file when decisions change; do not silently diverge in code |

---

## 10. First milestone (implementation order)

1. `cmd` + `pkg/registry` + one role package + blank-import profile  
2. Multicall + Cobra + symlink list generation  
3. Protobuf control API + UDS server in PID 1 + client in `systemctl` applet  
4. Embed `default.target` + sample `.socket` / `.service`  
5. Container: PID 1, socket-activate demo service, attributed lines on init stderr  
6. `systemctl`-lite: status / start / stop / restart / reload  

---

## 11. Open implementation details (not product forks)

These may be decided in code without a SPEC revision unless they change user-visible contracts:

- Exact main module path and binary name  
- Exact control socket path and protobuf package names  
- Exact stderr log line grammar  
- How much coreutils is reimplemented vs reused from upstream  
- cgroup usage in v0 (none vs minimal)  
- Enable/disable persistence model for `[Install]`  

---

## 12. Document history

| Date | Change |
|------|--------|
| 2026-08-04 | Initial freeze from design grilling |
