# clawops

ClawOps CLI — 터미널에서 전화와 문자를 다룬다.

```bash
clawops auth login
clawops messages send "점검 안내" --to 01000000000
clawops messages list --status failed
clawops messages list --limit 200 --json | jq -r '.[].to' | sort | uniq -c
```

## 커맨드

플래그와 예시는 `--help` 가 정본이다. 여기 옮겨 적으면 반드시 어긋난다.

```bash
clawops --help
clawops messages send --help
```

| 리소스 | 하위 명령 | 상태 |
|---|---|---|
| `auth` | `login` · `logout` · `status` · `refresh` | 동작 |
| `messages` | `send` · `list` · `get` | 동작 |
| `calls` | `create` · `list` · `get` | 미구현 |
| `numbers` | `list` | 미구현 (서버가 막혀 있음) |

미구현은 커맨드 트리와 플래그 계약만 확정된 상태다.

`numbers` 는 CLI 를 붙여도 아직 못 쓴다. **REST 의 numbers 라우트가 OIDC
access token 을 받지 않는다** — `read:phone_numbers` 를 가진 토큰으로 불러도
403 `접근 권한 없음` 이다(같은 토큰으로 `messages` 는 된다). 스코프 부족이
아니라 라우트가 API 키 경로에만 있는 것이라, `WWW-Authenticate` 에 scope 힌트도
없어서 CLI 가 승격 명령을 안내하지 못한다. 서버에서 먼저 열어야 한다.

## 설치

```bash
brew install clawopshq/tap/clawops
# 또는
curl -fsSL https://cli.claw-ops.com/install | sh
```

## 인증

| 환경 | 방법 |
|---|---|
| 로컬 개발자 | `clawops auth login` — 브라우저에서 로그인 |
| CI · 컨테이너 · 서버 | `CLAWOPS_API_KEY=sk_...` |

발신·발송 권한(`write:calls`, `write:messages`)은 실제로 요금이 발생하므로 기본
로그인에 없다. 필요한 순간 403 과 함께 승격 명령이 그대로 뜨므로 미리 외울 것은 없다.

계정이 여럿이면 프로필로 나눈다 (`--profile`). 프로필 하나가 계정 하나다.

## 자격증명 저장

토큰은 OS 키체인(macOS Keychain / libsecret / wincred)에, 나머지 설정은
`{config}/clawops/config.toml` (0600) 에 저장한다. 키체인을 쓸 수 없는 환경
(헤드리스 리눅스 등)에서만 `credentials.json` (0600) 으로 내려간다.

경로를 직접 보지 말고 `clawops auth status` 로 확인한다 — OS 별 실제 경로와
저장 방식을 이렇게 나눈 이유는 [CONTRIBUTING.md](CONTRIBUTING.md) 에 있다.

## 레이아웃

```
cmd/clawops/        진입점
internal/cli/       커맨드 트리 (플래그 파싱 + 출력 선택만)
internal/api/       HTTP 클라이언트 — 도메인 판단 없음
internal/config/    프로필·자격증명 (키체인, 폴백 0600)
internal/output/    table / json 렌더러
```

왜 이렇게 나눴는지는 [CONTRIBUTING.md](CONTRIBUTING.md) 의 설계 원칙 참고.

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
