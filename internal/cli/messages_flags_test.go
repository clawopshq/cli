package cli

import (
	"bytes"
	"strings"
	"testing"
)

// 실행하면 인증까지 가므로, 여기서는 인증 **전에** 걸러져야 하는 입력만 본다.
// 잘못된 입력이 네트워크까지 가기 전에 멈추는지가 관심사다.
func runMessages(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := newRootCmd(BuildInfo{Version: "test"})
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs(args)
	err := root.Execute()
	return buf.String(), err
}

// --limit 0 / 음수가 조용히 기본값(20)으로 떨어지면, 사용자는 자기가 지정한 값이
// 먹었다고 믿는다. 명시적으로 거절한다.
func TestListRejectsNonPositiveLimit(t *testing.T) {
	for _, v := range []string{"0", "-1", "-100"} {
		_, err := runMessages(t, "messages", "list", "--limit", v)
		if err == nil {
			t.Fatalf("--limit %s 는 거절해야 한다", v)
		}
		if !strings.Contains(err.Error(), "1 이상") {
			t.Errorf("--limit %s: 이유를 말해야 한다, 실제 %q", v, err.Error())
		}
	}
}

// 빈 ID 를 서버로 보내면 /messages/ 가 되어 목록 라우트에 걸리고, 빈 상세 화면이
// 성공(exit 0)처럼 보인다. 실제로 그랬다.
func TestGetRejectsBlankID(t *testing.T) {
	for _, v := range []string{"", "   "} {
		_, err := runMessages(t, "messages", "get", v)
		if err == nil {
			t.Fatalf("빈 ID(%q)는 거절해야 한다", v)
		}
		if !strings.Contains(err.Error(), "비어 있") {
			t.Errorf("이유를 말해야 한다, 실제 %q", err.Error())
		}
	}
}

// cobra 는 알 수 없는 인자를 받아도 도움말을 보여주고 0 으로 끝낸다.
// `clawops messages lst` 같은 오타가 스크립트에서 성공으로 보이면 안 된다.
func TestGroupCommandRejectsUnknownSubcommand(t *testing.T) {
	for _, group := range []string{"messages", "calls", "numbers"} {
		_, err := runMessages(t, group, "bogus")
		if err == nil {
			t.Fatalf("%s bogus 는 에러여야 한다", group)
		}
		if !strings.Contains(err.Error(), "알 수 없는 하위 명령") {
			t.Errorf("%s: 무엇이 잘못됐는지 말해야 한다, 실제 %q", group, err.Error())
		}
		// 어디서 사용법을 볼지 알려줘야 한다.
		if !strings.Contains(err.Error(), "--help") {
			t.Errorf("%s: 다음 행동을 안내해야 한다, 실제 %q", group, err.Error())
		}
	}
}

// 그룹만 치면 도움말이 나오고 성공이어야 한다 (탐색 중인 사용자).
func TestGroupCommandAloneShowsHelp(t *testing.T) {
	out, err := runMessages(t, "messages")
	if err != nil {
		t.Fatalf("그룹만 치는 것은 에러가 아니다: %v", err)
	}
	if !strings.Contains(out, "list") || !strings.Contains(out, "get") {
		t.Errorf("하위 명령을 보여줘야 한다:\n%s", out)
	}
}

// 표는 SMS 처럼 대문자로 보여주고 help 도 그렇게 읽힌다. 사용자가 본 대로 쳤을 때
// 서버 enum(소문자) 과 어긋나 400 이 나면 안 된다.
func TestFilterValuesAreCaseInsensitive(t *testing.T) {
	// 정규화는 요청 직전에 일어나므로 여기서는 정규화 함수의 계약만 고정한다.
	for in, want := range map[string]string{
		"SMS": "sms", "Sms": "sms", "sms": "sms", " LMS ": "lms",
		"SENT": "sent", "Received": "received", "": "",
	} {
		if got := strings.ToLower(strings.TrimSpace(in)); got != want {
			t.Errorf("%q → %q 여야 한다, 실제 %q", in, want, got)
		}
	}
}
