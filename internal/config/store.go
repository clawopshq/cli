package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/zalando/go-keyring"
)

// keyringService 는 OS 키체인 항목의 service 이름. user 는 프로필 이름을 쓴다.
const keyringService = "clawops"

// Token 은 한 프로필의 자격증명이다.
type Token struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	Expiry       time.Time `json:"expiry"`
	Scopes       []string  `json:"scopes,omitempty"`

	// 표시 전용. access token 의 claim 에서 채운다.
	AccountID string `json:"account_id,omitempty"`
	Email     string `json:"email,omitempty"`
}

// Expired 는 만료됐거나 곧 만료될 때 true 를 돌려준다.
// 60 초 여유를 두어 요청 도중 만료되는 것을 피한다.
func (t *Token) Expired() bool {
	if t == nil || t.AccessToken == "" {
		return true
	}
	if t.Expiry.IsZero() {
		return false
	}
	return time.Now().After(t.Expiry.Add(-60 * time.Second))
}

// ErrNoToken 은 저장된 자격증명이 없을 때 반환한다.
var ErrNoToken = errors.New("저장된 자격증명이 없습니다")

// SaveToken 은 토큰을 키체인에 저장한다. 키체인을 쓸 수 없으면 0600 파일로 내려간다.
func SaveToken(profile string, t *Token) error {
	b, err := json.Marshal(t)
	if err != nil {
		return err
	}
	if keychainAvailable() {
		if err := keyring.Set(keyringService, profile, string(b)); err == nil {
			// 폴백 파일에 남아 있던 예전 값을 지운다 — 두 곳에 있으면 어느 쪽이
			// 진짜인지 모르게 된다.
			_ = deleteFileToken(profile)
			return nil
		}
		// 키체인이 있다고 판단했는데 실패한 경우(잠긴 키체인, D-Bus 거부 등)
		// 조용히 파일로 내려간다. 사용자를 막지는 않는다.
	}
	return saveFileToken(profile, b)
}

// LoadToken 은 키체인 → 폴백 파일 순으로 찾는다.
func LoadToken(profile string) (*Token, error) {
	if keychainAvailable() {
		s, err := keyring.Get(keyringService, profile)
		if err == nil {
			var t Token
			if err := json.Unmarshal([]byte(s), &t); err != nil {
				return nil, fmt.Errorf("키체인의 자격증명을 읽을 수 없습니다: %w", err)
			}
			return &t, nil
		}
		if !errors.Is(err, keyring.ErrNotFound) && !errors.Is(err, keyring.ErrUnsupportedPlatform) {
			// 조회 자체가 실패했으면 파일도 확인해 본다.
			_ = err
		}
	}
	return loadFileToken(profile)
}

// DeleteToken 은 두 저장소 모두에서 지운다.
func DeleteToken(profile string) error {
	var firstErr error
	if keychainAvailable() {
		if err := keyring.Delete(keyringService, profile); err != nil &&
			!errors.Is(err, keyring.ErrNotFound) && !errors.Is(err, keyring.ErrUnsupportedPlatform) {
			firstErr = err
		}
	}
	if err := deleteFileToken(profile); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

// --- 폴백 파일 (~/.config/clawops/credentials.json, 0600) ---

func credentialsPath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "credentials.json"), nil
}

type credentialsFile map[string]json.RawMessage

func readCredentials() (credentialsFile, string, error) {
	path, err := credentialsPath()
	if err != nil {
		return nil, "", err
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return credentialsFile{}, path, nil
	}
	if err != nil {
		return nil, "", err
	}
	var cf credentialsFile
	if err := json.Unmarshal(b, &cf); err != nil {
		return nil, "", fmt.Errorf("%s 를 읽을 수 없습니다: %w", path, err)
	}
	if cf == nil {
		cf = credentialsFile{}
	}
	return cf, path, nil
}

func writeCredentials(cf credentialsFile, path string) error {
	b, err := json.MarshalIndent(cf, "", "  ")
	if err != nil {
		return err
	}
	// 같은 디렉토리에 임시 파일로 쓰고 rename — 쓰다 죽어도 반쪽 파일이 남지 않는다.
	tmp, err := os.CreateTemp(filepath.Dir(path), ".credentials-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func saveFileToken(profile string, raw []byte) error {
	cf, path, err := readCredentials()
	if err != nil {
		return err
	}
	cf[profile] = raw
	return writeCredentials(cf, path)
}

func loadFileToken(profile string) (*Token, error) {
	cf, _, err := readCredentials()
	if err != nil {
		return nil, err
	}
	raw, ok := cf[profile]
	if !ok {
		return nil, ErrNoToken
	}
	var t Token
	if err := json.Unmarshal(raw, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

func deleteFileToken(profile string) error {
	cf, path, err := readCredentials()
	if err != nil {
		return err
	}
	if _, ok := cf[profile]; !ok {
		return nil
	}
	delete(cf, profile)
	return writeCredentials(cf, path)
}

// ListProfiles 는 자격증명이 저장된 프로필 이름을 돌려준다.
// 키체인은 열거를 지원하지 않으므로 config.toml 에 적힌 프로필이 SoT 다.
func ListProfiles() ([]string, error) {
	cfg, err := LoadFile()
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(cfg.Profiles))
	for name := range cfg.Profiles {
		names = append(names, name)
	}
	return names, nil
}

// marshalToken 은 테스트에서도 같은 직렬화를 쓰도록 노출한 헬퍼다.
func marshalToken(t *Token) ([]byte, error) { return json.Marshal(t) }
