param(
    [string]$LegacyGoRoot = $env:XPSHELL_GO110_ROOT,
    [string]$TargetArch = "386"
)

$ErrorActionPreference = "Stop"

if (-not $LegacyGoRoot) {
    Write-Error "Pass -LegacyGoRoot, or set `$env:XPSHELL_GO110_ROOT, to your extracted go1.10.8 install (e.g. C:\go1.10.8). Download go1.10.8.windows-386.zip from https://go.dev/dl/ and extract it."
    exit 1
}
$legacyGoExe = Join-Path $LegacyGoRoot "bin\go.exe"
if (-not (Test-Path $legacyGoExe)) {
    Write-Error "go.exe not found at $legacyGoExe"
    exit 1
}

$repoRoot = Split-Path -Parent $PSScriptRoot
$modulePath = "github.com/BaseMax/remote-shell-windows-xp"
$moduleParentRel = "src\github.com\BaseMax"
$moduleLinkRel = "src\github.com\BaseMax\remote-shell-windows-xp"

$gopath = Join-Path $env:TEMP "xpshell-legacy-gopath"
$linkParent = Join-Path $gopath $moduleParentRel
$linkPath = Join-Path $gopath $moduleLinkRel

New-Item -ItemType Directory -Force -Path $linkParent | Out-Null
if (-not (Test-Path $linkPath)) {
    cmd /c mklink /J "$linkPath" "$repoRoot" | Out-Null
}

$outDir = Join-Path $repoRoot "dist"
New-Item -ItemType Directory -Force -Path $outDir | Out-Null
$outFile = Join-Path $outDir "xpagent.exe"

Write-Host "Building xpagent with $legacyGoExe (GOOS=windows GOARCH=$TargetArch)..."

$env:GOROOT = $LegacyGoRoot
$env:GOPATH = $gopath
$env:GO111MODULE = "off"
$env:GOOS = "windows"
$env:GOARCH = $TargetArch
$env:CGO_ENABLED = "0"

try {
    & $legacyGoExe build -o $outFile "$modulePath/cmd/xpagent"
    if ($LASTEXITCODE -ne 0) {
        throw "go build exited with code $LASTEXITCODE"
    }
} finally {
    Remove-Item Env:\GOROOT, Env:\GOPATH, Env:\GO111MODULE, Env:\GOOS, Env:\GOARCH, Env:\CGO_ENABLED -ErrorAction SilentlyContinue
}

Write-Host "Built $outFile"
Write-Host "Copy it to the XP guest and run it with -listen, e.g.:"
Write-Host "  xpagent.exe -listen 192.168.56.101:5959"
