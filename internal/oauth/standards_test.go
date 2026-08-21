package oauth

// 표준 준수 검증 — 어겼을 때 증상이 한참 뒤에야 드러나는 것들만 모았다.
// (OIDC Core §11 offline_access, RFC 8707 resource, RFC 9207 iss, Discovery §4.3)

import (
	"context"
	"strings"
	"testing"
	"time"
)

// OIDC Core §11: offline_access 는 prompt 에 consent 가 없으면 AS 가 조용히 버린다.
// 빠뜨리면 refresh token 이 영원히 안 나오고, 증상은 access token 이 만료되는
// 한 시간 뒤에야 "왜 또 로그인하지" 로 드러난다.
func TestLoginRequestsConsentPromptForOfflineAccess(t *testing.T) {
	f := newFakeIssuer(t)
	withBrowser(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := Login(ctx, LoginOptions{Issuer: f.URL, ClientID: "clawops-cli"}); err != nil {
		t.Fatalf("Login 실패: %v", err)
	}
	if !strings.Contains(f.gotScope, "offline_access") {
		t.Fatalf("offline_access 를 요청하지 않았다: %q", f.gotScope)
	}
	if f.gotPrompt != "consent" {
		t.Errorf("prompt = %q, offline_access 를 요청하면 consent 여야 한다", f.gotPrompt)
	}
}

// RFC 8707: resource 를 지정해야 서버가 JWT access token 을 발급한다.
// authorize 와 token 양쪽에 같은 값이 실려야 발급 대상이 어긋나지 않는다(§2.2).
func TestLoginSendsResourceIndicator(t *testing.T) {
	f := newFakeIssuer(t)
	withBrowser(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := Login(ctx, LoginOptions{Issuer: f.URL, ClientID: "clawops-cli"}); err != nil {
		t.Fatalf("Login 실패: %v", err)
	}
	if f.gotResource != f.protectedResource {
		t.Errorf("authorize 의 resource = %q, want %q", f.gotResource, f.protectedResource)
	}
	if f.gotTokenResource != f.protectedResource {
		t.Errorf("token 의 resource = %q, want %q", f.gotTokenResource, f.protectedResource)
	}
}

// protected resource metadata 는 선택이다. 없어도 로그인이 막히면 안 된다.
func TestLoginWorksWithoutProtectedResourceMetadata(t *testing.T) {
	f := newFakeIssuer(t)
	f.protectedResource = ""
	withBrowser(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := Login(ctx, LoginOptions{Issuer: f.URL, ClientID: "clawops-cli"}); err != nil {
		t.Fatalf("metadata 부재로 로그인이 막혔다: %v", err)
	}
	if f.gotResource != "" {
		t.Errorf("resource 를 보내지 말았어야 한다: %q", f.gotResource)
	}
}

// RFC 9207: iss 가 우리가 요청을 보낸 AS 와 같아야 한다.
func TestLoginAcceptsMatchingIss(t *testing.T) {
	f := newFakeIssuer(t)
	f.issSupported = true
	withBrowser(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := Login(ctx, LoginOptions{Issuer: f.URL, ClientID: "clawops-cli"}); err != nil {
		t.Fatalf("iss 가 일치하는데 실패했다: %v", err)
	}
}

// mix-up 공격: 다른 AS 의 것으로 위조된 iss 는 거절해야 한다.
func TestLoginRejectsMismatchedIss(t *testing.T) {
	f := newFakeIssuer(t)
	f.issSupported = true
	f.issInResponse = "https://attacker.example.test"
	withBrowser(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := Login(ctx, LoginOptions{Issuer: f.URL, ClientID: "clawops-cli"})
	if err == nil {
		t.Fatal("iss 불일치인데 성공했다")
	}
	if !strings.Contains(err.Error(), "iss") {
		t.Errorf("에러가 iss 를 가리키지 않는다: %v", err)
	}
}

// 지원한다고 광고해 놓고 iss 를 빼먹는 것도 이상 신호다.
func TestLoginRejectsMissingIssWhenAdvertised(t *testing.T) {
	f := newFakeIssuer(t)
	f.issSupported = true
	f.issInResponse = "-"
	withBrowser(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := Login(ctx, LoginOptions{Issuer: f.URL, ClientID: "clawops-cli"}); err == nil {
		t.Fatal("iss 누락인데 성공했다")
	}
}

// OIDC Discovery §4.3 / RFC 8414 §3.3: 문서의 issuer 는 조회에 쓴 URL 과 같아야 한다.
// 다르면 RFC 9207 검증의 기준값을 신뢰할 수 없다.
func TestDiscoverRejectsIssuerMismatch(t *testing.T) {
	f := newFakeIssuer(t)
	f.overrideIssuer = "https://elsewhere.example.test"

	_, err := Discover(context.Background(), f.URL)
	if err == nil {
		t.Fatal("issuer 불일치를 받아들였다")
	}
	if !strings.Contains(err.Error(), "issuer") {
		t.Errorf("에러가 issuer 를 가리키지 않는다: %v", err)
	}
}
