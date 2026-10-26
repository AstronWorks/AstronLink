# 관리 API와 대시보드

서버의 `admin_addr`(기본 `:9090`)에서 하나의 HTTP 서버가 대시보드, JSON API, SSE 이벤트 스트림을 제공합니다.

근거: `internal/admin/api.go` (`Server.Start`, `StartWithListener`, 각 `handle*`), `internal/admin/events.go`, `internal/admin/ui/`, `cmd/server/main.go` (`tofuCheck`, `dialClientLoop`).

## 바인딩과 인증

- `admin_addr` 를 `:9090` 처럼 호스트 없이 지정하면 `127.0.0.1:9090` 에만 바인딩합니다. 외부에서 접근하려면 `0.0.0.0:9090` 처럼 호스트를 명시합니다. 빈 값이면 관리 서버를 띄우지 않습니다.
- `admin_token` 을 설정하면 `/api/*` 요청에 `Authorization: Bearer <admin_token>` 이 필요하고, 틀리면 `401` 입니다(상수 시간 비교). 빈 값이면 인증하지 않습니다.
- 대시보드 HTML(`/`)과 정적 파일(`/static/`)은 인증하지 않습니다.
- 대시보드는 `Authorization` 헤더를 보내지 않으므로, `admin_token` 을 설정하면 대시보드의 데이터 호출이 `401` 로 실패합니다. 이 경우 API 를 직접 호출하십시오.

## 엔드포인트

액션 파라미터는 쿼리 스트링으로 넘기며 요청 본문은 쓰지 않습니다. 허용되지 않은 메서드는 `405` 입니다.

| 메서드 | 경로 | 응답 | 오류 |
|--------|------|------|------|
| GET | `/api/clients` | `{"clients":[{"id","name","addr","connected_at"}]}` | |
| GET | `/api/tunnels` | `{"tunnels":[{"id","client_id","type","local_addr","public_addr"?,"hostname"?}]}` — `type` 은 `tcp`·`http`·`https` | |
| GET | `/api/stats` | `{"total_connections","active_connections","bytes_in","bytes_out","tunnels"?}` | |
| GET | `/api/pending` | `{"pending":[{"client_name","addr","fingerprint","requested_at"}]}` | |
| GET | `/api/known-hosts` | `{"hosts":[{"Name","Fingerprint"}]}` — 키가 대문자로 시작 | |
| DELETE | `/api/known-hosts?name=<이름>` | `{"ok":true}` — 다음 다이얼 때 다시 승인 필요 | `400` 이름 없음 |
| POST | `/api/decide?name=<이름>&action=approve\|reject` | `{"ok":true}` | `400` 파라미터 오류, `404` 대기 요청 없음, `503` 승인 기능 없음 |
| POST | `/api/reconnect?name=<이름>` | `{"ok":true,"disconnected":<개수>}` — 서버가 재다이얼 | `400` 이름 없음 |
| GET | `/api/events` | `text/event-stream` | `503` 이벤트 기능 없음 |

```bash
curl http://127.0.0.1:9090/api/clients
curl -X POST "http://127.0.0.1:9090/api/decide?name=client1&action=approve"
curl -H "Authorization: Bearer <admin_token>" http://127.0.0.1:9090/api/stats
```

### `/api/stats` 의 값

- `total_connections` / `active_connections` 는 서버가 맺은 **제어 연결** 수입니다(`cmd/server` 의 다이얼 루프에서 증감).
- `bytes_in` / `bytes_out` 과 터널별 `tunnels` 통계는 현재 데이터 경로에 집계가 연결되어 있지 않아 `0` 이거나 생략됩니다.

## SSE 이벤트 (`/api/events`)

- 연결 직후 `: connected` 주석을 보내고, 이벤트는 `data: <json>\n\n` 형식입니다. 15초마다 `: ping` 주석을 보내려 합니다.
- 이벤트 종류(`cmd/server`): `client.connected`, `client.disconnected`, `client.pending`, `client.approved`, `client.rejected`, `client.fingerprint_mismatch`.
- 구독자 버퍼가 가득 차면 이벤트를 버립니다(발행자는 막히지 않음).
- 관리 HTTP 서버의 `WriteTimeout` 이 10초이므로, 스트림은 연결 10초 뒤 첫 쓰기(이벤트 또는 15초 ping)에서 끊깁니다. 브라우저의 `EventSource` 는 자동으로 다시 연결하지만, 끊기기 직전에 발행된 이벤트는 놓칠 수 있습니다.

## 대시보드 (`/`)

- 2초마다 `/api/clients`, `/api/tunnels`, `/api/stats`, `/api/pending`, `/api/known-hosts` 를 읽고, `/api/events` 를 `EventSource` 로 구독합니다.
- 동작: 대기 클라이언트 승인·거부, 클라이언트 재연결, 신뢰 호스트 철회.
- 알려진 제약: `/api/known-hosts` 는 `Name`·`Fingerprint` 키로 응답하지만 대시보드는 소문자 키를 읽기 때문에, 승인된 호스트 표에 이름·지문이 비어 보이고 철회 버튼은 빈 이름을 보냅니다(`400`). 철회는 API 를 직접 호출하십시오.

## Windows 트레이 서버 (`cmd/winserver`)

`cmd/winserver` 는 `internal/app.ServerApp` 으로 서버를 실행하며 관리 서버에 승인 기능과 이벤트 버스를 연결하지 않습니다. 따라서 `/api/pending` 은 항상 빈 목록, `/api/decide` 는 `503`, `/api/events` 는 `503` 입니다.
