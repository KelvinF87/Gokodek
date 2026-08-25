$ErrorActionPreference = 'Stop'
$installDir = Join-Path $env:LOCALAPPDATA 'Programs\gokodek'
if (Test-Path $installDir) {
    Remove-Item -Recurse -Force $installDir
}
$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
if ($userPath) {
    $cleanPath = ($userPath -split ';' | Where-Object { $_ -and $_ -ne $installDir }) -join ';'
    [Environment]::SetEnvironmentVariable('Path', $cleanPath, 'User')
}
Write-Host 'gokodek desinstalado para el usuario actual.'
