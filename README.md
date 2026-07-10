# remote-shell-windows-xp

An SSH-like remote control system: a controller CLI (`xpctl`) drives an
agent daemon (`xpagent`) over a TLS-encrypted, certificate-pinned
connection — run commands, manage files, and inspect the system on a
machine you don't have (or want) an interactive session on. Scriptable
enough for an AI agent to drive directly.

Originally built to control a Windows XP VirtualBox guest from a Windows 11
host; both `xpctl` and `xpagent` are now cross-platform.

## Supported platforms

| | Windows XP / Server 2003 | Windows 7/8/10/11 | Linux | FreeBSD / OpenBSD / NetBSD | macOS |
|---|---|---|---|---|---|
| `xpagent` | yes (386, needs go1.10.8 toolchain — see below) | yes (386/amd64/arm64) | yes (386/amd64/arm/arm64) | yes (386/amd64) | yes (amd64/arm64) |
| `xpctl` | — | yes | yes | yes | yes |

`xpctl` (the controller) never needs the legacy toolchain — it always
builds with whatever modern Go you have. `xpagent` (the agent/receiver)
only needs the legacy go1.10.8 toolchain when the *target* is Windows
XP/Server 2003; every other target builds with modern Go too. Both 32-bit
and 64-bit architectures are supported everywhere the target OS itself
supports them (XP is realistically 386-only).

On Unix targets, `xpagent` shells out to `ps`, `uname`, `df`, and `sysctl`
(all POSIX/near-universal) for process listing and system info, and to
`/bin/sh` for command execution — kept dependency-free the same way the
Windows side is (raw syscalls, no `golang.org/x/sys`). One known limitation:
`sysinfo`'s uptime is only populated on Linux (via `/proc/uptime`); it
reads as 0 on BSD/macOS.

## Quick start

**1. Build.**

```powershell
# Windows, controller (xpctl) — always the modern toolchain:
.\build\build-sender.ps1                              # -> dist\xpctl.exe

# Windows, agent (xpagent) targeting modern Windows (7/8/10/11):
.\build\build-agent.ps1 -TargetOS windows -TargetArch amd64   # -> dist\xpagent-windows-amd64.exe

# Windows, agent targeting Windows XP/Server 2003 (needs go1.10.8):
#   download go1.10.8.windows-386.zip from https://go.dev/dl/ and extract it
.\build\build-agent.ps1 -TargetOS windows -TargetArch 386 -LegacyGoRoot C:\go1.10.8

# Windows, agent targeting Linux/BSD (still built from a Windows host):
.\build\build-agent.ps1 -TargetOS linux -TargetArch amd64     # -> dist\xpagent-linux-amd64
```

```sh
# Linux/BSD/macOS, both binaries for the current OS/arch:
./build/build.sh
# ...or a specific target:
./build/build.sh linux amd64
./build/build.sh windows amd64   # produces a modern-Windows agent, not XP-compatible
```

**2. Run the agent** on the target machine, bound to an address reachable
from the controller (never `0.0.0.0` by default — pass `-insecure-bind-all`
to override that safety check):

```
xpagent -listen 192.168.56.101:5959          # Windows
./xpagent -listen 192.168.1.50:5959          # Linux/BSD/macOS
```

On first run it generates a certificate and auth token and prints the
`xpctl pair` command to run on the controller.

**3. Pair and go, from the controller:**

```
xpctl pair <agent-addr:port> --token <token-from-xpagent>
xpctl exec <agent-addr:port> "whoami"
xpctl shell <agent-addr:port>
xpctl put <agent-addr:port> ./local.txt /remote/path/local.txt
xpctl get <agent-addr:port> /remote/path/file.txt ./file.txt
xpctl sysinfo <agent-addr:port>
```

Run `xpctl help` for the full command list (`ls`, `stat`, `mkdir`, `rm`,
`mv`, `cp`, `ps`, `kill` round out file and process management).

## How it works

- **Transport**: TLS 1.2, AEAD cipher suites only. The agent's certificate
  is self-signed (no CA); `xpctl pair` records its SHA-256 fingerprint
  locally (like SSH's `known_hosts`) on first connect, and every later
  connection is verified against that pin. A bearer token sent inside the
  TLS channel authenticates the client.
- **Wire protocol**: a single TCP connection carrying length-prefixed
  frames, multiplexed by a stream ID so exec sessions, file transfers, and
  heartbeats can share one connection concurrently (`internal/protocol`,
  `internal/transport`).
- **Exec**: each command spawns a fresh process (`cmd.exe /C` on Windows,
  `/bin/sh -c` elsewhere) rather than feeding a single long-lived shell
  with sentinel-delimited output — far more robust exit-code and I/O
  handling. `cd`/`chdir` is special-cased and resolved against a
  per-stream tracked working directory instead of spawning a process,
  which is what makes `xpctl shell` feel like a persistent session
  (`internal/session`).
- **File transfer**: `get`/`put` are chunked and SHA-256-checksummed;
  uploads resume from a `<name>.xpupload.tmp` partial file left by an
  interrupted transfer.
- **No separate control API**: `xpctl` itself, as a scriptable CLI, is the
  automation surface for an AI agent to drive — the same shape a coding
  agent already uses for any other CLI tool.
- **Audit log**: the agent appends every command and file operation it
  executes, with a timestamp, to a local log — since the point of this
  project is letting an agent run commands with nobody watching in real
  time, there should always be a record of what actually ran.
- **Windows XP compatibility**: Go 1.11 dropped support for binaries that
  run on Windows XP/Server 2003, and go1.10 (the last version that
  supports it) predates Go modules — so anything that might end up in an
  XP build (`internal/protocol`, `internal/transport`, `internal/certutil`,
  `internal/auditlog`, `internal/session`, `internal/sysinfo`,
  `internal/procops`, `internal/winapi`, `cmd/xpagent`) is stdlib-only, and
  Windows syscalls are reached by hand through `syscall.NewLazyDLL` rather
  than `golang.org/x/sys/windows`.
