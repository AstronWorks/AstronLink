# 설정 레퍼런스

설정은 YAML 파일(`--config`, 기본 `config.yaml`)과 명령줄 플래그로 지정합니다.

- 파일이 없으면 오류가 아니며 기본값으로 실행합니다.
- 알 수 없는 키가 있으면 파싱 오류로 기동을 거부합니다(`yaml` `KnownFields(true)`). 서버는 종료 코드 1, 클라이언트는 오류 로그 후 종료합니다.
- 플래그는 명령줄에 **직접 지정한 것만** 파일 값을 덮어씁니다(`flag.Visit`).

근거: `internal/config/config.go` (`DefaultServerConfig`, `DefaultClientConfig`, `loadYAML`), `cmd/server/main.go`, `cmd/client/main.go`.

## 서버 (`ServerConfig`)

| 키 | 타입 | 기본값 | 설명 |
|----|------|--------|------|
| `data_addr` | string | `:8444` | 노출 경로 데이터 연결 리스너 |
| `http_addr` | string | `:8080` | HTTP `Host` 라우팅 프록시. 빈 값이면 끔 |
| `https_addr` | string | `:8445` | HTTPS SNI 라우팅 프록시. 빈 값이면 끔 |
| `admin_addr` | string | `:9090` | 관리 API·대시보드. 빈 값이면 끔. `:포트` 처럼 호스트를 생략하면 `127.0.0.1` 에 바인딩 |
| `auth_token` | string | `""` | 기본 공유 토큰. `insecure: false` 에서 비어 있거나 `changeme` 면 기동 거부 |
| `admin_token` | string | `""` | 관리 API Bearer 토큰. 빈 값이면 인증 없음 |
| `cert_path` / `key_path` | string | `server.crt` / `server.key` | 없으면 자체 서명 인증서 생성. 서버는 TLS 클라이언트로만 동작하므로 현재 연결에는 쓰이지 않음 |
| `insecure` | bool | `false` | 클라이언트 인증서의 CA·호스트명 검증 생략. TOFU 지문 확인은 그대로 수행 |
| `client_ca_cert` | string | `""` | `insecure: false` 일 때 클라이언트 인증서를 검증할 CA. 비우면 시스템 루트 |
| `tls_fingerprint` | string | `""` | 파싱만 되고 코드에서 사용하지 않음. 지문 고정은 TOFU(`known_hosts.yaml`)로 수행 |
| `min_port` | int | `1` | 파싱만 되고 효과 없음. 출구 대상 포트 하한은 `1` 로 고정 |
| `allow_private_targets` | bool | `false` | 출구 다이얼(SOCKS5·HTTP CONNECT·포트 포워드·VPN)에서 루프백·사설·링크로컬 주소 허용 |
| `max_conn_rate` | float | `0` | IP별 초당 연결 수. `0` 이면 무제한 |
| `max_conn_burst` | int | `0` | IP별 버스트. 리미터가 만들어질 때 `0` 이하면 `10` 사용 |
| `max_concurrent` | int64 | `0` | 전역 동시 프록시 연결 상한. `0` 이면 무제한 |
| `log_level` | string | `info` | `debug` / `info` / `warn` / `error` (그 외 값은 `info`) |
| `known_hosts_path` | string | `known_hosts.yaml` | TOFU 지문 저장 파일 |
| `auto_approve` | bool | `false` | 미등록 클라이언트를 승인 없이 신뢰하고 저장 |
| `clients` | list | 없음 | 다이얼할 클라이언트 목록 (아래) |
| `file_transfer` | object | 꺼짐 | 파일 전송 ([FILE_TRANSFER.md](../FILE_TRANSFER.md)) |
| `vpn` | object | 아래 | VPN 모드 서버 설정 |

레이트 리미터는 `max_conn_rate > 0` 또는 `max_concurrent > 0` 일 때만 만들어지며, HTTP·HTTPS 프록시의 accept 루프에만 적용됩니다. TCP 공개 포트, 데이터 리스너, 출구 경로에는 적용되지 않습니다.

`clients[]`

| 키 | 설명 |
|----|------|
| `name` | 클라이언트 이름이자 TOFU 키. 클라이언트의 `name` 과 같아야 함 |
| `address` | 클라이언트가 리슨하는 `host:port` |
| `auth_token` | 클라이언트별 토큰. 비우면 서버 `auth_token` 사용 |

`vpn` (서버)

