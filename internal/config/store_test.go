package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func isolate(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(EnvAPIKey, "")
	t.Setenv(EnvProfile, "")
	t.Setenv(EnvIssuer, "")
}

// 키체인을 못 쓰는 환경에서도 저장·조회·삭제가 되어야 한다.
func TestFileTokenRoundTrip(t *testing.T) {
	isolate(t)
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "") // linux 에서 키체인 비활성

	want := &Token{
		AccessToken:  "at",
		RefreshToken: "rt",
		Expiry:       time.Now().Add(time.Hour).Truncate(time.Second),
		Scopes:       []string{"read:calls"},
		AccountID:    "AC00000000000000000000000000000000",
	}
	if err := saveFileToken("default", mustJSON(t, want)); err != nil {
		t.Fatal(err)
	}

	got, err := loadFileToken("default")
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != want.AccessToken || got.RefreshToken != want.RefreshToken {
		t.Errorf("토큰이 왕복하지 않았다: %+v", got)
	}

	if err := deleteFileToken("default"); err != nil {
		t.Fatal(err)
	}
	if _, err := loadFileToken("default"); !errors.Is(err, ErrNoToken) {
		t.Errorf("삭제 후에도 조회된다: %v", err)
	}
}

// 폴백 파일은 소유자만 읽을 수 있어야 한다. 발신 권한이 붙은 토큰이다.
func TestCredentialsFilePermissions(t *testing.T) {
	// Windows 의 파일 권한은 POSIX 모드 비트가 아니다 (Chmod 는 읽기전용 비트만
	// 바꾼다). Windows 는 wincred 를 쓰므로 이 폴백 경로 자체를 거의 타지 않는다.
	if runtime.GOOS == "windows" {
		t.Skip("POSIX 권한 비트가 없는 플랫폼")
	}
	isolate(t)
	if err := saveFileToken("default", []byte(`{"access_token":"at"}`)); err != nil {
		t.Fatal(err)
	}
	path, err := credentialsPath()
	if err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := st.Mode().Perm(); perm != 0o600 {
		t.Errorf("credentials.json 권한이 %o 다. 0600 이어야 한다", perm)
	}
	if dir := filepath.Dir(path); dir != "" {
		if dst, err := os.Stat(dir); err == nil && dst.Mode().Perm() != 0o700 {
			t.Errorf("설정 디렉토리 권한이 %o 다. 0700 이어야 한다", dst.Mode().Perm())
		}
	}
}

func TestExpiredTolerance(t *testing.T) {
	cases := []struct {
		name string
		tok  *Token
		want bool
	}{
		{"토큰 없음", nil, true},
		{"빈 access token", &Token{}, true},
		{"만료됨", &Token{AccessToken: "at", Expiry: time.Now().Add(-time.Minute)}, true},
		{"30초 뒤 만료 — 여유분 안에 든다", &Token{AccessToken: "at", Expiry: time.Now().Add(30 * time.Second)}, true},
		{"충분히 남음", &Token{AccessToken: "at", Expiry: time.Now().Add(time.Hour)}, false},
		{"만료 없음", &Token{AccessToken: "at"}, false},
	}
	for _, c := range cases {
		if got := c.tok.Expired(); got != c.want {
			t.Errorf("%s: Expired() = %v, want %v", c.name, got, c.want)
		}
	}
}

// 프로필 우선순위: 인자 > CLAWOPS_PROFILE > config.toml 의 default_profile.
func TestProfileResolution(t *testing.T) {
	isolate(t)
	if err := SaveProfile(&Profile{Name: "prod", Issuer: "https://prod.example.com"}); err != nil {
		t.Fatal(err)
	}
	if err := SaveProfile(&Profile{Name: "sandbox", Issuer: "https://sandbox.example.com"}); err != nil {
		t.Fatal(err)
	}

	// default_profile 은 처음 저장한 prod 로 잡힌다.
	p, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "prod" {
		t.Errorf("기본 프로필 = %q, prod 여야 한다", p.Name)
	}

	t.Setenv(EnvProfile, "sandbox")
	if p, _ = Load(""); p.Name != "sandbox" {
		t.Errorf("env 프로필이 무시됐다: %q", p.Name)
	}
	if p, _ = Load("prod"); p.Name != "prod" {
		t.Errorf("인자가 env 를 이기지 못했다: %q", p.Name)
	}
	if p.Issuer != "https://prod.example.com" {
		t.Errorf("issuer 가 복원되지 않았다: %q", p.Issuer)
	}

	// CLAWOPS_ISSUER 는 저장된 값을 덮어쓴다.
	t.Setenv(EnvIssuer, "http://localhost:3010")
	if p, _ = Load("prod"); p.Issuer != "http://localhost:3010" {
		t.Errorf("env issuer 가 무시됐다: %q", p.Issuer)
	}
}

func mustJSON(t *testing.T, tok *Token) []byte {
	t.Helper()
	b, err := marshalToken(tok)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
