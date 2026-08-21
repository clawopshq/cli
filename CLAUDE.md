# clawops-cli

ClawOps 공식 CLI (Go). **공개 레포**이며 MIT 다.

## 절대 규칙

1. **실계정·실번호·실키를 커밋하지 않는다.** 공개 레포라 히스토리가 되돌아가지
   않는다. 예시는 README 의 더미 표를 따른다. `scripts/check-no-real-data.sh` 가 CI 게이트다.
2. **도메인 판단을 CLI 에 넣지 않는다.** VoiceML 파싱, DNC 판정, 요금 계산,
   메시지 타입 결정은 전부 서버 몫이다. 검증이 필요하면 서버 엔드포인트를 추가하고
   CLI 는 호출만 한다. 이 규칙을 어기면 CLI 와 서버가 서로 다른 답을 내기 시작한다.
3. **JSON 출력에 사람용 텍스트를 섞지 않는다.** stdout 은 데이터 전용, 안내는 stderr.

## 인증 배경 (서버 쪽 사실)

서버는 `node-oidc-provider` 로 돌고 아래는 **이미 구현돼 있다** — 다시 만들지 말 것.

- PKCE S256 강제 (`pkce.required: () => true`)
- loopback wildcard redirect: `http://127.0.0.1:*/cb` (RFC 8252 §7.3)
- `/.well-known/openid-configuration` root alias → 엔드포인트 discovery 가능
- refresh token 30 일, `offline_access` scope
- revocation (RFC 7009)
- scope: `read:*` 16 개 / `write:*` 12 개

**미구현**: Device Flow. `adapter.findByUserCode()` 가 `undefined` 를 반환한다.
headless 가 실제로 필요해지면 그때 서버에 붙인다 — v1 은 API 키로 커버한다.

### 반드시 지킬 것 — refresh 직렬화

서버가 `rotateRefreshToken: true` 로 돈다. 두 프로세스가 동시에 refresh 하면
한쪽이 재사용으로 판정돼 **grant 전체가 revoke 되고 사용자가 이유 없이
로그아웃당한다.** 모든 refresh 는 `config.LockPath()` 파일 락 안에서 직렬화한다.
이걸 빠뜨리면 "가끔 로그아웃됨" 이라는 재현 안 되는 버그로 돌아온다.

## 커맨드 추가

상위 커맨드는 손으로 다듬지만, 그 외는 OpenAPI spec 에서 생성하는 것이 목표다.
소스는 `clawops/app/src/swagger/openapi.bundled.json` (operationId 112, 태그 15).
요청 필드는 Twilio 호환 **PascalCase** 다 — `To`, `From`, `Body`, `Url`, `AgentId`.

페이지네이션 파라미터는 라우트마다 다르다 (`pageSize` / `page_size` / `limit` / `page`).
공통 파서로 통일하지 말고 각 라우트의 계약을 그대로 따른다.

## 검증

```bash
make lint && make test && ./scripts/check-no-real-data.sh
```
