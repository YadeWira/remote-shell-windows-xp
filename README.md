# remote-shell-windows-xp

An SSH-like remote control system: a controller CLI (`xpctl`) drives an
agent daemon (`xpagent`) over a TLS-encrypted, certificate-pinned
connection; run commands, manage files, and inspect the system on a
machine you don't have (or want) an interactive session on.

Originally built to control a Windows XP VirtualBox guest from a Windows 11
host; both `xpctl` and `xpagent` are now cross-platform. This document is
written to be usable by a human operator and by an AI agent driving the
CLI directly, see **For AI agents** below.

## For AI agents

`xpctl` is a plain, non-interactive CLI: every command is one process that
runs, prints plain text (or JSON with `--json`) to stdout, and exits with
a meaningful code, the same shape as any other tool you already call via a
shell. There is no separate API to integrate with; the CLI *is* the API.

**One-time setup, per agent.** You need the agent's `host:port` and the
auth token it printed on first run (ask the human operator for these, or
for help starting an agent, see **Quick start** below). Then:

```
xpctl pair <addr:port> --token <token> --yes
```

`--yes` is required for non-interactive pairing since there is no TTY to
answer the fingerprint confirmation prompt. This is a one-time step per
`addr:port`; it saves the connection's trust locally, and every later
command against that address works without passing the token again.

**Then drive it directly**, for example:

```
xpctl exec <addr:port> "<command>" --json     # run a command, get {stdout,stderr,exit_code}
xpctl cat <addr:port> <path>                   # read a file straight into context
xpctl tree <addr:port> <path> --json           # explore a directory tree, machine-readable
xpctl ls <addr:port> <path>                    # list one directory
xpctl sysinfo <addr:port>                      # find out what OS/arch you're talking to
xpctl put <addr:port> <local> <remote>         # upload a file
xpctl get <addr:port> <remote> <local>         # download a file
```

Read **Command reference** below before constructing commands; exact flag
defaults matter (`put`'s `--resume` defaults to true, for example), and
every command's output format and exit-code behavior is documented there
precisely, not just by example.

**Path syntax depends on the agent's OS, not the machine `xpctl` runs on.**
If you don't already know what the target agent is, run `xpctl sysinfo
<addr:port>` first: a Windows agent expects `C:\Users\...`-style paths, a
Unix agent expects `/home/...`-style paths, regardless of which OS you're
issuing the command from.

**Scope of what pairing grants**: full command execution and file
read/write/delete on the target machine, with no built-in allowlist or
per-command confirmation beyond the one-time pairing step. Treat it like
SSH access, not a sandboxed tool: use `ls`/`cat`/`stat`/`tree` to confirm
state before running anything destructive (`rm`, `mv`, or an `exec` that
deletes/overwrites), and prefer the least destructive command that
accomplishes the task. Every command the agent executes is appended to a
local audit log on the agent's machine with a timestamp.

## Quick start

**1. Build.**

```powershell
# Windows, controller (xpctl), always the modern toolchain:
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
from the controller (never `0.0.0.0` by default; pass `-insecure-bind-all`
to override that safety check):

```
xpagent -listen 192.168.56.101:5959          # Windows
./xpagent -listen 192.168.1.50:5959          # Linux/BSD/macOS
```

On first run it generates a certificate and auth token and prints the
`xpctl pair` command to run on the controller, for example:

```
==================================================================
 xpagent: first-run setup complete
==================================================================
 Certificate fingerprint (sha256): <64 hex chars>
 Auth token:                       <64 hex chars>

 On the controller machine, run:
   xpctl pair 192.168.56.101:5959 --token <64 hex chars>
==================================================================
```

**3. Pair and go, from the controller:**

```
xpctl pair <agent-addr:port> --token <token-from-xpagent> --yes
xpctl exec <agent-addr:port> "whoami"
xpctl shell <agent-addr:port>
xpctl cat <agent-addr:port> /remote/path/file.txt
xpctl tree <agent-addr:port> /remote/path
xpctl put <agent-addr:port> ./local.txt /remote/path/local.txt
xpctl get <agent-addr:port> /remote/path/file.txt ./file.txt
xpctl sysinfo <agent-addr:port>
```

Run `xpctl help` for a one-line-per-command summary; see **Command
reference** below for full detail.

## Command reference

Every command's first argument is `<addr:port>`, the agent to talk to
(must already be paired via `xpctl pair`, see above). Flags can appear
anywhere after the address.

**Exit codes**: `0` success. `2` bad arguments/usage; nothing was sent to
the agent. `1` the request reached the agent but failed (connection error,
remote-side error, checksum mismatch, path not found, etc); the reason is
printed to stderr as `xpctl: <message>`. The one exception is `exec`,
which exits with the **remote command's own exit code** (0-255) instead of
xpctl's usual 0/1/2.

---

### `pair` - establish trust with an agent

```
xpctl pair <addr:port> --token <token> [--yes]
```

First-time setup for a given `addr:port`. Connects without verifying the
agent's certificate yet, authenticates with `--token` (printed by
`xpagent` on its own first run), and shows the certificate fingerprint the
agent presented.

- Without `--yes`: prompts `Trust this fingerprint and save it for future
  connections? [y/N]` on stdin. **Always pass `--yes` for scripted/AI use**
  since there's no TTY to answer it.
- On success: saves `{addr, fingerprint, token}` to a local trust store
  (`%AppData%\xpctl\known_agents.json` on Windows,
  `~/.config/xpctl/known_agents.json` on Linux/BSD,
  `~/Library/Application Support/xpctl/known_agents.json` on macOS). Every
  later command to that `addr:port` reads this automatically; the token
  never needs to be passed again.
- Re-running `pair` for an address you've already paired overwrites the
  saved entry (useful if the agent was reinstalled with a new
  cert/token).

