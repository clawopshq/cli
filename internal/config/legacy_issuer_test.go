package config

import "testing"

// 예전 빌드는 프로필에 issuer 로 api.claw-ops.com 을 적어 두었다. 그 호스트는
// discovery 문서에서 issuer 로 auth.claw-ops.com 을 돌려주므로, OIDC Discovery
// §4.3 검사를 도입한 뒤에는 저장된 값을 그대로 쓰면 로그인도 갱신도 전부 막힌다.
// 사용자가 TOML 을 손으로 고치게 하지 않고 읽는 시점에 옮겨야 한다.
func TestLegacyIssuerIsMigratedOnLoad(t *testing.T) {
	isolate(t)

	if err := SaveProfile(&Profile{Name: "prod", Issuer: "https://api.claw-ops.com"}); err != nil {
		t.Fatal(err)
	}

	p, err := Load("prod")
	if err != nil {
		t.Fatal(err)
	}
	if p.Issuer != DefaultIssuer {
		t.Errorf("옛 issuer 가 그대로 남았다: %q (want %q)", p.Issuer, DefaultIssuer)
	}
}

// 마이그레이션이 임의의 커스텀 issuer 까지 덮어쓰면 안 된다 (dev/staging 지정).
func TestCustomIssuerSurvivesLoad(t *testing.T) {
	isolate(t)

	const custom = "https://auth.staging.example.test"
	if err := SaveProfile(&Profile{Name: "staging", Issuer: custom}); err != nil {
		t.Fatal(err)
	}

	p, err := Load("staging")
	if err != nil {
		t.Fatal(err)
	}
	if p.Issuer != custom {
		t.Errorf("커스텀 issuer 가 덮어써졌다: %q", p.Issuer)
	}
}
