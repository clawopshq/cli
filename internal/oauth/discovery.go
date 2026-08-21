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

// Metadata 는 OIDC discovery 문서에서 우리가 쓰는 필드들이다.
type Metadata struct {
	Issuer                string   `json:"issuer"`
	AuthorizationEndpoint string   `json:"authorization_endpoint"`
	TokenEndpoint         string   `json:"token_endpoint"`
	RevocationEndpoint    string   `json:"revocation_endpoint"`
	UserinfoEndpoint      string   `json:"userinfo_endpoint"`
	ScopesSupported       []string `json:"scopes_supported"`

	// Device Flow 는 서버에서 아직 켜지 않았다. 비어 있으면 headless 안내로 넘긴다.
	DeviceAuthorizationEndpoint string `json:"device_authorization_endpoint"`
}

// Discover 는 {issuer}/.well-known/openid-configuration 을 읽는다.
//
// 엔드포인트를 하드코딩하지 않는 이유: dev/staging/prod 가 issuer 하나로 갈리고,
// 서버가 경로를 바꿔도 CLI 를 다시 배포하지 않아도 된다.
func Discover(ctx context.Context, issuer string) (*Metadata, error) {
	base := strings.TrimRight(issuer, "/")
	url := base + "/.well-known/openid-configuration"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("issuer 에 연결할 수 없습니다 (%s): %w", base, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("discovery 실패 (%s): HTTP %d", url, resp.StatusCode)
	}

	var m Metadata
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, fmt.Errorf("discovery 문서를 읽을 수 없습니다 (%s): %w", url, err)
	}
	if m.AuthorizationEndpoint == "" || m.TokenEndpoint == "" {
		return nil, fmt.Errorf("discovery 문서에 필수 엔드포인트가 없습니다 (%s)", url)
	}
	return &m, nil
}
