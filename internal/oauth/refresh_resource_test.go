package oauth

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/learners-superpumped/clawops-cli/internal/config"
)

// refresh 요청에도 resource 를 실어야 한다 (RFC 8707).
//
// 빠뜨리면 서버가 resourceServer 없이 토큰을 발급한다 — oidc-provider 의
// resolve_resource.js 에서 모든 분기가 떨어져 resource 가 undefined 가 되고,
// refresh_token.js 는 opaque access token 을 내면서 scope 를 OIDC 표준 세 개로
// 깎는다(getOIDCScopeFiltered).
//
// 증상이 지연된다는 게 고약한 점이다. 로그인 직후에는 멀쩡하고, 한 시간 뒤
// 첫 자동 갱신에서 조용히 자격증명이 망가져 이후 모든 API 가 401 이 된다.
func TestRefreshSendsResourceIndicator(t *testing.T) {
	isolate(t)
	ri := newRotatingIssuer(t, "rt-initial")

	prof := &config.Profile{Name: "default", Issuer: ri.URL}
	if err := config.SaveToken(prof.Name, &config.Token{
		AccessToken:  "expired",
		RefreshToken: "rt-initial",
		Expiry:       time.Now().Add(-time.Hour),
		Scopes:       []string{"openid", "profile", "email", "read:calls", "read:messages"},
	}); err != nil {
		t.Fatal(err)
	}

	ts := &TokenSource{Profile: prof}
	if _, err := ts.Token(context.Background()); err != nil {
		t.Fatalf("갱신 실패: %v", err)
	}

	if ri.lastResource != rotatingResource {
		t.Errorf("refresh 요청의 resource = %q, want %q", ri.lastResource, rotatingResource)
	}

	saved, err := config.LoadToken(prof.Name)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(saved.AccessToken, ".") != 2 {
		t.Error("갱신된 access token 이 JWT 가 아니다 — resource 를 빠뜨리면 opaque 가 된다")
	}
	if saved.AccountID == "" {
		t.Error("갱신 후 account_id 가 사라졌다")
	}
	for _, want := range []string{"read:calls", "read:messages"} {
		if !slices.Contains(saved.Scopes, want) {
			t.Errorf("갱신 후 %q 스코프가 깎였다: %v", want, saved.Scopes)
		}
	}
}
