# winserver (Windows 트레이 서버) 배포 가이드

`cmd/winserver`는 Astron Link **서버**를 사용자 세션에서 실행하는 Windows 트레이 앱입니다.
트레이 아이콘 + 네이티브 관리 콘솔(Gio) + 설정 다이얼로그를 제공합니다.

## 빌드

```bash
make winserver-windows   # dist/windows/astronlink-server.exe (버전 스탬프 포함)
# 또는 직접:
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build \
  -ldflags="-H windowsgui -X main.Version=$(git describe --tags --always) -X main.Commit=$(git rev-parse --short HEAD)" \
  -o dist/windows/astronlink-server.exe ./cmd/winserver/
```

순수 Go(CGO 불필요)라 Linux/WSL에서 크로스 컴파일됩니다. **GUI 동작은 Windows에서만 검증 가능합니다.**

## 폴더 배치 (예: `C:\astronlink\`)

```
astronlink-server.exe
server.yaml          # 첫 실행 시 자동 생성(없으면 템플릿 작성 후 메모장 오픈)
server.crt / server.key  # 자동 생성(자체서명)
server.log           # 로그(5MB 초과 시 시작할 때 server.log.1로 1세대 로테이션)
```

## 실행 방식 — 서비스 ❌ / 사용자 세션 ✅

Windows 서비스(session 0)는 **트레이 아이콘을 띄울 수 없습니다.** 따라서:

- NSSM `reversproxy` 서비스를 쓰던 경우 → **중지 + 수동(demand)으로 전환**하고 winserver를 사용
  ```cmd
  sc stop reversproxy
  sc config reversproxy start= demand
  ```
- winserver를 **로그온 자동 실행** 등록: 트레이 메뉴 → "로그온 시 자동 실행" 체크
  (HKCU\…\Run, 관리자 권한 불필요)
- 서비스와 winserver는 **동시 실행 금지**(같은 포트 바인딩). winserver는 단일 인스턴스 가드(named mutex)로 이중 실행을 막습니다.

## 트레이 메뉴

상태 / 서버 재시작 / 관리 콘솔 열기 / 설정 / 설정 파일 열기 / 로그 파일 열기 / 로그온 자동 실행 / 정보 / 종료

## 관리 콘솔 (웹 아님, 네이티브)

탭: **클라이언트**(이름·주소·접속시각·경과·터널수, 선택 끊기) / **터널**(종류·공개포트/호스트·로컬대상) /
**통계**(총·활성 연결, 수신/송신, 프록시 상태) / **로그**(자동 스크롤). 2초 자동 새로고침.
버튼: 새로고침 / 서버 재시작 / 설정 / Admin URL 복사 / 접속 정보 복사 / 정보 / 닫기.

## 보안 (실 배포 전 필수)

- `auth_token`은 **강한 값**으로(설정 다이얼로그의 "생성" 버튼). 클라이언트와 **동일**해야 연결됨.
- 가능하면 `insecure: false` + `tls_fingerprint`(클라이언트 인증서 SHA-256 핀닝) 또는 `client_ca_cert`.
- 내부망 프록시가 필요 없으면 `allow_private_targets`를 끄면 SSRF 가드가 켜집니다.
- `admin_token`을 설정하면 Admin API에 Bearer 인증이 걸립니다(비우면 loopback 무인증).

## 배포 전 포트 확인

서버를 시작하기 전에 `server.yaml` 의 `data_addr`·`http_addr`·`https_addr`·`admin_addr` 포트를 같은 PC 의 다른 프로그램이 쓰고 있지 않은지 확인합니다(기본값은 [설정 레퍼런스](configuration.md#수신-주소-기본값과-포트-충돌) 참고).

```powershell
# 포트별 점유 확인 (예: 8787) — 출력이 있으면 이미 누군가 그 포트를 듣고 있습니다
Get-NetTCPConnection -State Listen -LocalPort 8787 | Select-Object LocalAddress, LocalPort, OwningProcess
netstat -ano | findstr :8787

# 점유한 프로세스 이름 확인
Get-Process -Id <OwningProcess>
```

- 확인할 포트: 기본값 `8444`, `8080`, `8445`, `9090` 과 `server.yaml` 에 직접 적은 포트.
- 점유 중이면 `server.yaml` 에서 다른 포트로 바꿉니다. 예: 같은 PC 에서 `8787` 을 이미 쓰는 서비스가 있으면 `http_addr: ":8787"` 대신 다른 번호(`http_addr: ":8081"`)로 지정합니다.
- 포트를 바꾸면 클라이언트 측 방화벽 규칙과 접속 주소도 새 번호에 맞춥니다.

## 방화벽

`data_addr`/`http_addr`/`https_addr` 포트의 인바운드 허용 규칙이 필요합니다(외부 노출 시).
`admin_addr`는 loopback 권장.

## 코드 서명(선택, Windows 전용 수동 단계)

```cmd
signtool sign /fd SHA256 /tr http://timestamp.digicert.com /td SHA256 dist\winserver.exe
```
