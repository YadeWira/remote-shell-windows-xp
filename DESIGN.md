# Design: remote-shell-windows-xp

> **Status: implemented.** Phases 1–3 below (core exec, file ops, interactive
> shell/sysinfo/procops) are built — see `internal/`, `cmd/xpagent`,
> `cmd/xpctl`, and the README for usage. Two deliberate simplifications vs.
> the original plan, made while implementing:
> - The per-connection frame writer is a mutex around a single `Write`
>   rather than a writer-goroutine-plus-channel — same fairness in
>   practice (frame-granular, ~64KiB max), much less shutdown complexity.
> - File **download** (`get`) always starts at offset 0; only **upload**
>   (`put`) supports resume. Resuming a multi-GB upload to the XP box after
>   a network blip is the valuable case; symmetric download-resume was cut
>   to keep the checksum/offset protocol simpler and can be added later if
>   needed.
>
> Phases 4–5 (service install/lifecycle, guard rails) are not yet built.

A high-quality, SSH-like remote control system between a **Windows 11 controller**
("sender") and a **Windows XP agent** ("receiver") running in VirtualBox on the
same host-only network. Goal: full remote shell + file management, scriptable
enough that an AI coding agent can drive it directly as a CLI tool.

## 1. Constraints that drive the whole design

These are non-negotiable technical facts about the target, discovered before
writing any code, and they shape almost every decision below.

1. **Go 1.10.x is the last Go toolchain that can build binaries that run on
   Windows XP/Server 2003.** Go 1.11+ dropped XP support. So `xpagent` (the
   XP-side binary) must be cross-compiled with an archived `go1.10.8` toolchain,
   `GOOS=windows GOARCH=386` (XP is virtually always 32-bit; confirm the VM's
   bitness before building — amd64 XP exists but is rare).
2. **Go 1.10 has no module support** (Go modules shipped in 1.11). It only
   understands `GOPATH`. So the agent build cannot `go get` third-party
   dependencies. Consequence: **`xpagent` is stdlib-only, no exceptions.**
   Anything Win32-specific (process listing, service install, etc.) is done
   via raw `syscall.NewLazyDLL(...)` bindings to kernel32/advapi32, not
   `golang.org/x/sys/windows`. This also sidesteps GOPATH/module friction
   entirely — no vendoring, no shims beyond pointing GOPATH at the repo.
3. **Go 1.10's `crypto/tls` tops out at TLS 1.2** (TLS 1.3 landed later). That's
   fine — we pin `MinVersion: tls.VersionTLS12` on both ends anyway, so the
   modern `xpctl` side interoperates exactly.
4. **No ConPTY on XP** (introduced in Windows 10). There is no real pseudo-terminal.
   Interactive "shell" means proxying `cmd.exe`'s stdin/stdout/stderr pipes —
   great for line-oriented tools, but full-screen console apps (menus, cursor
   positioning) won't render correctly. This is an accepted v1 limitation.
