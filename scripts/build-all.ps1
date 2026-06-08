$ErrorActionPreference = "Stop"

$Root = Resolve-Path (Join-Path $PSScriptRoot "..")
$Dist = Join-Path $Root "dist"
New-Item -ItemType Directory -Force -Path $Dist | Out-Null

function Build-Janus {
    param(
        [string]$Goos,
        [string]$Goarch,
        [string]$Ext
    )

    $Output = Join-Path $Dist "janus-$Goos-$Goarch$Ext"
    Write-Host "building $Output"
    $env:GOOS = $Goos
    $env:GOARCH = $Goarch
    go build -o $Output (Join-Path $Root "cmd/janus")
}

Build-Janus "linux" "amd64" ""
Build-Janus "linux" "arm64" ""
Build-Janus "darwin" "amd64" ""
Build-Janus "darwin" "arm64" ""
Build-Janus "windows" "amd64" ".exe"
Build-Janus "windows" "arm64" ".exe"

Remove-Item Env:\GOOS
Remove-Item Env:\GOARCH
