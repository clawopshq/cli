# clawops

ClawOps CLI — 터미널에서 전화와 문자를 다룬다.

```bash
clawops auth login
clawops messages send "점검 안내" --to 01000000000
clawops messages list --status failed
clawops messages list --limit 200 --json | jq -r '.[].to' | sort | uniq -c
```

> 현재 상태: `auth` 와 `messages` (`send` / `list` / `get`) 가 동작한다.
> `calls` · `numbers` 는 커맨드 트리와 플래그 계약만 확정된 상태이고 실행부는
> 미구현이다. 호출하면 뜨는 403 `insufficient_scope` 도 서버 쪽 scope 매핑이
> 아직 안 열려서다.

## 문자 보내기

```bash
# SMS — --type 을 생략하면 서버 기본값 sms
clawops messages send "서버 점검은 오늘 밤 12시부터입니다" --to 01000000000

# LMS — 제목을 쓰려면 --type lms 가 필요하다
clawops messages send --type lms --subject "이용약관 변경 안내" \
  --body-file notice.txt --to 01000000000

# MMS — 이미지 최대 3장 (jpg·jpeg·png·bmp)
clawops messages send --type mms --to 01000000000 \
  --media-url https://cdn.example.com/promo.jpg "신규 매장 오픈 안내"

# 도착까지 확인하고 스크립트에서 분기 (실패면 exit 1)
clawops messages send "인증번호는 482913입니다" --to 01000000000 --wait \
  && echo "발송 확인됨"

# 보내지 않고 조립된 요청만 확인
clawops messages send "본문" --to 01000000000 --dry-run
```

발송 권한은 요금이 발생하므로 기본 로그인에 없다. 처음 보낼 때 403 이 뜨면
`clawops auth refresh -s write:messages` 로 승격한다 (CLI 가 그 명령을 그대로 띄워 준다).

`--type` 은 **명시**해야 한다. 서버는 본문 길이나 첨부 유무로 타입을 추측해 올려주지
않는다 — 그러면 사용자가 모르는 사이에 단가가 바뀌기 때문이다. 조합이 안 맞으면
(`--type sms` 에 `--subject`) 서버가 400 으로 거절한다.

수신자는 한 번에 한 명이다. 서버 API 자체가 건당 한 명만 받으므로 여러 명은 셸에서
반복한다 — 어느 건이 실패했는지 CLI 가 뭉뚱그리지 않는다.

```bash
for n in 01000000001 01000000002; do
  clawops messages send "공통 공지사항입니다" --to "$n" || echo "실패: $n"
done
```

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
