# remote-shell-windows-xp

An SSH-like remote control system between a Windows 11 controller (`xpctl`)
and a Windows XP agent (`xpagent`) running in VirtualBox: run commands,
manage files, and inspect the system, scriptable enough for an AI agent to
drive directly. See [DESIGN.md](DESIGN.md) for the full architecture.

## Quick start

**1. Build both binaries.** `xpagent` must be cross-compiled with an
archived **go1.10.8** toolchain (the last one that targets Windows XP);
`xpctl` builds with whatever modern Go you already have.

```powershell
# On Windows 11 (controller):
.\build\build-sender.ps1                                    # -> dist\xpctl.exe

# Also on Windows 11, cross-compiling for the XP guest:
$env:XPSHELL_GO110_ROOT = "C:\go1.10.8"                      # extracted go1.10.8.windows-386.zip
.\build\build-agent.ps1                                      # -> dist\xpagent.exe
```

**2. Copy `dist\xpagent.exe` into the XP VM** (shared folder, or any other
transfer method you already have) and run it, bound to the VirtualBox
host-only adapter IP (see `ipconfig` on the XP guest — typically
`192.168.56.x`, matching the `Ethernet 3` adapter below on the Windows 11
side):

```
xpagent.exe -listen 192.168.56.101:5959
```

On first run it generates a certificate and auth token and prints an
`xpctl pair` command.

**3. On Windows 11, pair and go:**

```powershell
dist\xpctl.exe pair 192.168.56.101:5959 --token <token-from-xpagent>
dist\xpctl.exe exec 192.168.56.101:5959 "dir C:\"
dist\xpctl.exe shell 192.168.56.101:5959
dist\xpctl.exe put 192.168.56.101:5959 .\local.txt C:\Users\XP\Desktop\local.txt
dist\xpctl.exe get 192.168.56.101:5959 C:\Windows\win.ini .\win.ini
dist\xpctl.exe sysinfo 192.168.56.101:5959
```

Run `xpctl help` for the full command list.