| 키 | 타입 | 기본값 | 설명 |
|----|------|--------|------|
| `enabled` | bool | `true` | 클라이언트의 VPN 요청 허용. 클라이언트가 요청할 때만 세션 생성 |
| `subnet` | string | `10.77.0.0/24` | 가상 네트워크. 첫 호스트 = 게이트웨이(서버), 두 번째 = 클라이언트. IPv4, `/30` 이하 |
| `mtu` | int | `1500` | 가상 링크 MTU (576 – 65535) |
| `dns` | list | `[]` | 클라이언트에 알려 줄 DNS. 비우면 게이트웨이 주소와 서버 내장 리졸버 |

### 수신 주소 기본값과 포트 충돌

서버가 여는 주소의 기본값입니다. 값은 코드의 기본값과 대조했고, 설치 스크립트가 만드는 `server.yaml` 과 `cmd/winserver` 가 첫 실행에 만드는 설정(`cmd/winserver/server.go:226` 이 `DefaultServerConfig` 를 사용)은 기본값과 같습니다.

| 키 | 코드 기본값 | 근거 | 다른 곳의 값 |
|----|-------------|------|--------------|
| `data_addr` | `:8444` | `internal/config/config.go:118` | 같음 (`scripts/install-server.ps1:47`) |
| `http_addr` | `:8080` | `internal/config/config.go:119` | 설치 스크립트는 같음(`scripts/install-server.ps1:48`), README 빠른 시작 예제는 `""`(끔, `README.md:185`) |
| `https_addr` | `:8445` | `internal/config/config.go:120` | 설치 스크립트는 같음(`scripts/install-server.ps1:49`), README 빠른 시작 예제는 `""`(끔, `README.md:186`) |
| `admin_addr` | `:9090` (호스트 생략 시 `127.0.0.1`) | `internal/config/config.go:121` | 같음 (`scripts/install-server.ps1:50`) |
| `file_transfer.receive_addr` | `127.0.0.1:8089` | `internal/config/config.go:283` | — |
| `file_transfer.control_addr` | `127.0.0.1:8077` | `internal/config/config.go:285` | — |

클라이언트의 기본 리스너도 같은 번호를 쓸 수 있습니다: `listen_addr` `:8443`(`config.go:292`), `socks_addr` `127.0.0.1:1080`(`config.go:303`), `http_proxy_addr` `127.0.0.1:8080`(`config.go:304`). 서버의 `http_addr`(`:8080`)와 클라이언트의 `http_proxy_addr`(`127.0.0.1:8080`)는 기본값이 같은 번호이므로, 한 PC 에서 서버와 클라이언트를 함께 실행하면 둘 중 하나를 바꿔야 합니다.

