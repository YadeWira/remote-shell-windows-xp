param(
    [string]$TargetOS = "windows",
    [string]$TargetArch = "amd64",
    [string]$LegacyGoRoot = $env:XPSHELL_GO110_ROOT
)

$ErrorActionPreference = "Stop"

$repoRoot = Split-Path -Parent $PSScriptRoot
$outDir = Join-Path $repoRoot "dist"
New-Item -ItemType Directory -Force -Path $outDir | Out-Null

$ext = ""
if ($TargetOS -eq "windows") { $ext = ".exe" }
$outFile = Join-Path $outDir "xpagent-$TargetOS-$TargetArch$ext"

$useLegacy = ($TargetOS -eq "windows") -and $LegacyGoRoot

if ($useLegacy) {
    $legacyGoExe = Join-Path $LegacyGoRoot "bin\go.exe"
    if (-not (Test-Path $legacyGoExe)) {
        Write-Error "go.exe not found at $legacyGoExe"
        exit 1
    }

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

    Write-Host "Building xpagent for $TargetOS/$TargetArch with $legacyGoExe (go1.10, Windows XP/Server 2003 compatible)..."

    $env:GOROOT = $LegacyGoRoot
    $env:GOPATH = $gopath
    $env:GO111MODULE = "off"
    $env:GOOS = $TargetOS
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
} else {
    Write-Host "Building xpagent for $TargetOS/$TargetArch with the host Go toolchain..."

    Push-Location $repoRoot
    try {
        $env:GOOS = $TargetOS
        $env:GOARCH = $TargetArch
        $env:CGO_ENABLED = "0"
        go build -o $outFile ./cmd/xpagent
        if ($LASTEXITCODE -ne 0) {
            throw "go build exited with code $LASTEXITCODE"
        }
    } finally {
        Remove-Item Env:\GOOS, Env:\GOARCH, Env:\CGO_ENABLED -ErrorAction SilentlyContinue
        Pop-Location
    }
}

Write-Host "Built $outFile"
if ($TargetOS -eq "windows" -and -not $useLegacy) {
    Write-Host "Targets modern Windows (7/8/10/11). For Windows XP/Server 2003, pass -LegacyGoRoot pointing at an extracted go1.10.8 toolchain (download go1.10.8.windows-386.zip from https://go.dev/dl/)."
}
