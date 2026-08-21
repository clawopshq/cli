package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gofrs/flock"
	"golang.org/x/oauth2"

	"github.com/learners-superpumped/clawops-cli/internal/config"
)

// refreshLockTimeout 은 다른 프로세스의 refresh 를 기다리는 최대 시간이다.
const refreshLockTimeout = 20 * time.Second

// TokenSource 는 요청마다 유효한 Bearer 값을 공급한다.
//
// API 키가 있으면 그대로 쓰고, 없으면 저장된 OAuth 토큰을 쓰되 만료됐으면
// 갱신한다. 호출부는 둘을 구분하지 않는다.
type TokenSource struct {
	Profile *config.Profile
	// Notify 는 갱신 같은 배경 동작을 알린다 (선택).
	Notify func(format string, args ...any)
}

// Token 은 유효한 access token(또는 API 키)을 돌려준다.
func (ts *TokenSource) Token(ctx context.Context) (string, error) {
	if ts.Profile.APIKey != "" {
		return ts.Profile.APIKey, nil
	}

	tok, err := config.LoadToken(ts.Profile.Name)
	if errors.Is(err, config.ErrNoToken) {
		return "", config.ErrNotAuthenticated
	}
	if err != nil {
		return "", err
	}
	if !tok.Expired() {
		return tok.AccessToken, nil
	}
	if tok.RefreshToken == "" {
		return "", fmt.Errorf("세션이 만료되었습니다. `clawops auth login` 을 다시 실행하세요")
	}

	refreshed, err := ts.refreshLocked(ctx, tok)
	if err != nil {
		return "", err
	}
	return refreshed.AccessToken, nil
}

// refreshLocked 는 파일 락 안에서 refresh 를 수행한다.
//
// 서버가 rotateRefreshToken=true 로 돈다. 두 프로세스가 같은 refresh token 으로
// 동시에 갱신하면 뒤늦은 쪽이 "재사용" 으로 판정돼 grant 전체가 revoke 되고,
// 사용자는 아무 짓도 안 했는데 로그아웃당한다.
//
// 락만으로는 부족하다 — 락을 잡은 뒤 저장소를 다시 읽어야 한다. 기다리는 동안
// 다른 프로세스가 이미 갱신해 두었다면 우리가 들고 있는 refresh token 은 그
// 시점에 이미 무효다.
func (ts *TokenSource) refreshLocked(ctx context.Context, stale *config.Token) (*config.Token, error) {
	lockPath, err := config.LockPath(ts.Profile.Name)
	if err != nil {
		return nil, err
	}
	lock := flock.New(lockPath)

	lockCtx, cancel := context.WithTimeout(ctx, refreshLockTimeout)
	defer cancel()
	locked, err := lock.TryLockContext(lockCtx, 100*time.Millisecond)
	if err != nil || !locked {
		return nil, fmt.Errorf("토큰 갱신 락을 얻지 못했습니다 (%s): 다른 clawops 프로세스가 갱신 중일 수 있습니다", lockPath)
	}
	defer func() { _ = lock.Unlock() }()

	// 락을 기다리는 동안 다른 프로세스가 끝냈는지 확인한다.
	if fresh, err := config.LoadToken(ts.Profile.Name); err == nil && !fresh.Expired() {
		return fresh, nil
	} else if err == nil && fresh.RefreshToken != stale.RefreshToken {
		// 갱신은 됐지만 그 사이 또 만료된 경우 — 최신 refresh token 으로 진행한다.
		stale = fresh
	}

	meta, err := Discover(ctx, ts.Profile.Issuer)
	if err != nil {
		return nil, err
	}

	// RFC 8707: refresh 에도 resource 를 실어야 한다. 빠뜨리면 서버가
	// resourceServer 없이 발급해 access token 이 opaque 로 바뀌고 scope 가 OIDC
	// 표준 세 개로 깎인다 — 로그인은 멀쩡했는데 첫 갱신에서 자격증명이 조용히
	// 망가지고 이후 모든 API 가 401 이 된다.
	// 보통은 로그인 때 저장해 둔 값을 쓴다. 비어 있는 경우는 resource 를 보내기
	// 전 빌드로 로그인한 토큰뿐이라, 그런 세션이 첫 갱신에서 망가지지 않도록
	// 여기서 한 번 채워 준다.
	resource := stale.Resource
	if resource == "" {
		if pr, err := DiscoverProtectedResource(ctx, meta.Issuer); err == nil {
			resource = pr.Resource
		}
	}

	if ts.Notify != nil {
		ts.Notify("세션을 갱신하는 중...")
	}
	newTok, err := refreshGrant(ctx, meta.TokenEndpoint, stale.RefreshToken, resource)
	if err != nil {
		return nil, fmt.Errorf("세션 갱신에 실패했습니다. `clawops auth login` 을 다시 실행하세요: %w", err)
	}

	out := toStoredToken(newTok)
	out.Resource = resource
	if out.RefreshToken == "" {
		// 서버가 새 refresh token 을 주지 않았으면 기존 것을 유지한다.
		out.RefreshToken = stale.RefreshToken
	}
	if err := config.SaveToken(ts.Profile.Name, out); err != nil {
		return nil, fmt.Errorf("갱신한 토큰을 저장하지 못했습니다: %w", err)
	}
	return out, nil
}

