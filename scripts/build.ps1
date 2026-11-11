param(
    [string]$Version = $(if ($env:VERSION) { $env:VERSION } else { '0.0.1-dev' })
)

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$dist = Join-Path $root 'dist'
$previousGoos = $env:GOOS
$previousGoarch = $env:GOARCH
$previousCgo = $env:CGO_ENABLED
New-Item -ItemType Directory -Force -Path $dist | Out-Null
Push-Location $root

try {
    $commit = 'unknown'
    try {
        $candidate = (& git rev-parse --short HEAD 2>$null).Trim()
        if ($LASTEXITCODE -eq 0 -and $candidate) { $commit = $candidate }
    } catch { }
    $buildDate = [DateTime]::UtcNow.ToString('yyyy-MM-ddTHH:mm:ssZ')
    $ldflags = "-s -w -X wslc-bridge/internal/buildinfo.Version=$Version -X wslc-bridge/internal/buildinfo.Commit=$commit -X wslc-bridge/internal/buildinfo.Date=$buildDate"
    $artifacts = @()
    $checksums = @()

    foreach ($arch in @('amd64', 'arm64')) {
        $fileName = "wslc-bridge-windows-$arch.exe"
        $output = Join-Path $dist $fileName
        $env:GOOS = 'windows'
        $env:GOARCH = $arch
        $env:CGO_ENABLED = '0'
        & go build -trimpath -ldflags $ldflags -o $output ./cmd/wslc-bridge
        if ($LASTEXITCODE -ne 0) { throw "go build failed for windows/$arch" }
        $hash = (Get-FileHash -Algorithm SHA256 -Path $output).Hash.ToLowerInvariant()
        $artifacts += [ordered]@{ os = 'windows'; arch = $arch; file = $fileName; sha256 = $hash }
        $checksums += "$hash  $fileName"
    }

    $manifest = [ordered]@{
        schemaVersion = 1
        version = $Version
        commit = $commit
        buildDate = $buildDate
        artifacts = $artifacts
    }
    $json = $manifest | ConvertTo-Json -Depth 5
    [System.IO.File]::WriteAllText((Join-Path $dist 'bridge-manifest.json'), "$json`n", [System.Text.UTF8Encoding]::new($false))
    [System.IO.File]::WriteAllLines((Join-Path $dist 'checksums.txt'), $checksums, [System.Text.UTF8Encoding]::new($false))
    Write-Host "Build complete: $dist"
} finally {
    if ($null -eq $previousGoos) { Remove-Item Env:GOOS -ErrorAction SilentlyContinue } else { $env:GOOS = $previousGoos }
    if ($null -eq $previousGoarch) { Remove-Item Env:GOARCH -ErrorAction SilentlyContinue } else { $env:GOARCH = $previousGoarch }
    if ($null -eq $previousCgo) { Remove-Item Env:CGO_ENABLED -ErrorAction SilentlyContinue } else { $env:CGO_ENABLED = $previousCgo }
    Pop-Location
}
