#define MyAppName "gokodek"
#define MyAppVersion "0.1.0"
#define MyAppPublisher "gokodek"
#define MyAppExeName "gokodek.exe"

[Setup]
AppId={{B7B7D3C5-5A7D-4A9F-9D47-8D8A8B8C2E10}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppPublisher={#MyAppPublisher}
DefaultDirName={localappdata}\Programs\gokodek
DefaultGroupName={#MyAppName}
DisableProgramGroupPage=yes
PrivilegesRequired=lowest
OutputDir=..\dist
OutputBaseFilename=gokodek-setup-{#MyAppVersion}
Compression=lzma
SolidCompression=yes
WizardStyle=modern
Uninstallable=yes

[Files]
Source: "..\dist\gokodek.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\README.md"; DestDir: "{app}"; Flags: ignoreversion

[Registry]
Root: HKCU; Subkey: "Environment"; ValueType: expandsz; ValueName: "Path"; ValueData: "{olddata};{app}"; Flags: preservestringtype

[Run]
Filename: "{app}\gokodek.exe"; Description: "Iniciar gokodek"; Flags: postinstall nowait skipifsilent

[UninstallDelete]
Type: filesandordirs; Name: "{app}"
