param(
    [string]$TargetOS = "",
    [string]$TargetArch = ""
)

$ErrorActionPreference = "Stop"

$repoRoot = Split-Path -Parent $PSScriptRoot
$outDir = Join-Path $repoRoot "dist"
New-Item -ItemType Directory -Force -Path $outDir | Out-Null

$suffix = ""
$ext = ""
if ($TargetOS) {
    $suffix = "-$TargetOS-$TargetArch"
    if ($TargetOS -eq "windows") { $ext = ".exe" }
} elseif ($env:OS -eq "Windows_NT") {
    $ext = ".exe"
}
$outFile = Join-Path $outDir "xpctl$suffix$ext"

Push-Location $repoRoot
try {
    if ($TargetOS) {
        $env:GOOS = $TargetOS
        $env:GOARCH = $TargetArch
    }
    Write-Host "Building xpctl..."
    go build -o $outFile ./cmd/xpctl
    if ($LASTEXITCODE -ne 0) {
        throw "go build exited with code $LASTEXITCODE"
    }
    Write-Host "Built $outFile"
} finally {
    if ($TargetOS) {
        Remove-Item Env:\GOOS, Env:\GOARCH -ErrorAction SilentlyContinue
    }
    Pop-Location
}
