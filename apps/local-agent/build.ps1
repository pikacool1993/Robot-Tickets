param(
    [string]$ServerEnv = "",
    [switch]$Preview
)
$ErrorActionPreference = "Stop"
Push-Location $PSScriptRoot
try {
    if ($ServerEnv) {
        go run ./cmd/prepare-trust -env $ServerEnv
        if ($LASTEXITCODE -ne 0) { throw "Trust preparation failed" }
    }
    if (!$Preview -and !(Test-Path -LiteralPath "internal/trust/assets/local.json")) {
        throw "Run go run ./cmd/prepare-trust -env ../cloud-server/.env first, or use -Preview for an unconfigured UI build."
    }
    Push-Location frontend
    try {
        pnpm install --frozen-lockfile
        if ($LASTEXITCODE -ne 0) { throw "Frontend install failed" }
        pnpm build
        if ($LASTEXITCODE -ne 0) { throw "Frontend build failed" }
    } finally { Pop-Location }
    New-Item -ItemType Directory -Force build/bin/config | Out-Null
    $seatCatalog = Join-Path $PSScriptRoot "config/seat-catalog.json"
    if (!(Test-Path -LiteralPath $seatCatalog)) { throw "Missing config/seat-catalog.json" }
    Get-Content -LiteralPath $seatCatalog -Raw -Encoding UTF8 | ConvertFrom-Json | Out-Null
    # Preserve an operator's existing runtime catalog. The source copy is for fresh packages.
    if (!(Test-Path -LiteralPath "build/bin/config/seat-catalog.json")) {
        Copy-Item -LiteralPath $seatCatalog -Destination "build/bin/config/seat-catalog.json"
    }
    $previousOS = $env:GOOS
    $previousArch = $env:GOARCH
    try {
        $env:GOOS = "windows"
        $env:GOARCH = "amd64"
        go build -tags production -trimpath -ldflags "-s -w -H windowsgui" -o build/bin/Robot.exe .
        if ($LASTEXITCODE -ne 0) { throw "Desktop build failed" }
    } finally { $env:GOOS = $previousOS; $env:GOARCH = $previousArch }
    Write-Output "Built build/bin/Robot.exe. Place the exported bootstrap.cfg in build/bin/config/."
} finally { Pop-Location }