### `exec` - run one command and wait for it to finish

```
xpctl exec <addr:port> "<command>" [--cwd <dir>] [--json]
```

Runs `<command>` through the target's native shell (`cmd.exe /C` on a
Windows agent, `/bin/sh -c` on a Unix agent) in the agent's tracked
working directory (default `C:\` or `/`), or in `--cwd` if given. Quote
`<command>` exactly as you would typing it directly into that shell.

- Without `--json`: streams stdout/stderr live as the command runs, then
  xpctl exits with the same exit code the remote command returned.
- With `--json`: buffers everything, then prints one line to stdout:
  `{"stdout":"...","stderr":"...","exit_code":N}`, and still exits with
  that same `N`. **Recommended for AI/scripted use.**
- `cd`/`chdir` as the command is special-cased server-side (no subprocess
  spawned) and updates the agent's tracked working directory for that
  connection, but a single `exec` call is a new connection each time, so
  `cd` alone in one `exec` call has no effect on a later one; use `--cwd`,
  or chain with `&&` (`cmd.exe`) / `;` (`/bin/sh`), or use `shell` (below)
  if you need a working directory that persists across multiple commands.

### `shell` - interactive session with persistent working directory

```
xpctl shell <addr:port>
```

Opens a `path> ` prompt reading commands from stdin, one per line; `cd`
persists across commands within the session (unlike separate `exec`
calls); type `exit` or `quit` to leave. Built for a human at a terminal.
It can be driven non-interactively by piping newline-separated commands
into stdin followed by `exit`, but output isn't structured (prompts and
command output interleave as plain text); prefer `exec` with `--cwd` for
scripted/AI use unless you specifically need `cd` to persist across
several commands in one session.

### `ls` - list a directory

```
xpctl ls <addr:port> <path>
```

One line per entry: `F` or `D`, size in bytes, modification time (UTC,
ISO 8601), name. Example:

```
D             0  2026-07-10T14:50:07Z  testdir
F            21  2026-07-10T14:50:07Z  uploaded.txt
```

### `tree` - recursive directory listing

```
xpctl tree <addr:port> <path> [--depth N] [--json]
```

Walks the directory tree entirely server-side (one round trip regardless
of depth).

- `--depth N`: stop descending after N levels (default `0` = unlimited).
- Default output: ASCII tree art, plus a trailing `N directories, M files`
  summary line.
- `--json`: prints `{"entries":[{"path":"...","is_dir":bool,"size":N},
  ...],"truncated":bool}` instead of ASCII art. **Recommended for
  AI/scripted use.** `path` is always relative to `<path>` and `/`-
  separated regardless of the agent's OS.
- Capped at 50,000 entries for safety; `"truncated":true` (and a
  `note: output truncated...` line in non-JSON mode) means there was more
  than that under `<path>`.

### `stat` - metadata for one path

```
xpctl stat <addr:port> <path>
```

Prints `does not exist`, or `file, N bytes, modified TIME` /
`directory, N bytes, modified TIME`.

### `cat` - print remote file contents

```
xpctl cat <addr:port> <path> [<path>...]
```

Streams the remote file's raw bytes straight to stdout; no local file is
created. Multiple paths are concatenated in order, like Unix `cat`. Useful
for reading a config file, log, or source file straight into context.

### `mkdir` - create a directory

```
xpctl mkdir <addr:port> <path>
```

Creates `path`, including any missing parent directories (like
`mkdir -p`). No output on success.

### `rm` - delete a file or directory

```
xpctl rm <addr:port> <path> [--recursive]
```

Deletes a single file, or (with `--recursive`) a directory and everything
under it. **Irreversible, no trash/undo.** No output on success.

### `mv` - rename or move

```
xpctl mv <addr:port> <src> <dst>
```

Renames/moves `src` to `dst` on the agent's filesystem (no bytes cross the
network). No output on success.

### `cp` - copy

```
xpctl cp <addr:port> <src> <dst>
```

Copies a file, or recursively a directory tree, entirely on the agent's
filesystem (no bytes cross the network). No output on success.

### `get` - download a file

```
xpctl get <addr:port> <remote-path> <local-path>
```

Downloads from the agent to the controller's local filesystem, verifying
a SHA-256 checksum after transfer. Prints `downloaded N bytes to
<local-path>` on success.

### `put` - upload a file

```
xpctl put <addr:port> <local-path> <remote-path> [--overwrite] [--resume]
```

Uploads from the controller to the agent, verifying a SHA-256 checksum
after transfer.

- `--overwrite` (default **false**): required to replace an existing
  remote file; otherwise the upload fails if `<remote-path>` already
  exists.
- `--resume` (default **true**): resumes from a `<name>.xpupload.tmp`
  partial file the agent kept after a previously interrupted upload to
  the same path, instead of re-sending from byte 0. Pass `--resume=false`
  to force a full re-upload.
- Prints `uploaded N bytes to <remote-path>` on success.

### `ps` - list processes

```
xpctl ps <addr:port>
```

Header row `PID PPID NAME` then one row per running process. `NAME` is
the full command line with arguments on a Unix agent, the executable name
only on a Windows agent.

### `kill` - terminate a process

```
xpctl kill <addr:port> <pid>
```

Force-terminates the process by PID (`TerminateProcess` on a Windows
agent, `SIGKILL` on a Unix agent). No output on success.

### `sysinfo` - system information

```
xpctl sysinfo <addr:port>
```

Prints hostname, OS version string, CPU architecture, uptime, free/total
memory in MB, and free/total space for the root drive/filesystem. Useful
as the first command against an unfamiliar agent, since every other
command's path syntax depends on the target OS this reports.

Copyright 2026, Seyyed Ali Mohammadiyeh (MAX BASE)