> **주의 — 같은 PC 의 다른 서비스와 포트가 겹치지 않게 지정하십시오.** `data_addr`·`http_addr`·`https_addr`·`admin_addr` 에 적은 포트를 같은 PC 의 다른 프로그램이 이미 쓰고 있으면 안 됩니다. 예를 들어 다른 서비스가 `0.0.0.0:8787` 을 쓰고 있다면 `http_addr` 를 `:8787` 로 지정하지 말고 다른 포트로 지정합니다. 리슨에 실패하면 현재 코드는 오류를 돌려주고, CLI 서버는 로그를 남긴 뒤 종료 코드 1 로 끝납니다(`cmd/server/main.go:168-171`). 설정을 정하기 전에 점검하는 방법은 [winserver 배포 가이드](winserver-deploy.md#배포-전-포트-확인)에 있습니다.

## 클라이언트 (`ClientConfig`)

| 키 | 타입 | 기본값 | 설명 |
|----|------|--------|------|
| `listen_addr` | string | `:8443` | 서버가 다이얼해 들어오는 TLS 리스너 |
| `auth_token` | string | `""` | 서버가 보낸 토큰과 비교. 클라이언트는 약한 토큰으로도 기동하며 `changeme` 일 때 경고만 남김 |
| `name` | string | `client1` | 서버 `clients[].name` 과 같아야 함 |
| `insecure` | bool | `false` | 현재 TLS 동작에 영향 없음. 클라이언트는 서버 인증서를 요구하지 않으며 서버 인증은 `auth_token` 으로 수행 |
| `tunnels` | list | 없음 | 서버에 등록할 터널 (아래) |
| `log_level` | string | `info` | `debug` / `info` / `warn` / `error` |
| `cert_path` / `key_path` | string | `client.crt` / `client.key` | 리스너 인증서. 없으면 자체 서명 인증서 생성 |
| `socks_addr` | string | `127.0.0.1:1080` | SOCKS5 리스너. 빈 값이면 끔 |
| `socks_user` / `socks_pass` | string | `""` | 둘 다 있으면 RFC 1929 인증 요구 |
| `http_proxy_addr` | string | `127.0.0.1:8080` | HTTP CONNECT 리스너. 빈 값이면 끔 |
| `http_proxy_user` / `http_proxy_pass` | string | `""` | 설정 키와 Windows 설정 창에는 있으나 현재 HTTP CONNECT 리스너에 적용되지 않음(인증 없음) |
| `port_forwards` | list | 없음 | 정적 포트 포워드 (아래) |
| `file_transfer` | object | 꺼짐 | 파일 전송 ([FILE_TRANSFER.md](../FILE_TRANSFER.md)) |
| `vpn` | object | 꺼짐 | VPN 모드 클라이언트 설정 (아래) |

`tunnels[]`

| 키 | 설명 |
|----|------|
| `type` | `tcp` / `http` / `https`. 빈 값은 `tcp` |
| `local_host` / `local_port` | 클라이언트 쪽 로컬 서비스 |
| `requested_port` | `tcp` 의 서버 공개 포트. `0` 이면 OS 가 할당 |
| `hostname` | `http` / `https` 의 라우팅 호스트명 |

`port_forwards[]`

| 키 | 설명 |
|----|------|
| `local_port` | 로컬 리슨 포트 |
| `remote_host` / `remote_port` | 서버가 다이얼할 대상 |
| `bind` | 로컬 바인드 주소. 빈 값이면 `127.0.0.1` |

`vpn` (클라이언트)

| 키 | 타입 | 기본값 | 설명 |
|----|------|--------|------|
| `enabled` | bool | `false` | VPN 모드 사용 (어댑터 생성에 관리자/root 권한 필요) |
| `interface_name` | string | `""` | 어댑터 이름. 비우면 Linux `rproxy0`, Windows `AstronLink` |
| `routes` | list | `[]` | 어댑터로 보낼 CIDR. 비우면 전체 터널(`0.0.0.0/1` + `128.0.0.0/1`). 주소만 쓰면 호스트 경로 |
| `bypass` | list | `[]` | 항상 물리 네트워크로 보낼 CIDR. 접속한 서버 주소는 자동으로 추가 |
| `dns` | list | `[]` | 서버가 알려 준 DNS 대신 사용할 서버 |
| `mtu` | int | `0` | 서버에 요청할 MTU. `0` 이면 서버 값 |

## 명령줄 플래그

서버 (`cmd/server`)

| 플래그 | 덮어쓰는 키 |
|--------|-------------|
| `--config` | 설정 파일 경로 (기본 `config.yaml`) |
| `--data-addr` | `data_addr` |
| `--http-addr` | `http_addr` |
| `--https-addr` | `https_addr` |
| `--admin-addr` | `admin_addr` |
| `--token` | `auth_token` |
| `--cert` / `--key` | `cert_path` / `key_path` |
| `--log-level` | `log_level` |

클라이언트 (`cmd/client`)

| 플래그 | 덮어쓰는 키 / 동작 |
|--------|--------------------|
| `--config` | 설정 파일 경로 (기본 `config.yaml`) |
| `--listen` | `listen_addr` |
| `--token` | `auth_token` |
| `--name` | `name` |
| `--insecure` | `insecure` |
| `--socks-addr` / `--socks-user` / `--socks-pass` | `socks_addr` / `socks_user` / `socks_pass` |
| `--http-proxy-addr` | `http_proxy_addr` |
| `--log-level` | `log_level` |
| `--cert` / `--key` | `cert_path` / `key_path` |
| `--local-host` (기본 `127.0.0.1`), `--local-port`, `--pub-port` | `--local-port > 0` 이면 TCP 터널을 `tunnels` 뒤에 추가 |
| `--http-host` + `--http-port` | 둘 다 있으면 HTTP 터널 추가 |
| `--https-host` + `--https-port` | 둘 다 있으면 HTTPS 터널 추가 |

클라이언트에는 파일 전송 서브커맨드 `astronlink-client send` 가 있습니다([FILE_TRANSFER.md](../FILE_TRANSFER.md)).

## 컴파일 타임 상수

`internal/config/defaults.go` 의 값으로, 설정으로 바꿀 수 없습니다. TLS 최소 버전(1.3)도 고정입니다.

| 상수 | 값 |
|------|----|
| `HandshakeTimeout` | 10s |
| `MessageReadTimeout` | 45s |
| `HeartbeatInterval` | 10s |
| `HeartbeatStaleThreshold` | 30s |
| `PongTimeout` | 10s |
| `DataConnWaitTimeout` | 15s |
| `SOCKSDialTimeout` | 15s |
| `SOCKSReadyTimeout` | 30s |
| `SOCKSHandshakeTimeout` | 30s |
| `TCPKeepAlivePeriod` | 15s |
| `ProxyReadTimeout` | 10s |
| `RelayBufSize` | 32768 |
| `MuxChannelBuffer` | 64 |
