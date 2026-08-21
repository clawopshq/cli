// Package config 는 프로필과 자격증명을 읽고 쓴다.
//
// 저장 위치:
//   - 설정 (프로필, issuer, 기본 발신번호):  ~/.config/clawops/config.toml
//   - 토큰 (access/refresh):                 OS 키체인. 폴백은 credentials.json (0600)
//
// 토큰에는 실제로 요금이 발생하는 발신 권한이 붙어 있다. 평문 파일을 기본값으로
// 두지 않는다 — 키체인이 없을 때만 0600 파일로 내려간다.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

const (
	// EnvAPIKey 가 설정돼 있으면 OAuth 를 통째로 건너뛴다 (CI, 컨테이너).
	EnvAPIKey  = "CLAWOPS_API_KEY"
	EnvProfile = "CLAWOPS_PROFILE"
	EnvIssuer  = "CLAWOPS_ISSUER"

	DefaultProfile = "default"
	DefaultIssuer  = "https://api.claw-ops.com"

	// CLIClientID 는 서버에 시드된 public client. secret 이 없고 PKCE S256 만 쓴다.
	CLIClientID = "clawops-cli"
)

// Profile 은 한 계정에 대한 접속 정보다.
//
// 서버의 OIDC grant 가 (사용자, 계정) 쌍으로 발급되므로 — access token 의
// account_id claim 이 grant 에서 나온다 — 프로필 하나가 계정 하나에 자연스럽게
// 대응한다. CLI 가 계정을 따로 관리하지 않는다.
type Profile struct {
	Name      string
	Issuer    string
	AccountID string // 토큰 claim 에서 채운다. 표시 전용.
	// DefaultFrom 이 있으면 messages/calls 의 --from 을 생략할 수 있다.
	DefaultFrom string

	// APIKey 가 비어 있지 않으면 OAuth 토큰 대신 이 값을 Bearer 로 쓴다.
	APIKey string
}

// Load 는 프로필을 해석한다. name 이 비면 env → default 순으로 찾는다.
func Load(name string) (*Profile, error) {
	if name == "" {
		name = os.Getenv(EnvProfile)
	}
	if name == "" {
		name = DefaultProfile
	}

	p := &Profile{Name: name, Issuer: DefaultIssuer}
	if v := os.Getenv(EnvIssuer); v != "" {
		p.Issuer = v
	}
	if key := os.Getenv(EnvAPIKey); key != "" {
		p.APIKey = key
		return p, nil
	}

	// TODO(scaffold): config.toml 로드 + 키체인에서 토큰 조회.
	return p, nil
}

// Dir 은 설정 디렉토리 경로를 돌려준다 (없으면 만든다).
func Dir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("설정 디렉토리를 찾을 수 없습니다: %w", err)
	}
	dir := filepath.Join(base, "clawops")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("설정 디렉토리를 만들 수 없습니다 (%s): %w", dir, err)
	}
	return dir, nil
}

// LockPath 는 refresh 직렬화용 파일 락 경로다.
//
// 서버가 rotateRefreshToken=true 로 돌기 때문에, 두 프로세스가 동시에 refresh
// 하면 한쪽이 재사용으로 판정돼 grant 전체가 revoke 된다 (= 사용자가 이유 없이
// 로그아웃당한다). 모든 refresh 는 이 락 안에서 직렬화해야 한다.
func LockPath(profile string) (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, fmt.Sprintf("refresh-%s.lock", profile)), nil
}

// ErrNotAuthenticated 는 토큰도 API 키도 없을 때 반환한다.
var ErrNotAuthenticated = errors.New(
	"인증되지 않았습니다. `clawops auth login` 을 실행하거나 CLAWOPS_API_KEY 를 설정하세요")

// keychainAvailable 는 OS 키체인을 쓸 수 있는지 알려준다.
func keychainAvailable() bool {
	switch runtime.GOOS {
	case "darwin", "windows":
		return true
	case "linux":
		// libsecret(D-Bus) 가 없는 헤드리스 서버가 흔하다. 런타임 탐지 필요.
		return os.Getenv("DBUS_SESSION_BUS_ADDRESS") != ""
	}
	return false
}
