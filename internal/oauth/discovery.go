// Package oauth 는 CLI 의 OIDC 인증 흐름을 구현한다.
//
// 서버는 node-oidc-provider 로 돌고, CLI 가 필요로 하는 것은 전부 이미 켜져 있다:
// PKCE S256 강제, loopback wildcard redirect(RFC 8252 §7.3), discovery, revocation.
// 따라서 여기서 하는 일은 표준 Authorization Code + PKCE 흐름을 그대로 타는 것뿐이다.
package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// httpClient 는 이 패키지의 모든 서버 호출이 공유한다. Transport 를 지정하지
// 않았으므로 http.DefaultTransport 의 커넥션 풀을 그대로 쓴다 — 타임아웃 값이
// 호출마다 제각각이 되는 것만 막는 용도다.
var httpClient = &http.Client{Timeout: 30 * time.Second}

// Metadata 는 OIDC discovery 문서에서 우리가 쓰는 필드들이다.
type Metadata struct {
	Issuer                string   `json:"issuer"`
	AuthorizationEndpoint string   `json:"authorization_endpoint"`
	TokenEndpoint         string   `json:"token_endpoint"`
	RevocationEndpoint    string   `json:"revocation_endpoint"`
	UserinfoEndpoint      string   `json:"userinfo_endpoint"`
	ScopesSupported       []string `json:"scopes_supported"`

	// RFC 9207. true 면 AS 가 authorization 응답에 iss 를 실어 보내고, 클라이언트는
	// 그것을 검증해야 한다 (mix-up 공격 방어).
	IssParameterSupported bool `json:"authorization_response_iss_parameter_supported"`

	// Device Flow 는 서버에서 아직 켜지 않았다. 비어 있으면 headless 안내로 넘긴다.
	DeviceAuthorizationEndpoint string `json:"device_authorization_endpoint"`
}

// ProtectedResource 는 RFC 9728 protected resource metadata 중 우리가 쓰는 부분이다.
type ProtectedResource struct {
	Resource             string   `json:"resource"`
	AuthorizationServers []string `json:"authorization_servers"`
}

// Discover 는 {issuer}/.well-known/openid-configuration 을 읽는다.
//
// 엔드포인트를 하드코딩하지 않는 이유: dev/staging/prod 가 issuer 하나로 갈리고,
// 서버가 경로를 바꿔도 CLI 를 다시 배포하지 않아도 된다.
func Discover(ctx context.Context, issuer string) (*Metadata, error) {
	base := strings.TrimRight(issuer, "/")
	url := base + "/.well-known/openid-configuration"

	var m Metadata
	if err := fetchJSON(ctx, url, &m); err != nil {
		return nil, err
	}
	if m.AuthorizationEndpoint == "" || m.TokenEndpoint == "" {
		return nil, fmt.Errorf("discovery 문서에 필수 엔드포인트가 없습니다 (%s)", url)
	}

	// OIDC Discovery §4.3 / RFC 8414 §3.3: 돌려받은 issuer 는 조회에 쓴 URL 과
	// 같아야 한다. 다르면 metadata 를 어느 AS 의 것으로 믿어야 할지가 모호해지고,
	// 뒤이은 RFC 9207 iss 검증의 기준값도 신뢰할 수 없다.
	if m.Issuer != base {
		// 문서가 주장하는 issuer 를 "이걸로 다시 시도하라" 고 안내하지 않는다.
		// 이 검사의 전제가 그 문서를 믿을 수 없다는 것인데, 받은 값을 그대로
		// 제안하면 탈취된 호스트가 스스로를 신뢰하게 만드는 문구를 CLI 가
		// 대신 출력해 주는 셈이 된다.
		return nil, fmt.Errorf(
			"issuer 가 일치하지 않습니다 (OIDC Discovery §4.3).\n"+
				"  조회한 곳: %s\n"+
				"  문서가 주장하는 issuer: %s\n"+
				"  올바른 issuer 주소를 확인한 뒤 --issuer 로 지정하세요",
			base, m.Issuer)
	}
	return &m, nil
}

// DiscoverProtectedResource 는 RFC 9728 protected resource metadata 를 읽어
// resource indicator (RFC 8707) 로 쓸 값을 얻는다.
//
// resource 를 지정해야 서버가 JWT access token 을 발급한다. 지정하지 않으면
// opaque 토큰이라 CLI 가 account_id 를 표시할 수 없다.
func DiscoverProtectedResource(ctx context.Context, issuer string) (*ProtectedResource, error) {
	base := strings.TrimRight(issuer, "/")
	url := base + "/.well-known/oauth-protected-resource"

	var pr ProtectedResource
	if err := fetchJSON(ctx, url, &pr); err != nil {
		return nil, err
	}
	if pr.Resource == "" {
		return nil, fmt.Errorf("protected resource metadata 에 resource 가 없습니다 (%s)", url)
	}
	return &pr, nil
}

func fetchJSON(ctx context.Context, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("연결할 수 없습니다 (%s): %w", url, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("조회 실패 (%s): HTTP %d", url, resp.StatusCode)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("응답을 읽을 수 없습니다 (%s): %w", url, err)
	}
	return nil
}
