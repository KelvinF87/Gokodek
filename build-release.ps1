$ErrorActionPreference = 'Stop'

$Root = Split-Path -Parent $MyInvocation.MyCommand.Path
$BuildScript = Join-Path $Root 'build-windows.ps1'
if (-not (Test-Path $BuildScript)) { throw "No se encontró $BuildScript" }
& $BuildScript
$exe = Join-Path $Root 'dist\gokodek.exe'
if (-not (Test-Path $exe)) { throw "La compilación no generó $exe" }

$iscc = Get-Command ISCC.exe -ErrorAction SilentlyContinue
if (-not $iscc) {
    $candidates = @(
        (Join-Path ${env:ProgramFiles(x86)} 'Inno Setup 6\ISCC.exe'),
        (Join-Path $env:ProgramFiles 'Inno Setup 6\ISCC.exe')
    )
    foreach ($candidate in $candidates) {
        if ($candidate -and (Test-Path $candidate)) { $iscc = @{ Source = $candidate }; break }
    }
}

if (-not $iscc) {
    Write-Warning 'Inno Setup 6 was not found. dist\gokodek.exe was built, but no setup EXE was generated.'
    Write-Host 'Install Inno Setup 6 on the build machine and run this script again.'
    exit 0
}

$iss = Join-Path $Root 'installer\gokodek.iss'
if (-not (Test-Path $iss)) { throw "No se encontró $iss" }
& $iscc.Source $iss
if ($LASTEXITCODE -ne 0) { throw "Inno Setup terminó con código $LASTEXITCODE" }
$setup = Join-Path $Root 'dist\gokodek-setup-0.1.0.exe'
if (-not (Test-Path $setup)) { throw "No se generó $setup" }
Write-Host "Created $setup"
