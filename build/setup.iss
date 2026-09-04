; SM GUI 安装包脚本（Inno Setup 6）
; CI 调用: ISCC.exe build\setup.iss /DAppVersion=v1.2.3
; 打包内容与 zip 一致：主程序 + bin 双内核 + run/rules 规则集（release\sm-gui 暂存目录）

#ifndef AppVersion
#define AppVersion "0.0.0"
#endif

[Setup]
AppId={{8F1E5A34-9D2C-4B7E-A6D1-3C5F0E2B7A91}
AppName=SM GUI
AppVersion={#AppVersion}
AppPublisher=evecus
AppPublisherURL=https://github.com/evecus/sm-gui
; 按用户安装（localappdata），避免 Program Files 写入 data/run/configs 需要管理员的问题
PrivilegesRequired=lowest
DefaultDirName={localappdata}\Programs\SM GUI
DefaultGroupName=SM GUI
; Inno 的路径相对于本 .iss 文件所在目录（build\），仓库根是 ..
OutputDir=..
OutputBaseFilename=SM-GUI-{#AppVersion}-setup
Compression=lzma2
SolidCompression=yes
ArchitecturesInstallIn64BitMode=x64compatible
UninstallDisplayIcon={app}\sm-gui.exe
DisableProgramGroupPage=yes
CloseApplications=yes

[Tasks]
Name: "desktopicon"; Description: "创建桌面快捷方式"; GroupDescription: "附加任务:"

[Files]
; 整个暂存目录（sm-gui.exe、bin\、run\rules\、README、config.example.json）
Source: "..\release\sm-gui\*"; DestDir: "{app}"; Flags: recursesubdirs createallsubdirs ignoreversion

[Icons]
Name: "{autoprograms}\SM GUI"; Filename: "{app}\sm-gui.exe"
Name: "{autodesktop}\SM GUI"; Filename: "{app}\sm-gui.exe"; Tasks: desktopicon

[Run]
Filename: "{app}\sm-gui.exe"; Description: "运行 SM GUI"; Flags: nowait postinstall skipifsilent

[Messages]
; 卸载提示：运行时生成的用户数据（data/ configs/ 等）会保留
ConfirmUninstall=确定要卸载 SM GUI 吗？%n%n运行时生成的用户数据（节点库、配置文件等）将保留，可手动删除安装目录。
