package oauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// fakeIssuer 는 node-oidc-provider 의 CLI 관련 동작만 흉내내는 테스트 서버다.
// PKCE S256 검증, state 반환, authorization_code 교환을 실제로 수행한다.
type fakeIssuer struct {
	*httptest.Server

	// 기록용 — 테스트가 검증한다.
	gotChallenge   string
	gotMethod      string
	gotScope       string
	gotRedirectURI string
	gotClientID    string

	// authorize 응답을 조작한다.
	overrideState string
	denyWith      string
}

func newFakeIssuer(t *testing.T) *fakeIssuer {
	t.Helper()
	f := &fakeIssuer{}
	mux := http.NewServeMux()

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"issuer":                 f.URL,
			"authorization_endpoint": f.URL + "/authorize",
			"token_endpoint":         f.URL + "/token",
			"revocation_endpoint":    f.URL + "/revoke",
			"scopes_supported": []string{
				"openid", "profile", "email", "offline_access",
				"read:calls", "read:messages", "write:calls", "write:messages",
			},
		})
	})

	mux.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		f.gotChallenge = q.Get("code_challenge")
		f.gotMethod = q.Get("code_challenge_method")
		f.gotScope = q.Get("scope")
		f.gotRedirectURI = q.Get("redirect_uri")
		f.gotClientID = q.Get("client_id")

		state := q.Get("state")
		if f.overrideState != "" {
			state = f.overrideState
		}

		back, _ := url.Parse(q.Get("redirect_uri"))
		rq := back.Query()
		if f.denyWith != "" {
			rq.Set("error", f.denyWith)
			rq.Set("error_description", "테스트에서 거절함")
		} else {
			rq.Set("code", "test-auth-code")
		}
		rq.Set("state", state)
		back.RawQuery = rq.Encode()
		http.Redirect(w, r, back.String(), http.StatusFound)
	})

	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()

		// PKCE 검증 — verifier 를 S256 한 값이 authorize 때 받은 challenge 와 같아야 한다.
		sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
		want := base64.RawURLEncoding.EncodeToString(sum[:])
		if want != f.gotChallenge {
			http.Error(w, "PKCE 불일치", http.StatusBadRequest)
			return
		}
		if r.Form.Get("grant_type") == "authorization_code" && r.Form.Get("code") != "test-auth-code" {
			http.Error(w, "잘못된 코드", http.StatusBadRequest)
			return
		}

		writeJSON(w, map[string]any{
			"access_token":  makeJWT(map[string]any{"account_id": "AC00000000000000000000000000000000", "email": "dev@example.com"}),
			"refresh_token": "test-refresh-token",
			"token_type":    "Bearer",
			"expires_in":    3600,
			"scope":         f.gotScope,
		})
	})

	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Close)
	return f
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// makeJWT 는 서명 없는 JWT 모양의 문자열을 만든다.
// parseClaims 가 서명을 검증하지 않으므로(표시 전용) 테스트에 충분하다.
func makeJWT(claims map[string]any) string {
	b, _ := json.Marshal(claims)
	return "e30." + base64.RawURLEncoding.EncodeToString(b) + ".sig"
}

// withBrowser 는 브라우저 대신 HTTP 클라이언트가 authorize URL 을 따라가게 한다.
func withBrowser(t *testing.T) {
	t.Helper()
	orig := openBrowser
	openBrowser = func(rawURL string) error {
		go func() {
			// 리다이렉트를 따라가면 loopback 콜백까지 도달한다.
			resp, err := http.Get(rawURL)
			if err == nil {
				resp.Body.Close()
			}
		}()
		return nil
	}
	t.Cleanup(func() { openBrowser = orig })
}

