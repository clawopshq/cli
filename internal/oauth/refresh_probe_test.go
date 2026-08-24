//go:build refreshprobe

// 실서버 자동 갱신 실측 프로브. 평소 빌드·테스트에서는 빌드 태그로 제외된다.
//
//	go test -tags refreshprobe -run TestRefreshProbe ./internal/oauth/ -v
//
// 확인하는 것은 8/22 에 고친 축이 refresh 경로에서도 살아 있는가다. resource
// (RFC 8707) 를 갱신 요청에 싣지 않으면 로그인 직후엔 멀쩡하다가 첫 자동
// 갱신에서 access token 이 opaque 로 바뀌고 스코프가 3 개로 깎인다.
package oauth

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/learners-superpumped/clawops-cli/internal/config"
)

func kind(tok string) string {
	if strings.Count(tok, ".") == 2 {
		return "JWT"
	}
	return "opaque ⚠️"
}

func TestRefreshProbe(t *testing.T) {
	prof, err := config.Load("")
	if err != nil {
		t.Fatalf("프로필 로드 실패: %v", err)
	}
	if prof.APIKey != "" {
		t.Fatalf("CLAWOPS_API_KEY 가 설정돼 있어 OAuth 경로를 타지 않는다")
	}

	before, err := config.LoadToken(prof.Name)
	if err != nil {
		t.Fatalf("토큰 로드 실패: %v", err)
	}
	t.Logf("[before] 프로필=%s 만료=%s 만료됨=%v 종류=%s 스코프=%d개 계정=%s resource=%q",
		prof.Name, before.Expiry.Format(time.RFC3339), before.Expired(),
		kind(before.AccessToken), len(before.Scopes), before.AccountID, before.Resource)

	if !before.Expired() {
		t.Fatalf("아직 만료 전이라 갱신이 일어나지 않는다 (만료 %s, 60초 여유 적용)",
			before.Expiry.Format(time.RFC3339))
	}

	ts := &TokenSource{Profile: prof, Notify: func(f string, a ...any) { t.Logf("[notify] "+f, a...) }}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	got, err := ts.Token(ctx)
	if err != nil {
		t.Fatalf("❌ 자동 갱신 실패: %v", err)
	}

	after, err := config.LoadToken(prof.Name)
	if err != nil {
		t.Fatalf("갱신 후 토큰 로드 실패: %v", err)
	}
	t.Logf("[after ] 만료=%s 종류=%s 스코프=%d개 계정=%s",
		after.Expiry.Format(time.RFC3339), kind(after.AccessToken),
		len(after.Scopes), after.AccountID)

	if got == before.AccessToken {
		t.Fatalf("❌ access token 이 그대로다 — 갱신이 일어나지 않았다")
	}
	if k := kind(after.AccessToken); k != "JWT" {
		t.Fatalf("❌ 갱신 후 access token 이 %s 다 — refresh 요청에 resource 가 빠졌다 (RFC 8707)", k)
	}
	if len(after.Scopes) != len(before.Scopes) {
		t.Fatalf("❌ 스코프가 %d개 → %d개로 변했다 — resource 누락 시 OIDC 표준 3개로 깎인다",
			len(before.Scopes), len(after.Scopes))
	}
	if after.AccountID != before.AccountID {
		t.Fatalf("❌ 계정이 %q → %q 로 변했다", before.AccountID, after.AccountID)
	}
	if after.RefreshToken == before.RefreshToken {
		t.Logf("⚠️ refresh token 이 회전하지 않았다 (서버는 rotateRefreshToken=true 여야 한다)")
	} else {
		t.Logf("✅ refresh token 회전 확인")
	}
	t.Logf("✅ 자동 갱신 통과 — JWT 유지 · 스코프 %d개 유지 · 계정 %s 유지",
		len(after.Scopes), after.AccountID)
}
