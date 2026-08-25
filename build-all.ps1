# Script PowerShell para compilación cruzada multiplataforma de Gokodek en Windows
$ErrorActionPreference = 'Stop'

Write-Host "=== Compilando Gokodek para Linux, macOS y Windows (Cross-compilation) ===" -ForegroundColor Cyan

if (-not (Test-Path dist)) {
    New-Item -ItemType Directory -Path dist | Out-Null
}

Write-Host "Compilando Linux amd64..."
$env:GOOS = "linux"; $env:GOARCH = "amd64"; go build -o dist/gokodek-linux-amd64 .

Write-Host "Compilando Linux arm64..."
$env:GOOS = "linux"; $env:GOARCH = "arm64"; go build -o dist/gokodek-linux-arm64 .

Write-Host "Compilando macOS (Intel)..."
$env:GOOS = "darwin"; $env:GOARCH = "amd64"; go build -o dist/gokodek-darwin-amd64 .

Write-Host "Compilando macOS (Apple Silicon)..."
$env:GOOS = "darwin"; $env:GOARCH = "arm64"; go build -o dist/gokodek-darwin-arm64 .

Write-Host "Compilando Windows amd64..."
$env:GOOS = "windows"; $env:GOARCH = "amd64"; go build -o dist/gokodek-windows-amd64.exe .

Remove-Item Env:\GOOS; Remove-Item Env:\GOARCH

Write-Host "`nBinarios generados exitosamente en dist/:" -ForegroundColor Green
Get-ChildItem dist/