func TestLogin(t *testing.T) {
	f := newFakeIssuer(t)
	withBrowser(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tok, err := Login(ctx, LoginOptions{
		Issuer:   f.URL,
		ClientID: "clawops-cli",
	})
	if err != nil {
		t.Fatalf("Login 실패: %v", err)
	}

	if tok.RefreshToken != "test-refresh-token" {
		t.Errorf("refresh token = %q", tok.RefreshToken)
	}
	if tok.AccountID != "AC00000000000000000000000000000000" {
		t.Errorf("account_id claim 을 못 읽었다: %q", tok.AccountID)
	}
	if tok.Email != "dev@example.com" {
		t.Errorf("email claim = %q", tok.Email)
	}
	if tok.Expiry.IsZero() || tok.Expiry.Before(time.Now()) {
		t.Errorf("만료 시각이 이상하다: %v", tok.Expiry)
	}

	// PKCE 는 S256 이어야 한다. plain 으로 떨어지면 안 된다.
	if f.gotMethod != "S256" {
		t.Errorf("code_challenge_method = %q, S256 이어야 한다", f.gotMethod)
	}
	if f.gotChallenge == "" {
		t.Error("code_challenge 가 비어 있다")
	}
	if f.gotClientID != "clawops-cli" {
		t.Errorf("client_id = %q", f.gotClientID)
	}

	// redirect_uri 는 loopback 이어야 하고 포트가 붙어 있어야 한다.
	if !strings.HasPrefix(f.gotRedirectURI, "http://127.0.0.1:") ||
		!strings.HasSuffix(f.gotRedirectURI, "/cb") {
		t.Errorf("redirect_uri = %q", f.gotRedirectURI)
	}

	// 기본 스코프: offline_access 있어야 refresh token 이 나오고, write 는 없어야 한다.
	if !strings.Contains(f.gotScope, "offline_access") {
		t.Errorf("offline_access 가 빠졌다: %q", f.gotScope)
	}
	if strings.Contains(f.gotScope, "write:") {
		t.Errorf("기본 로그인에 write 스코프가 들어갔다: %q", f.gotScope)
	}
	if !strings.Contains(f.gotScope, "read:calls") {
		t.Errorf("discovery 의 read 스코프가 반영되지 않았다: %q", f.gotScope)
	}
}

func TestLoginExtraScopes(t *testing.T) {
	f := newFakeIssuer(t)
	withBrowser(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := Login(ctx, LoginOptions{
		Issuer:      f.URL,
		ClientID:    "clawops-cli",
		ExtraScopes: []string{"write:messages"},
	}); err != nil {
		t.Fatalf("Login 실패: %v", err)
	}
	if !strings.Contains(f.gotScope, "write:messages") {
		t.Errorf("승격한 스코프가 빠졌다: %q", f.gotScope)
	}
}

// state 가 어긋나면 인가 코드를 써서는 안 된다 (CSRF).
func TestLoginRejectsBadState(t *testing.T) {
	f := newFakeIssuer(t)
	f.overrideState = "attacker-state"
	withBrowser(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := Login(ctx, LoginOptions{Issuer: f.URL, ClientID: "clawops-cli"})
	if err == nil {
		t.Fatal("state 불일치인데 성공했다")
	}
	if !strings.Contains(err.Error(), "state") {
		t.Errorf("에러가 state 를 가리키지 않는다: %v", err)
	}
}

// 사용자가 승인을 거절하면 그 사실이 그대로 전달돼야 한다.
func TestLoginPropagatesDenial(t *testing.T) {
	f := newFakeIssuer(t)
	f.denyWith = "access_denied"
	withBrowser(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := Login(ctx, LoginOptions{Issuer: f.URL, ClientID: "clawops-cli"})
	if err == nil {
		t.Fatal("거절인데 성공했다")
	}
	if !strings.Contains(err.Error(), "access_denied") {
		t.Errorf("거절 사유가 전달되지 않았다: %v", err)
	}
}

func TestDiscoverRejectsIncomplete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"issuer": "https://example.com"})
	}))
	defer srv.Close()

	if _, err := Discover(context.Background(), srv.URL); err == nil {
		t.Fatal("엔드포인트가 없는 문서를 받아들였다")
	}
}
