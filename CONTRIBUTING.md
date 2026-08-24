# 기여 가이드

## 설계 원칙

**1. spec 에서 생성 가능한 것만 담는다.**
커맨드 트리는 `app/src/swagger/openapi.bundled.json`(operationId 112 개, 태그 15 개)에서
생성한다. 손으로 다듬는 것은 자주 쓰는 상위 15~20 개뿐이다.

**2. 판단은 서버에 민다.**
VoiceML 검증, DNC 판정, 요금 계산, 메시지 타입(SMS/LMS/MMS) 결정 — 전부 서버가 한다.
CLI 안에 두 번째 진실의 원본을 만들면, CLI 가 "괜찮다" 고 한 것을 서버가 거절하는
순간부터 아무도 CLI 를 믿지 않는다.

**3. `--json` 은 1급 계약이다.**
CLI 가 SDK·MCP·대시보드와 겹치지 않는 유일한 영역이 파이프다. JSON 모드에서
stdout 에는 데이터만 나가고, 사람용 메시지는 전부 stderr 로 간다.

**4. "보냈다" 와 "도착했다" 를 구분한다.**
`--watch` 는 종착 상태까지 따라가고 exit code 로 결과를 낸다. 문자는 `queued` 에서
멎는 실패 모드가 실재하므로, 발송 API 가 200 을 줬다는 사실만으로 성공이라고
말하지 않는다.

## 인증 흐름 내부

`clawops auth login` 은 RFC 8252 native app 흐름을 쓴다. `127.0.0.1` 의 임의 포트에
콜백 서버를 띄우고, 서버는 native 클라이언트의 loopback redirect 에서 포트를
무시한다(RFC 8252 §7.3) — 등록값은 포트 없는 `http://127.0.0.1/cb` 하나다.
엔드포인트는 `{issuer}/.well-known/openid-configuration` 으로 discovery 하므로
하드코딩하지 않는다.

### refresh 직렬화

서버가 `rotateRefreshToken: true` 로 돈다. `clawops` 는 여러 셸에서 동시에 돌 수
있는데, 두 프로세스가 동시에 refresh 하면 한쪽이 재사용으로 판정돼 grant 전체가
revoke 되고 사용자는 이유 없이 로그아웃당한다. 그래서 모든 갱신은 파일 락 안에서
직렬화하고, 락을 잡은 뒤 저장소를 다시 읽는다 — 기다리는 동안 다른 프로세스가
이미 갱신했다면 우리가 든 refresh token 은 그 시점에 무효이기 때문이다.

## 자격증명 저장 경로

`{config}` 는 Go 의 `os.UserConfigDir()` 이 정하는 OS 별 표준 설정 디렉터리다.

| OS | 실제 경로 |
|---|---|
| macOS | `~/Library/Application Support/clawops/` |
| Linux | `$XDG_CONFIG_HOME/clawops/` (미설정 시 `~/.config/clawops/`) |
| Windows | `%AppData%\clawops\` |

⚠️ macOS 에서 `~/.config/clawops/` 를 보고 "설정이 없다 = 로그인한 적 없다" 고
판단하지 말 것. 경로를 직접 쓰는 대신 `clawops auth status` 로 확인한다.

설정 파일에는 토큰을 넣지 않는다. `auth status` 로 프로필을 확인하는 데 키체인을
열 필요가 없고, 설정 파일을 실수로 공유해도 자격증명이 새지 않는다.

## 검증

```bash
make lint && make test && ./scripts/check-no-real-data.sh
```
