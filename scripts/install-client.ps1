<#
.SYNOPSIS
    Install the reversproxy Windows GUI (system-tray) client.
.DESCRIPTION
    Per-user install (no admin required): downloads the tray client, creates a
    default config.yaml (with a file_transfer block), registers it to auto-start
    at logon, registers the Explorer right-click "파일 전송" menu, and launches it.
.USAGE
    # PowerShell (일반 사용자 권한):
    irm https://raw.githubusercontent.com/EcoKG/reversproxy/master/scripts/install-client.ps1 | iex
#>

$ErrorActionPreference = "Stop"
$Repo = "EcoKG/reversproxy"
$InstallDir = Join-Path $env:LOCALAPPDATA "reversproxy"
$BinaryName = "reversproxy-client.exe"
$BinaryPath = Join-Path $InstallDir $BinaryName
$ConfigPath = Join-Path $InstallDir "config.yaml"

Write-Host "==> reversproxy GUI 클라이언트 설치 (Windows)" -ForegroundColor Cyan

New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null

# 최신 릴리스에서 GUI 클라이언트(winclient) 바이너리 내려받기.
$ReleasesApi = "https://api.github.com/repos/$Repo/releases/latest"
try {
    $Release = Invoke-RestMethod -Uri $ReleasesApi
    $Asset = $Release.assets | Where-Object { $_.name -like "*client-windows*" } | Select-Object -First 1
    $DownloadUrl = $Asset.browser_download_url
    $Version = $Release.tag_name
} catch {
    Write-Host "==> 릴리스 조회 실패, 직접 URL 사용" -ForegroundColor Yellow
    $DownloadUrl = "https://github.com/$Repo/releases/latest/download/reversproxy-client-windows-amd64.exe"
    $Version = "latest"
}
Write-Host "==> 다운로드 $Version : $DownloadUrl"
Invoke-WebRequest -Uri $DownloadUrl -OutFile $BinaryPath

# VPN 모드용 wintun.dll (공식 배포본). 실행 파일 옆에 두어야 하며, 설치 전에
# WireGuard LLC Authenticode 서명을 검증한다. 실패해도 VPN 모드만 영향받는다.
$WintunDll = Join-Path $InstallDir "wintun.dll"
if (-not (Test-Path $WintunDll)) {
    $WintunZip = Join-Path $env:TEMP "reversproxy-wintun.zip"
    $WintunTmp = Join-Path $env:TEMP "reversproxy-wintun"
    try {
        Write-Host "==> wintun.dll 다운로드 (VPN 모드용)"
        Invoke-WebRequest -Uri "https://www.wintun.net/builds/wintun-0.14.1.zip" -OutFile $WintunZip
        Expand-Archive -Path $WintunZip -DestinationPath $WintunTmp -Force
        $WintunArch = if ($env:PROCESSOR_ARCHITECTURE -eq "ARM64") { "arm64" } else { "amd64" }
        $WintunSrc = Join-Path $WintunTmp "wintun\bin\$WintunArch\wintun.dll"
        $Sig = Get-AuthenticodeSignature -FilePath $WintunSrc
        if ($Sig.Status -ne "Valid" -or $Sig.SignerCertificate.Subject -notmatch "WireGuard LLC") {
            throw "서명 검증 실패 ($($Sig.Status))"
        }
        Copy-Item -Path $WintunSrc -Destination $WintunDll
    } catch {
        Write-Host "==> wintun.dll 설치 실패 — VPN 모드를 쓰려면 https://www.wintun.net 에서 받아 $InstallDir 에 두십시오: $_" -ForegroundColor Yellow
    } finally {
        Remove-Item -Path $WintunZip -Force -ErrorAction SilentlyContinue
        Remove-Item -Path $WintunTmp -Recurse -Force -ErrorAction SilentlyContinue
    }
}

# 기본 config.yaml 생성 (이미 있으면 건드리지 않음).
if (-not (Test-Path $ConfigPath)) {
    Write-Host "==> 기본 설정 생성: $ConfigPath"
    @"
# reversproxy GUI 클라이언트 설정
listen_addr: "0.0.0.0:8443"      # 서버가 이 주소로 접속 (인바운드 허용 필요)
auth_token: "changeme"
name: "$env:COMPUTERNAME"
insecure: false
log_level: "info"
tunnels: []

# 파일 전송 (드롭 폴더 수신 + 우클릭 전송). 배선 설명은 FILE_TRANSFER.md 참고.
file_transfer:
  enabled: true
  receive_addr: "127.0.0.1:8089"
  drop_dir: "received"
  token: "changeme-ft"
  send_endpoint: "http://127.0.0.1:8090"   # 터널 너머 상대 수신기로 닿는 로컬 주소
  control_addr: "127.0.0.1:8077"
  max_file_size: 0

# VPN 모드 (가상 네트워크 어댑터). 관리자 권한으로 실행해야 하며 wintun.dll 필요.
# 최초 핸드셰이크 후 어댑터가 올라오고, 이후 트래픽은 어댑터를 통해 서버로 흐른다.
vpn:
  enabled: false
  routes: []      # 비우면 전체 터널. 예: ["10.0.0.0/8"]
  bypass: []
"@ | Set-Content -Path $ConfigPath -Encoding UTF8
}

# 로그온 시 자동 시작 (HKCU Run — 관리자 권한 불필요).
$runKey = "HKCU:\Software\Microsoft\Windows\CurrentVersion\Run"
Set-ItemProperty -Path $runKey -Name "Reversproxy" -Value "`"$BinaryPath`""

# 탐색기 우클릭 "파일 전송" 메뉴 등록.
Write-Host "==> 우클릭 메뉴 등록"
& $BinaryPath register-menu

# 트레이 앱 실행.
Write-Host "==> 트레이 클라이언트 실행"
Start-Process -FilePath $BinaryPath

Write-Host ""
Write-Host "==> 설치 완료!" -ForegroundColor Green
Write-Host ""
Write-Host "    설정 편집:   notepad `"$ConfigPath`""
Write-Host "    트레이 메뉴:  연결 / 수신함 열기 / 우클릭 메뉴 등록·해제"
Write-Host "    파일 전송:    탐색기에서 파일 우클릭 → 'Reversproxy로 파일 전송'"
Write-Host "                 (Windows 11은 '추가 옵션 표시' 안에 표시)"
Write-Host ""
