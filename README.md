# clawops

ClawOps CLI — 터미널에서 전화와 문자를 다룬다.

```bash
clawops auth login
clawops messages list --status failed
clawops messages list --limit 200 --json | jq -r '.[].to' | sort | uniq -c
```

> 현재 상태: `auth` 와 `messages list` / `messages get` 이 동작한다.
> `messages send` · `calls` · `numbers` 는 커맨드 트리와 플래그 계약만 확정된
> 상태이고 실행부는 미구현이다. `calls` / `numbers` 를 호출하면 뜨는
> 403 `insufficient_scope` 도 서버 쪽 scope 매핑이 아직 안 열려서다.

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

발신·발송 권한(`write:calls`, `write:messages`)은 실제로 요금이 발생하므로
기본 로그인에 포함하지 않는다. 필요할 때 승격한다:

```bash
clawops auth refresh -s write:messages
```

여러 계정을 쓴다면 프로필로 나눈다:

```bash
clawops auth login --profile sandbox
clawops --profile sandbox calls list
```

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
