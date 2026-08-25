@echo off
setlocal
powershell.exe -NoLogo -NoProfile -ExecutionPolicy Bypass -File "%~dp0build-release.ps1"
if errorlevel 1 (
  echo.
  echo ERROR: no se pudo crear el instalador.
  pause
  exit /b 1
)
echo.
echo Instalador creado en dist\gokodek-setup-0.1.0.exe
pause
