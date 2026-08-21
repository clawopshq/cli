package oauth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"golang.org/x/oauth2"

	"github.com/learners-superpumped/clawops-cli/internal/config"
)

// loginTimeout 은 브라우저 승인을 기다리는 최대 시간이다.
const loginTimeout = 3 * time.Minute

// LoginOptions 는 Login 의 입력이다.
type LoginOptions struct {
	Issuer   string
	ClientID string

	// Scopes 를 지정하면 그대로 쓴다. 비어 있으면 DefaultScopes 로 계산한다.
	Scopes []string
	// ExtraScopes 는 기본 스코프에 더할 것들이다 (예: write:messages).
	ExtraScopes []string

	// NoBrowser 가 true 면 브라우저를 열지 않고 URL 만 알린다.
	NoBrowser bool

	// Notify 는 사용자에게 보여줄 안내를 받는다 (stderr 로 나간다).
	Notify func(format string, args ...any)
}

// Login 은 Authorization Code + PKCE(S256) 흐름을 loopback 리다이렉트로 수행한다.
//
// 서버가 http://127.0.0.1:*/cb 를 wildcard 로 허용하므로(RFC 8252 §7.3) 포트를
// 미리 정할 필요가 없다 — 커널이 준 포트를 그대로 redirect_uri 에 넣는다.
func Login(ctx context.Context, opts LoginOptions) (*config.Token, error) {
	notify := opts.Notify
	if notify == nil {
		notify = func(string, ...any) {}
	}

	meta, err := Discover(ctx, opts.Issuer)
	if err != nil {
		return nil, err
	}

	scopes := opts.Scopes
	if len(scopes) == 0 {
		scopes = DefaultScopes(meta, opts.ExtraScopes)
	}

	// 커널이 빈 포트를 고르게 한다. 고정 포트를 쓰면 다른 프로세스와 충돌한다.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("로컬 콜백 서버를 열 수 없습니다: %w", err)
	}
	defer ln.Close()
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/cb", ln.Addr().(*net.TCPAddr).Port)

	conf := &oauth2.Config{
		ClientID: opts.ClientID,
		Endpoint: oauth2.Endpoint{
			AuthURL:   meta.AuthorizationEndpoint,
			TokenURL:  meta.TokenEndpoint,
			AuthStyle: oauth2.AuthStyleInParams, // public client — secret 이 없다
		},
		RedirectURL: redirectURI,
		Scopes:      scopes,
	}

	verifier := oauth2.GenerateVerifier()
	state, err := randomState()
	if err != nil {
		return nil, err
	}

	authOpts := []oauth2.AuthCodeOption{
		oauth2.S256ChallengeOption(verifier),
		// OIDC Core §11: offline_access 는 prompt 에 consent 가 없으면 AS 가
		// 조용히 무시한다 (oidc-provider check_scope.js). 이걸 빠뜨리면 refresh
		// token 이 영원히 발급되지 않아 access token 만료마다 재로그인해야 한다.
		oauth2.SetAuthURLParam("prompt", "consent"),
	}
	// RFC 8707 resource indicator. 지정해야 서버가 JWT access token 을 발급한다
	// (없으면 opaque 라 account_id claim 을 읽을 수 없다). metadata 조회가
	// 실패해도 로그인 자체는 막지 않는다 — 표시 정보를 잃을 뿐이다.
	var exchangeOpts []oauth2.AuthCodeOption
	var resource string
	if pr, err := DiscoverProtectedResource(ctx, meta.Issuer); err == nil {
		resource = pr.Resource
		resourceOpt := oauth2.SetAuthURLParam("resource", resource)
		authOpts = append(authOpts, resourceOpt)
		// 토큰 교환에도 같은 resource 를 실어야 발급 대상이 authorization 단계와
		// 어긋나지 않는다 (RFC 8707 §2.2).
		exchangeOpts = append(exchangeOpts, resourceOpt)
	} else {
		// resource 없이 받은 토큰은 opaque 이고 scope 가 OIDC 표준 세 개로 깎여
		// API 호출에 쓸 수 없다. 로그인을 막지는 않되 조용히 넘기지도 않는다.
		notify("경고: resource metadata 를 읽지 못했습니다 (%v).\n"+
			"  이 세션의 토큰으로는 API 호출이 거부될 수 있습니다.", err)
	}
	authURL := conf.AuthCodeURL(state, authOpts...)

	// 콜백 대기 시작 — 브라우저를 열기 전에 서버가 떠 있어야 한다.
	ctx, cancel := context.WithTimeout(ctx, loginTimeout)
	defer cancel()
	results := serveCallback(ctx, ln, state, meta)

	if opts.NoBrowser {
		notify("아래 주소를 브라우저에서 여세요:\n\n  %s\n", authURL)
	} else if err := openBrowser(authURL); err != nil {
		notify("브라우저를 열지 못했습니다. 아래 주소를 직접 여세요:\n\n  %s\n", authURL)
	} else {
		notify("브라우저에서 로그인을 완료하세요...")
	}

	select {
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("로그인 대기 시간이 지났습니다 (%s)", loginTimeout)
		}
		return nil, ctx.Err()
	case r := <-results:
		if r.err != nil {
			return nil, r.err
		}
		tok, err := conf.Exchange(ctx, r.code,
			append(exchangeOpts, oauth2.VerifierOption(verifier))...)
		if err != nil {
			return nil, fmt.Errorf("토큰 교환에 실패했습니다: %w", err)
		}
		stored := toStoredToken(tok)
		stored.Resource = resource
		if len(stored.Scopes) == 0 {
			stored.Scopes = scopes
		}
		return stored, nil
	}
}

