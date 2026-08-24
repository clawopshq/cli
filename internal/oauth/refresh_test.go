package oauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zalando/go-keyring"

	"github.com/clawopshq/cli/internal/config"
)

// isolate 는 설정·키체인을 테스트 안에 가둔다.
func isolate(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(config.EnvAPIKey, "")
	keyring.MockInit() // 실제 OS 키체인을 건드리지 않는다
}

// rotatingIssuer 는 서버의 rotateRefreshToken=true 동작을 모사한다.
// 이미 쓴 refresh token 을 다시 내밀면 400 을 준다 (재사용 감지).
type rotatingIssuer struct {
	*httptest.Server
	mu           sync.Mutex
	valid        string
	next         int
	attempts     atomic.Int32
	reuses       atomic.Int32
	lastResource string
}

// 실제 배포의 protected resource 에 대응하는 테스트용 값.
const rotatingResource = "https://api.example.test/mcp"

func newRotatingIssuer(t *testing.T, initial string) *rotatingIssuer {
	t.Helper()
	ri := &rotatingIssuer{valid: initial}
	mux := http.NewServeMux()

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"issuer":                 ri.URL,
			"authorization_endpoint": ri.URL + "/authorize",
			"token_endpoint":         ri.URL + "/token",
		})
	})

	mux.HandleFunc("/.well-known/oauth-protected-resource", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"resource":              rotatingResource,
			"authorization_servers": []string{ri.URL},
		})
	})

	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		ri.attempts.Add(1)
		_ = r.ParseForm()
		got := r.Form.Get("refresh_token")

		ri.mu.Lock()
		defer ri.mu.Unlock()
		if got != ri.valid {
			// 실제 서버라면 여기서 grant 전체가 revoke 된다.
			ri.reuses.Add(1)
			http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
			return
		}
		ri.next++
		ri.valid = "rt-rotated-" + string(rune('0'+ri.next))

		// 실제 서버(oidc-provider)를 그대로 흉내낸다. refresh 요청에 resource 가
		// 없으면 resolve_resource.js 의 모든 분기가 떨어져 resource=undefined 가
		// 되고, refresh_token.js 는 resourceServer 없이 opaque 토큰을 발급하면서
		// scope 를 OIDC 표준 것만 남긴다(getOIDCScopeFiltered). 픽스처가 이걸
		// 반영하지 않으면 "상상한 페이로드" 를 검증하게 된다.
		resource := r.Form.Get("resource")
		ri.lastResource = resource
		body := map[string]any{
			"refresh_token": ri.valid,
			"token_type":    "Bearer",
			"expires_in":    3600,
		}
		if resource == rotatingResource {
			body["access_token"] = makeJWT(map[string]any{
				"account_id": "AC00000000000000000000000000000000",
			})
			body["scope"] = "openid profile email read:calls read:messages"
		} else {
			body["access_token"] = "opaque-no-resource-token"
			body["scope"] = "openid profile email"
		}
		writeJSON(w, body)
	})

	ri.Server = httptest.NewServer(mux)
	t.Cleanup(ri.Close)
	return ri
}

// 동시에 여러 프로세스가 만료된 토큰으로 요청해도 refresh 는 한 번만 나가야 한다.
//
// 락이 없으면 둘 다 같은 refresh token 을 서버에 내밀고, 뒤늦은 쪽이 재사용으로
// 판정돼 grant 가 통째로 revoke 된다 — 사용자는 이유 없이 로그아웃당한다.
func TestConcurrentRefreshSerializes(t *testing.T) {
	isolate(t)
	ri := newRotatingIssuer(t, "rt-initial")

	prof := &config.Profile{Name: "default", Issuer: ri.URL}
	if err := config.SaveToken(prof.Name, &config.Token{
		AccessToken:  "expired",
		RefreshToken: "rt-initial",
		Expiry:       time.Now().Add(-time.Hour),
	}); err != nil {
		t.Fatal(err)
	}

	const n = 8
	var wg sync.WaitGroup
	errs := make([]error, n)
	toks := make([]string, n)
	start := make(chan struct{})

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			ts := &TokenSource{Profile: prof}
			toks[i], errs[i] = ts.Token(context.Background())
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d 실패: %v", i, err)
		}
	}
	// 전부 같은 access token 을 받아야 한다 — 각자 따로 갱신했다는 뜻이 아니어야.
	for i := 1; i < n; i++ {
		if toks[i] != toks[0] {
			t.Errorf("goroutine %d 가 다른 토큰을 받았다", i)
		}
	}
	if got := ri.attempts.Load(); got != 1 {
		t.Errorf("refresh 요청이 %d 번 나갔다. 락 안에서 재확인하면 1 번이어야 한다", got)
	}
	if got := ri.reuses.Load(); got != 0 {
		t.Errorf("refresh token 재사용이 %d 번 발생했다 — 실제 서버였다면 grant 가 revoke 된다", got)
	}

	// 회전된 refresh token 이 저장돼야 다음 갱신이 성공한다.
	saved, err := config.LoadToken(prof.Name)
	if err != nil {
		t.Fatal(err)
	}
	if saved.RefreshToken == "rt-initial" {
		t.Error("회전된 refresh token 이 저장되지 않았다")
	}
	if saved.Expired() {
		t.Error("갱신 후에도 만료 상태다")
	}
}

// API 키가 있으면 OAuth 경로를 아예 타지 않는다.
func TestTokenSourcePrefersAPIKey(t *testing.T) {
	isolate(t)
	ri := newRotatingIssuer(t, "rt-initial")

	ts := &TokenSource{Profile: &config.Profile{Name: "default", Issuer: ri.URL, APIKey: "sk_test_dummy"}}
	got, err := ts.Token(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != "sk_test_dummy" {
		t.Errorf("API 키가 아닌 값을 돌려줬다: %q", got)
	}
	if ri.attempts.Load() != 0 {
		t.Error("API 키가 있는데 토큰 엔드포인트를 호출했다")
	}
}

// 자격증명이 없으면 무엇을 해야 하는지 알려주는 에러여야 한다.
func TestTokenSourceUnauthenticated(t *testing.T) {
	isolate(t)
	ts := &TokenSource{Profile: &config.Profile{Name: "default", Issuer: "https://example.invalid"}}
	if _, err := ts.Token(context.Background()); err == nil {
		t.Fatal("인증 없이 토큰을 돌려줬다")
	}
}
