param(
    [string]$ServerEnv = "",
    [switch]$Preview,
    [string]$OutputDir = "build/bin"
)
$ErrorActionPreference = "Stop"
Push-Location $PSScriptRoot
try {
    if (![System.IO.Path]::IsPathRooted($OutputDir)) { $OutputDir = Join-Path $PSScriptRoot $OutputDir }
    $OutputDir = [System.IO.Path]::GetFullPath($OutputDir)
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
    New-Item -ItemType Directory -Force "$OutputDir/config" | Out-Null
    $seatCatalog = Join-Path $PSScriptRoot "config/seat-catalog.json"
    if (!(Test-Path -LiteralPath $seatCatalog)) { throw "Missing config/seat-catalog.json" }
    Get-Content -LiteralPath $seatCatalog -Raw -Encoding UTF8 | ConvertFrom-Json | Out-Null
    # Preserve an operator's existing runtime catalog. The source copy is for fresh packages.
    if (!(Test-Path -LiteralPath "$OutputDir/config/seat-catalog.json")) {
        Copy-Item -LiteralPath $seatCatalog -Destination "$OutputDir/config/seat-catalog.json"
    }
    $billRegions = Join-Path $PSScriptRoot "config/bill-regions.json"
    if (!(Test-Path -LiteralPath $billRegions)) { throw "Missing config/bill-regions.json" }
    Get-Content -LiteralPath $billRegions -Raw -Encoding UTF8 | ConvertFrom-Json | Out-Null
    if (!(Test-Path -LiteralPath "$OutputDir/config/bill-regions.json")) {
        Copy-Item -LiteralPath $billRegions -Destination "$OutputDir/config/bill-regions.json"
    }
    $previousOS = $env:GOOS
    $previousArch = $env:GOARCH
    $previousCGO = $env:CGO_ENABLED
    try {
        $env:GOOS = "windows"
        $env:GOARCH = "amd64"
        $env:CGO_ENABLED = "0"
        # Build protocol programs from source on every delivery; never ship stale prebuilt EXEs.
        foreach ($kind in @("yyb", "wechat")) {
            $sourceRoot = Join-Path $PSScriptRoot "../../protocol-services/$kind"
            $targetRoot = Join-Path $OutputDir "protocols/$kind"
            New-Item -ItemType Directory -Force -Path $targetRoot | Out-Null
            $protocolExe = if ($kind -eq "yyb") { "yyb-go.exe" } else { "wechat_win.exe" }
            $entry = if ($kind -eq "yyb") { "./cmd/yyb-go" } else { "." }
            Push-Location $sourceRoot
            try {
                go build -trimpath -ldflags "-s -w" -o (Join-Path $targetRoot $protocolExe) $entry
                if ($LASTEXITCODE -ne 0) { throw "$kind build failed; close the running protocol before replacing its EXE." }
            } finally { Pop-Location }
            if ($kind -eq "yyb") {
                New-Item -ItemType Directory -Force -Path "$targetRoot/resource/templates" | Out-Null
                Copy-Item -Path "$sourceRoot/resource/templates/*.html" -Destination "$targetRoot/resource/templates" -Force
            } else {
                New-Item -ItemType Directory -Force -Path "$targetRoot/lib", "$targetRoot/conf" | Out-Null
                foreach ($resource in @("08sae.dat", "rqtx.dat")) { Copy-Item -LiteralPath "$sourceRoot/$resource" -Destination $targetRoot -Force }
                foreach ($resource in @("v08.dll", "zlib.dll", "key")) { Copy-Item -LiteralPath "$sourceRoot/lib/$resource" -Destination "$targetRoot/lib" -Force }
                if (!(Test-Path -LiteralPath "$targetRoot/conf/app.conf")) { Copy-Item -LiteralPath "$sourceRoot/conf/app.conf" -Destination "$targetRoot/conf/app.conf" }
                Copy-Item -LiteralPath "$sourceRoot/swagger" -Destination $targetRoot -Recurse -Force
            }
        }
        @'
Protocol services for Robot local-agent (Windows x64).
Robot login prompts for the selected protocol proxy before starting the service.
Leave the proxy empty for direct connections. Robot manages service startup and controls.
Closing Robot keeps protocol services running; heartbeat/refresh resumes after next login.
WeChat requires external Redis; configure protocols/wechat/conf/app.conf and enable Redis persistence.
Runtime account data is not part of the delivery package. Do not copy a used data folder.
'@ | Set-Content -LiteralPath "$OutputDir/protocols/README.txt" -Encoding UTF8
        go build -tags production -trimpath -ldflags "-s -w -H windowsgui" -o "$OutputDir/Robot.exe" .
        if ($LASTEXITCODE -ne 0) { throw "Desktop build failed" }
    } finally { $env:GOOS = $previousOS; $env:GOARCH = $previousArch; $env:CGO_ENABLED = $previousCGO }
    Write-Output "Built $OutputDir/Robot.exe. Place the exported bootstrap.cfg in $OutputDir/config/."
} finally { Pop-Location }