type callbackResult struct {
	code string
	err  error
}

// serveCallback 은 redirect 를 한 번 받고 종료하는 로컬 HTTP 서버다.
func serveCallback(
	ctx context.Context,
	ln net.Listener,
	wantState string,
	meta *Metadata,
) <-chan callbackResult {
	out := make(chan callbackResult, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/cb", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()

		// RFC 9207: AS 가 iss 를 보내면 클라이언트는 그것이 우리가 요청을 보낸
		// AS 인지 확인해야 한다. 확인하지 않으면 여러 AS 를 쓰는 클라이언트가
		// mix-up 공격으로 인가 코드를 엉뚱한 AS 의 토큰 엔드포인트에 넘길 수 있다.
		// 서버가 지원한다고 광고했는데 값이 없으면 그것도 이상 신호로 본다.
		if gotIss := q.Get("iss"); gotIss != "" {
			if gotIss != meta.Issuer {
				writeResultPage(w, false, "발급자가 일치하지 않습니다", "요청이 변조되었을 수 있습니다.")
				out <- callbackResult{err: fmt.Errorf(
					"iss 가 일치하지 않습니다 (RFC 9207): 기대 %q, 수신 %q", meta.Issuer, gotIss)}
				return
			}
		} else if meta.IssParameterSupported {
			writeResultPage(w, false, "발급자 정보가 없습니다", "다시 시도해 주세요.")
			out <- callbackResult{err: errors.New(
				"AS 가 iss 파라미터를 지원한다고 광고했지만 응답에 없습니다 (RFC 9207)")}
			return
		}

		// /authorize 가 거절되면 error 파라미터로 되돌아온다 (RFC 6749 §4.1.2.1).
		if e := q.Get("error"); e != "" {
			desc := q.Get("error_description")
			msg := e
			if desc != "" {
				msg = fmt.Sprintf("%s — %s", e, desc)
			}
			writeResultPage(w, false, "로그인이 취소되었습니다", msg)
			out <- callbackResult{err: fmt.Errorf("인가가 거절되었습니다: %s", msg)}
			return
		}

		// state 는 CSRF 방어다. 어긋나면 코드를 쓰지 않고 버린다.
		if q.Get("state") != wantState {
			writeResultPage(w, false, "요청이 일치하지 않습니다", "다시 시도해 주세요.")
			out <- callbackResult{err: errors.New("state 가 일치하지 않습니다 (요청이 변조되었을 수 있습니다)")}
			return
		}

		code := q.Get("code")
		if code == "" {
			writeResultPage(w, false, "인가 코드가 없습니다", "다시 시도해 주세요.")
			out <- callbackResult{err: errors.New("인가 코드를 받지 못했습니다")}
			return
		}

		writeResultPage(w, true, "로그인이 완료되었습니다", "이 창을 닫고 터미널로 돌아가세요.")
		out <- callbackResult{code: code}
	})

	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() { _ = srv.Serve(ln) }()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	return out
}

func randomState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("난수 생성에 실패했습니다: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// openBrowser 는 테스트에서 교체할 수 있도록 변수로 둔다.
var openBrowser = defaultOpenBrowser

func defaultOpenBrowser(rawURL string) error {
	// 파싱되지 않는 URL 을 셸로 넘기지 않는다.
	if _, err := url.Parse(rawURL); err != nil {
		return err
	}
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", rawURL).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", rawURL).Start()
	default:
		if _, err := exec.LookPath("xdg-open"); err != nil {
			return errors.New("xdg-open 을 찾을 수 없습니다")
		}
		return exec.Command("xdg-open", rawURL).Start()
	}
}

// toStoredToken 은 oauth2.Token 을 저장 형식으로 바꾸고 claim 을 표시용으로 뽑는다.
func toStoredToken(t *oauth2.Token) *config.Token {
	out := &config.Token{
		AccessToken:  t.AccessToken,
		RefreshToken: t.RefreshToken,
		Expiry:       t.Expiry,
	}
	if s, ok := t.Extra("scope").(string); ok && s != "" {
		out.Scopes = strings.Fields(s)
	}
	if c, err := parseClaims(t.AccessToken); err == nil {
		out.AccountID = c.AccountID
		out.Email = c.Email
	}
	return out
}
