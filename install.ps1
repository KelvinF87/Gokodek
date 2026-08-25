param(
    [string]$SourceDirectory = (Split-Path -Parent $MyInvocation.MyCommand.Path)
)

$ErrorActionPreference = 'Stop'
$source = Join-Path $SourceDirectory 'gokodek.exe'
if (-not (Test-Path $source)) {
    throw "No se encontró gokodek.exe en $SourceDirectory. Ejecuta build-windows.ps1 primero."
}

$installDir = Join-Path $env:LOCALAPPDATA 'Programs\gokodek'
New-Item -ItemType Directory -Path $installDir -Force | Out-Null
Copy-Item $source (Join-Path $installDir 'gokodek.exe') -Force

$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
$pathItems = @()
if ($userPath) { $pathItems = $userPath -split ';' | Where-Object { $_ } }
if ($pathItems -notcontains $installDir) {
    [Environment]::SetEnvironmentVariable('Path', (($pathItems + $installDir) -join ';'), 'User')
}
$env:Path = "$installDir;$env:Path"

Write-Host "gokodek instalado en $installDir"
Write-Host 'Cierra y abre PowerShell para usar el comando gokodek desde cualquier directorio.'
& (Join-Path $installDir 'gokodek.exe') -help
