package oauth

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
)

// Claims 는 access token 에서 표시용으로 꺼내 쓰는 값들이다.
type Claims struct {
	Subject   string   `json:"sub"`
	AccountID string   `json:"account_id"`
	Email     string   `json:"email"`
	Scope     string   `json:"scope"`
	Audience  []string `json:"-"`
}

// parseClaims 는 JWT 페이로드를 디코드한다.
//
// 서명을 검증하지 않는다. 이 값은 오직 사람에게 보여주기 위한 것이고, 권한
// 판단은 전부 서버가 한다 — CLI 가 토큰을 해석해 무언가를 허용/차단하면
// 서버와 다른 답을 내기 시작한다.
func parseClaims(accessToken string) (*Claims, error) {
	parts := strings.Split(accessToken, ".")
	if len(parts) != 3 {
		return nil, errors.New("JWT 형식이 아닙니다")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, err
	}
	var c Claims
	if err := json.Unmarshal(payload, &c); err != nil {
		return nil, err
	}
	return &c, nil
}