// refreshGrant 는 refresh_token grant 를 직접 수행한다.
//
// oauth2.Config.TokenSource 를 쓰지 않는 이유: 그 경로는 요청에 추가 파라미터를
// 실을 수 없어 resource 를 보낼 방법이 없다 (Exchange 와 달리 옵션을 받지 않는다).
func refreshGrant(ctx context.Context, tokenURL, refreshToken, resource string) (*oauth2.Token, error) {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	form.Set("client_id", config.CLIClientID) // public client — secret 없음
	if resource != "" {
		form.Set("resource", resource)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var r struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int64  `json:"expires_in"`
		Scope        string `json:"scope"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("토큰 응답을 읽을 수 없습니다: %w", err)
	}
	if r.AccessToken == "" {
		return nil, errors.New("토큰 응답에 access_token 이 없습니다")
	}

	tok := &oauth2.Token{
		AccessToken:  r.AccessToken,
		RefreshToken: r.RefreshToken,
		TokenType:    r.TokenType,
	}
	if r.ExpiresIn > 0 {
		tok.Expiry = time.Now().Add(time.Duration(r.ExpiresIn) * time.Second)
	}
	// toStoredToken 이 응답의 scope 를 Extra 로 읽는다.
	return tok.WithExtra(map[string]any{"scope": r.Scope}), nil
}

// Revoke 는 서버의 grant 를 폐기한다 (RFC 7009).
//
// refresh token 을 폐기하면 서버가 grant 를 통째로 무효화하므로 access token 도
// 함께 죽는다. 엔드포인트가 없거나 실패해도 로컬 삭제는 계속 진행해야 한다 —
// 로그아웃이 서버 상태 때문에 막히면 안 된다.
func Revoke(ctx context.Context, issuer, token, hint string) error {
	meta, err := Discover(ctx, issuer)
	if err != nil {
		return err
	}
	if meta.RevocationEndpoint == "" {
		return errors.New("서버에 revocation 엔드포인트가 없습니다")
	}

	form := url.Values{}
	form.Set("token", token)
	form.Set("client_id", config.CLIClientID)
	if hint != "" {
		form.Set("token_type_hint", hint)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		meta.RevocationEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// RFC 7009 §2.2: 이미 무효인 토큰도 200 이다. 400 대는 진짜 실패.
	if resp.StatusCode >= 400 {
		return fmt.Errorf("revocation 실패: HTTP %d", resp.StatusCode)
	}
	return nil
}
