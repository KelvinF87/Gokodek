$ErrorActionPreference = 'Stop'

$Root = Split-Path -Parent $MyInvocation.MyCommand.Path
$Dist = Join-Path $Root 'dist'
$Version = if ($env:GOKODEK_VERSION) { $env:GOKODEK_VERSION } else { '0.1.0' }

if (Test-Path $Dist) {
    Remove-Item -Recurse -Force $Dist
}
New-Item -ItemType Directory -Path $Dist | Out-Null

Push-Location $Root
try {
    $env:CGO_ENABLED = '0'
    $env:GOOS = 'windows'
    $env:GOARCH = 'amd64'
    go build -trimpath -ldflags "-s -w -X main.version=$Version" -o (Join-Path $Dist 'gokodek.exe') .

    Copy-Item (Join-Path $Root 'README.md') $Dist
    Write-Host "Created $Dist\gokodek.exe"
    Write-Host "Go is only required on the build machine, not on the destination machine."
}
finally {
    Pop-Location
}
