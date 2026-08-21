# clawops

ClawOps CLI — 터미널에서 전화와 문자를 다룬다.

```bash
clawops auth login
clawops messages send "점검 안내" --to 01000000000
clawops calls list --status failed --since 1h --json | jq -r '.[].hangupCause' | sort | uniq -c
```

> 현재 상태: **스캐폴드.** 커맨드 트리와 플래그 계약은 확정, 실행부는 미구현.

## 설치

```bash
brew install learners-superpumped/tap/clawops
# 또는
curl -fsSL https://cli.claw-ops.com/install | sh
```

## 인증

| 환경 | 방법 |
|---|---|
| 로컬 개발자 | `clawops auth login` — 브라우저 loopback (PKCE S256) |
| CI · 컨테이너 · 서버 | `CLAWOPS_API_KEY=sk_...` — OAuth 를 통째로 건너뛴다 |

`clawops auth login` 은 RFC 8252 native app 흐름을 쓴다. `127.0.0.1` 의 임의 포트에
콜백 서버를 띄우고, 서버는 `http://127.0.0.1:*/cb` 를 wildcard 로 허용한다.
엔드포인트는 `{issuer}/.well-known/openid-configuration` 으로 discovery 하므로
하드코딩하지 않는다.

발신·발송 권한(`write:calls`, `write:messages`)은 **실제로 요금이 발생**하므로
기본 로그인에 포함하지 않는다. 필요할 때 승격시킨다:

```bash
clawops auth refresh -s write:messages
```

### 프로필

서버의 OIDC grant 가 (사용자, 계정) 쌍으로 발급되므로 프로필 하나가 계정
하나에 대응한다.

```bash
clawops auth login --profile sandbox
clawops --profile sandbox calls list
```

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

## 레이아웃

```
cmd/clawops/        진입점
internal/cli/       커맨드 트리 (플래그 파싱 + 출력 선택만)
internal/api/       HTTP 클라이언트 — 도메인 판단 없음
internal/config/    프로필·자격증명 (키체인, 폴백 0600)
internal/output/    table / json 렌더러
```

## 개발

```bash
make build
make test
make lint
go run ./cmd/clawops --help
```

## 공개 레포 규칙

실계정·실번호·실키를 커밋하지 않는다. git 히스토리는 되돌릴 수 없다.
CI 가 `scripts/check-no-real-data.sh` 로 검사하며, 예시는 다음 더미로 고정한다.

| 종류 | 더미 |
|---|---|
| 휴대전화 | `01000000000` |
| 070 | `07000000000` |
| 계정 ID | `AC00000000000000000000000000000000` |
| 통화 ID | `CA00000000000000000000000000000000` |

## 라이선스

MIT
