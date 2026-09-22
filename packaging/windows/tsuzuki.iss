; Windows installer for tsuzuki, built by .github/workflows/release.yml.
;
;   ISCC /DVersion=0.4.0 tsuzuki.iss
;
; Expects tsuzuki.exe for each architecture in amd64\ and arm64\ beside this
; file, along with LICENSE.txt. Needs Inno Setup 6.3 or newer for arm64.

#define AppName "tsuzuki"
#define AppPublisher "EmoFa"
#define AppURL "https://github.com/EmoFa/tsuzuki"

#ifndef Version
  #error Define Version, e.g. ISCC /DVersion=0.4.0 tsuzuki.iss
#endif

[Setup]
; Never changes: it's how Windows recognises an existing installation.
AppId={{F06DB79A-9D7B-4EE8-AA82-B57A90BA884A}
AppName={#AppName}
AppVersion={#Version}
AppPublisher={#AppPublisher}
AppPublisherURL={#AppURL}
AppSupportURL={#AppURL}/issues
AppUpdatesURL={#AppURL}/releases
VersionInfoVersion={#Version}
DefaultDirName={autopf}\{#AppName}
; A command needs no Start Menu folder.
DisableProgramGroupPage=yes
; Installs for one user without asking for administrator, but offers to install
; for everyone to anyone who wants that.
PrivilegesRequired=lowest
PrivilegesRequiredOverridesAllowed=dialog
ArchitecturesAllowed=x64compatible or arm64
ArchitecturesInstallIn64BitMode=x64compatible or arm64
OutputBaseFilename={#AppName}_{#Version}_windows_setup
SetupIconFile=tsuzuki.ico
UninstallDisplayIcon={app}\{#AppName}.exe
LicenseFile=LICENSE.txt
InfoAfterFile=after-install.txt
Compression=lzma2/max
SolidCompression=yes
WizardStyle=modern
; Tells Explorer that PATH changed, so new terminals pick it up.
ChangesEnvironment=yes

[Files]
Source: "amd64\tsuzuki.exe"; DestDir: "{app}"; Check: not IsArm64; Flags: ignoreversion
Source: "arm64\tsuzuki.exe"; DestDir: "{app}"; Check: IsArm64; Flags: ignoreversion

[Tasks]
Name: "addtopath"; Description: "Add tsuzuki to PATH, so it runs from any terminal"

[Registry]
Root: HKA; Subkey: "{code:EnvironmentKey}"; ValueType: expandsz; ValueName: "Path"; \
    ValueData: "{olddata};{app}"; Tasks: addtopath; Check: NotOnPath(ExpandConstant('{app}'))

[Code]
// Where PATH lives depends on whether this is an install for everyone.
function EnvironmentKey(Param: string): string;
begin
  if IsAdminInstallMode then
    Result := 'SYSTEM\CurrentControlSet\Control\Session Manager\Environment'
  else
    Result := 'Environment';
end;

function PathValue(var Value: string): Boolean;
begin
  Result := RegQueryStringValue(HKEY_AUTO, EnvironmentKey(''), 'Path', Value);
end;

// NotOnPath keeps a reinstall from adding the directory twice.
function NotOnPath(Dir: string): Boolean;
var
  Path: string;
begin
  if not PathValue(Path) then
    Result := True
  else
    Result := Pos(';' + Uppercase(Dir) + ';', ';' + Uppercase(Path) + ';') = 0;
end;

// Uninstalling takes the directory back out of PATH, leaving the rest alone.
procedure RemoveFromPath(Dir: string);
var
  Path: string;
  Start: Integer;
begin
  if not PathValue(Path) then
    Exit;
  Start := Pos(';' + Uppercase(Dir) + ';', ';' + Uppercase(Path) + ';');
  if Start = 0 then
    Exit;
  Delete(Path, Start, Length(Dir) + 1);
  RegWriteExpandStringValue(HKEY_AUTO, EnvironmentKey(''), 'Path', Path);
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
begin
  if CurUninstallStep = usUninstall then
    RemoveFromPath(ExpandConstant('{app}'));
end;
