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

	"github.com/BurntSushi/toml"
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
	Name      string `toml:"-"`
	Issuer    string `toml:"issuer,omitempty"`
	AccountID string `toml:"account_id,omitempty"`
	// DefaultFrom 이 있으면 messages/calls 의 --from 을 생략할 수 있다.
	DefaultFrom string `toml:"default_from,omitempty"`

	// APIKey 는 파일에 저장하지 않는다. env 에서만 온다.
	APIKey string `toml:"-"`
}

// File 은 config.toml 의 내용이다.
type File struct {
	DefaultProfile string              `toml:"default_profile,omitempty"`
	Profiles       map[string]*Profile `toml:"profiles"`
}

// Load 는 프로필을 해석한다.
//
// 우선순위: 인자 name > CLAWOPS_PROFILE > config.toml 의 default_profile > "default".
// issuer 는 CLAWOPS_ISSUER > 프로필 설정 > 기본값 순.
func Load(name string) (*Profile, error) {
	f, err := LoadFile()
	if err != nil {
		return nil, err
	}
	if name == "" {
		name = os.Getenv(EnvProfile)
	}
	if name == "" {
		name = f.DefaultProfile
	}
	if name == "" {
		name = DefaultProfile
	}

	p, ok := f.Profiles[name]
	if !ok || p == nil {
		p = &Profile{}
	}
	p.Name = name
	if p.Issuer == "" {
		p.Issuer = DefaultIssuer
	}
	if v := os.Getenv(EnvIssuer); v != "" {
		p.Issuer = v
	}
	if key := os.Getenv(EnvAPIKey); key != "" {
		p.APIKey = key
	}
	return p, nil
}

// LoadFile 은 config.toml 을 읽는다. 없으면 빈 설정을 돌려준다.
func LoadFile() (*File, error) {
	path, err := filePath()
	if err != nil {
		return nil, err
	}
	f := &File{Profiles: map[string]*Profile{}}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return f, nil
	}
	if err != nil {
		return nil, err
	}
	if err := toml.Unmarshal(b, f); err != nil {
		return nil, fmt.Errorf("%s 를 읽을 수 없습니다: %w", path, err)
	}
	if f.Profiles == nil {
		f.Profiles = map[string]*Profile{}
	}
	return f, nil
}

// SaveProfile 은 프로필 하나를 config.toml 에 병합해 저장한다.
func SaveProfile(p *Profile) error {
	f, err := LoadFile()
	if err != nil {
		return err
	}
	if f.Profiles == nil {
		f.Profiles = map[string]*Profile{}
	}
	f.Profiles[p.Name] = p
	if f.DefaultProfile == "" {
		f.DefaultProfile = p.Name
	}

	path, err := filePath()
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if err := toml.NewEncoder(tmp).Encode(f); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func filePath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.toml"), nil
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
