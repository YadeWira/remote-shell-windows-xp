$ErrorActionPreference = "Stop"

$repoRoot = Split-Path -Parent $PSScriptRoot
$outDir = Join-Path $repoRoot "dist"
New-Item -ItemType Directory -Force -Path $outDir | Out-Null
$outFile = Join-Path $outDir "xpctl.exe"

Push-Location $repoRoot
try {
    Write-Host "Building xpctl..."
    go build -o $outFile ./cmd/xpctl
    if ($LASTEXITCODE -ne 0) {
        throw "go build exited with code $LASTEXITCODE"
    }
    Write-Host "Built $outFile"
} finally {
    Pop-Location
}
