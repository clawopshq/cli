package oauth

import "strings"

// 표준 OIDC 스코프. offline_access 가 있어야 refresh token 이 나온다.
var baseScopes = []string{"openid", "profile", "email", "offline_access"}

// DefaultScopes 는 첫 로그인에서 요청할 스코프를 정한다.
//
// read:* 는 discovery 의 scopes_supported 에서 뽑는다 — 서버가 리소스를 추가해도
// CLI 를 다시 배포하지 않아도 된다.
//
// write:* 는 일부러 넣지 않는다. write:calls / write:messages 는 실제로 요금이
// 발생하는 권한이라, 사용자가 `clawops auth refresh -s write:messages` 로 한 번
// 의식하고 승인하게 한다.
func DefaultScopes(meta *Metadata, extra []string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			return
		}
		seen[s] = true
		out = append(out, s)
	}

	for _, s := range baseScopes {
		add(s)
	}
	for _, s := range meta.ScopesSupported {
		if strings.HasPrefix(s, "read:") {
			add(s)
		}
	}
	for _, s := range extra {
		add(s)
	}
	return out
}
