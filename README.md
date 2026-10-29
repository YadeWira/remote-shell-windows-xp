# remote-shell-windows-xp

An SSH-like remote control system: a controller CLI (`xpctl`) drives an
agent daemon (`xpagent`) over a TLS-encrypted, certificate-pinned
connection; run commands, manage files, and inspect the system on a
machine you don't have (or want) an interactive session on.

Originally built to control a Windows XP VirtualBox guest from a Windows 11
host; both `xpctl` and `xpagent` are now cross-platform. The documentation is
written for a human operator and for an AI agent driving the CLI directly
(see the wiki page *For AI agents*).

## About this fork

This is a maintained fork of [BaseMax/remote-shell-windows-xp](https://github.com/BaseMax/remote-shell-windows-xp). The design and protocol are unchanged, so a fork `xpctl` talks to an upstream `xpagent` and the other way round. The fork adds fixes found by running the agent on a real Windows XP SP3 guest driven by AI agents: exec quoting and compound `cd`, killing the whole process tree (Job Object), `exec --timeout`, `exec --desktop` for visible windows, `xpagent -install` (boot-time task with automatic restart), non-UTF-8 output, safer file operations, pairing with `--fingerprint`, and building the XP agent from Linux. The full list, tests and open work are in [What this fork changes](https://github.com/YadeWira/remote-shell-windows-xp/wiki/Fork-changes).

## Quick start

```sh
# Controller, any OS:
go build -o xpctl ./cmd/xpctl

# Windows XP agent from Linux/macOS (needs go1.10.8, see the Building page):
XPSHELL_GO110_ROOT=$PWD/go ./build/build.sh xp     # -> dist/xpagent-xp.exe
```

```
xpagent -listen 192.168.56.101:5959                  # on the target; prints the fingerprint and token
xpctl pair 192.168.56.101:5959 --token <token> --fingerprint <sha256> --yes
xpctl exec 192.168.56.101:5959 "dir C:\" --json --timeout 30s
xpctl exec 192.168.56.101:5959 "notepad.exe" --desktop
```

To start the agent at boot and restart it if it dies, run `xpagent -install -listen 192.168.56.101:5959` as Administrator on the Windows target (details in the wiki page *Installing the agent*).

`xpctl help` lists every command. Pairing grants full command execution and file access on the target, like SSH: use it on trusted networks only.

## Documentation

The details live in the [wiki](https://github.com/YadeWira/remote-shell-windows-xp/wiki): [Quick start](https://github.com/YadeWira/remote-shell-windows-xp/wiki/Quick-start), [Building](https://github.com/YadeWira/remote-shell-windows-xp/wiki/Building), [Command reference](https://github.com/YadeWira/remote-shell-windows-xp/wiki/Command-reference), [For AI agents](https://github.com/YadeWira/remote-shell-windows-xp/wiki/For-AI-agents), [Installing the agent](https://github.com/YadeWira/remote-shell-windows-xp/wiki/Installing-the-agent), [Running on Windows XP](https://github.com/YadeWira/remote-shell-windows-xp/wiki/Windows-XP-notes).

## License and credit

The code is BaseMax's, under the GNU GPL v3 (see `LICENSE`); the changes in this fork are released under the same license.

Copyright 2026, Seyyed Ali Mohammadiyeh (MAX BASE)