5. **The two machines already share a private network**: the VirtualBox
   host-only adapter, `192.168.56.1/24` (visible in the README's `ipconfig`).
   Nothing here should ever need to touch the internet, and the agent should
   refuse to bind to anything but that adapter / localhost by default.

## 2. Architecture

```
                 host-only network (192.168.56.0/24)
  ┌────────────────────┐   TLS 1.2, mutual trust    ┌──────────────────────┐
  │   Windows 11 host   │ ─────────────────────────▶ │   Windows XP guest    │
  │   xpctl (client)    │ ◀───────────────────────── │   xpagentd (server)   │
  │   - CLI / scriptable│    single TCP connection,   │   - listens on        │
  │   - AI agent drives │    multiplexed by stream ID │     192.168.56.x:PORT │
  │     this directly   │                             │   - spawns cmd.exe    │
  └────────────────────┘                             │   - stdlib only       │
                                                       └──────────────────────┘
```

- **`xpagentd`** (receiver) runs on XP, listens on the host-only IP, accepts a
  TLS connection, authenticates it, and executes commands / file ops locally.
  It is the "server" in networking terms even though it's the *receiver* of
  control — this mirrors sshd and avoids needing NAT traversal or a
  reconnect-on-drop story, since the two machines are always on the same LAN.
- **`xpctl`** (sender) runs on Windows 11. It's a CLI, not a daemon: each
  invocation dials the agent, does one thing (run a command, transfer a file,
  list a directory, open an interactive shell), and exits. This is exactly
  the shape an AI agent (or a human) already knows how to drive via a shell —
  see §7.

## 3. Security model

- **Transport**: TLS 1.2, AEAD cipher suites only (`ECDHE-{RSA,ECDSA}-AES{128,256}-GCM-SHA256`).
  The agent holds a self-signed ECDSA P-256 cert generated on first run
  (`xpagentd -init` writes `agent.crt`/`agent.key` next to the binary).
- **Trust-on-first-use pinning**: `xpctl pair <ip:port> --token <token>`
  connects once, records the agent cert's SHA-256 fingerprint into a local
  `known_agents.json` (like SSH's `known_hosts`), and stores the shared
  bearer token. Every subsequent connection verifies the fingerprint before
  sending anything.
- **Bearer token auth inside the channel**: after the TLS handshake, the
  client sends a `HELLO{token}` frame; the agent compares it (constant-time)
  against its configured token and drops the connection on mismatch. This is
  simpler than full mTLS while still being strong, since TLS already gives
  confidentiality/integrity and the pinning prevents MITM. Full client-cert
  mTLS is a documented v2 hardening option, not required for v1.
- **Bind safety**: `xpagentd` refuses to listen on `0.0.0.0` unless started
  with an explicit `--insecure-bind-all` flag. Default is the host-only
  adapter IP or `127.0.0.1`.
- **Audit log**: every executed command and file operation is appended to a
  local log on the agent (`audit.log`: timestamp, stream id, verb, args,
  exit code) *before* running it. This matters specifically because the
  stated end goal is letting an AI agent run arbitrary commands unsupervised
  — there should always be a durable record of what actually ran on the box.
- **Guard rails (optional, v2)**: an opt-in denylist of destructive patterns
  (`format`, `del /s /q C:\`, etc.) the agent refuses without a `--force`
  flag from the client. Off by default so it doesn't get in the way of "full
  control," but available for anyone who wants a safety net.

## 4. Wire protocol

Single TCP connection, framed messages, multiplexed by a stream ID so
multiple logical operations (an exec session, a file upload, a heartbeat)
can share one connection without blocking each other:

```
[4 bytes] uint32 BE  payload length
[1 byte]            message type
[4 bytes] uint32 BE  stream id (correlates request/response, 0 = connection-level)
[payload]           JSON for control messages; raw bytes for data chunks
```

A single writer goroutine per connection drains a channel fed by all active
streams, so large file transfers don't starve exec/heartbeat frames for more
than one chunk's transmission time (default chunk = 64 KiB). No priority
queueing in v1 — usage is expected to be mostly sequential, not deeply
concurrent.

Message types:

| Code | Name | Direction | Notes |
|---|---|---|---|
| 0x01 | HELLO | C→S / S→C | version + token / accept-reject |
| 0x02/0x03 | PING/PONG | either | idle heartbeat |
| 0x10 | EXEC_START | C→S | `{cmd, args, cwd, interactive}` |
| 0x11/0x12 | EXEC_STDOUT/STDERR | S→C | raw bytes |
| 0x13 | EXEC_STDIN | C→S | raw bytes, interactive only |
| 0x14 | EXEC_EXIT | S→C | exit code |
| 0x15 | EXEC_KILL | C→S | forwarded as CTRL_BREAK / TerminateProcess |
| 0x20 | FS_LIST / RESULT | both | directory listing |
| 0x21 | FS_STAT | both | size/mtime/isDir |
| 0x22–0x25 | FS_MKDIR/DELETE/MOVE/COPY | both | **executed entirely agent-side** — no bytes cross the wire, since source and dest are both on the XP filesystem |
| 0x26 | FS_READ / CHUNK | both | download, chunked, resumable by offset |
| 0x27 | FS_WRITE / ACK | both | upload, chunked; written to `<name>.xpupload.tmp`, sha256-verified, atomically renamed on success so a dropped transfer never leaves a corrupt final file |
| 0x30 | SYS_INFO | both | hostname, OS version, uptime, drives/free space |
| 0x31/0x32 | PROC_LIST/KILL | both | via `CreateToolhelp32Snapshot` syscalls |
| 0xFE | ERROR | S→C | code + message, correlated by stream id |
| 0xFF | BYE | either | graceful close |

**Why processes are spawned fresh per exec instead of one shared `cmd.exe`
session with sentinel parsing:** a shared-shell-with-markers approach (the
classic netcat-backdoor trick) is fragile — exit codes are unreliable,
partial-prompt output is easy to misparse, interactive prompts wedge the
session. Instead, each `EXEC_START` spawns a clean `os/exec` process, and the
"shell session" feel (persistent `cwd`) is reproduced by having the agent
track a per-stream working directory and prefixing each exec with
`cmd /C "cd /d <cwd> && <command>"`. Same UX, none of the fragility.

## 5. Feature set (what `xpctl` can do)

- `xpctl exec "<cmd>" [--cwd] [--timeout] [--json]` — one-shot command, waits
  for exit, prints stdout/stderr/exit code (or structured JSON).
- `xpctl shell` — interactive session, stdin/stdout streamed live, `cwd`
  persists across commands within the session.
- `xpctl ls / stat / mkdir / rm / mv / cp <path>` — file management, mapped
  1:1 to the `FS_*` messages.
- `xpctl get <remote> <local>` / `xpctl put <local> <remote>` — chunked,
  checksummed, resumable file transfer.
- `xpctl ps` / `xpctl kill <pid>` — process management.
- `xpctl sysinfo` — hostname, OS build, uptime, disk space.
- `xpctl pair <addr> --token <token>` — first-time trust setup (TOFU).

## 6. Project layout

```
remote-shell-windows-xp/
  go.mod                        module github.com/BaseMax/remote-shell-windows-xp
  cmd/
    xpctl/        main.go       sender CLI — built with the host's modern Go
    xpagent/      main.go       receiver daemon — built with archived go1.10.8
  internal/
    protocol/     frame.go, types.go        wire format, encode/decode
    transport/     tls.go, pin.go            listener/dialer, cert pinning, HELLO/auth
    session/       exec.go                   process spawn/kill, per-stream cwd tracking
    fsops/         fsops.go                  list/stat/mkdir/delete/move/copy/read/write
    sysinfo/       sysinfo_windows.go        hostname/version/uptime/drives via syscall
    procops/       procops_windows.go        toolhelp32 process list/kill via syscall
    auditlog/      auditlog.go               append-only command/audit log
    certutil/      certutil.go               self-signed cert gen + fingerprint store
  build/
    build-agent.ps1               GOPATH shim (junction) + go1.10.8 cross-compile, GOOS=windows GOARCH=386
    build-sender.ps1              plain `go build ./cmd/xpctl` with the host toolchain
  docs/
    PROTOCOL.md                   full wire-format spec (split out of this doc as it grows)
  README.md
  DESIGN.md                       this document
```

`go.mod` lives at the root for the modern build. Go 1.10 ignores `go.mod`
entirely (it predates modules) — `build-agent.ps1` just needs the source tree
reachable under a `GOPATH/src/<import-path>` layout, which a directory
junction (`mklink /J`, no admin rights required) gives it without duplicating
files.

## 7. Why there's no separate control API for the AI agent

The README's stated end goal is: *"ask an AI agent to fully control
everything ... through my app."* The natural way an AI coding agent (like
this one) already operates is by shelling out to CLI tools and reading their
output. So `xpctl` itself — a scriptable, `--json`-capable CLI — **is** the
automation surface. No JSON-RPC/HTTP control layer is needed for v1; it would
just be a second interface to keep in sync with the first. If tighter
integration is wanted later, an MCP server that wraps `xpctl` subcommands is
a natural v2 addition, but it's additive, not a prerequisite.

## 8. Phased implementation plan

1. **Core**: `protocol` + `transport` (TLS, pinning, HELLO/auth) + one-shot
   `EXEC` + `xpctl exec`. Enough to prove the whole pipe end-to-end.
2. **File ops**: `fsops` (list/stat/mkdir/delete/move/copy local to agent,
   get/put chunked+resumable+checksummed) + corresponding `xpctl` verbs.
3. **Interactive shell**: `session` cwd tracking, `xpctl shell`, stdin
   streaming, kill/interrupt forwarding; `sysinfo`, `procops` (ps/kill).
4. **Lifecycle & packaging**: `xpagentd -init` (cert/token generation),
   install-as-XP-service, `xpctl pair`, audit log.
5. **Hardening**: optional destructive-command guard rails, structured
   `--json` on every verb, tests, docs.

## 9. Open decisions worth confirming before Phase 1

- Confirm the XP VM is 32-bit (`GOARCH=386`) — if it's the rare x64 XP
  edition, the toolchain/version story changes slightly.
- Bind port for `xpagentd` (any free port on the host-only adapter is fine;
  no need to fight over 22).
- Whether to start with an already-downloaded `go1.10.8` toolchain or fetch
  it as part of `build-agent.ps1`'s first run.
