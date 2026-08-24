package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/clawopshq/cli/internal/api"
	"github.com/clawopshq/cli/internal/config"
	"github.com/clawopshq/cli/internal/output"
)

// --to 는 필수다. 없이 보내면 서버가 400 을 주기 전에 여기서 멈춰야 한다.
func TestSendRequiresRecipient(t *testing.T) {
	_, err := runMessages(t, "messages", "send", "본문")
	if err == nil {
		t.Fatal("--to 없이 보내는 것은 거절해야 한다")
	}
	if !strings.Contains(err.Error(), "--to") {
		t.Errorf("무엇이 빠졌는지 말해야 한다, 실제 %q", err.Error())
	}
}

// 본문은 위치 인자·--body-file·stdin 중 하나다. 빈 문자를 보내면 서버가 거절하는데,
// 그 왕복과 요금 대신 여기서 멈춘다.
func TestSendRequiresBody(t *testing.T) {
	_, err := runMessages(t, "messages", "send", "--to", "01000000000", "   ")
	if err == nil {
		t.Fatal("빈 본문은 거절해야 한다")
	}
	if !strings.Contains(err.Error(), "본문") {
		t.Errorf("이유를 말해야 한다, 실제 %q", err.Error())
	}
}

// 위치 인자와 --body-file 을 동시에 주면 무엇이 나갈지 사용자가 알 수 없다.
func TestSendRejectsTwoBodySources(t *testing.T) {
	_, err := runMessages(t, "messages", "send", "본문",
		"--body-file", "notice.txt", "--to", "01000000000")
	if err == nil {
		t.Fatal("본문을 두 곳에서 주는 것은 거절해야 한다")
	}
}

// --from 을 생략하면 프로필의 기본 발신번호로 채운다. 그것도 없으면 무엇을 해야
// 하는지 말한다 — 서버의 400 은 "From 은 필수" 라고만 해서 도움이 안 된다.
func TestBuildSendParamsResolvesFrom(t *testing.T) {
	prof := &config.Profile{Name: "default", DefaultFrom: "07000000000"}

	p, err := buildSendParams("01000000000", "", "본문", "", "", nil, prof)
	if err != nil {
		t.Fatal(err)
	}
	if p.From != "07000000000" {
		t.Errorf("프로필 기본 발신번호를 써야 한다, 실제 %q", p.From)
	}

	// 플래그가 프로필을 이긴다.
	p, err = buildSendParams("01000000000", "07000000001", "본문", "", "", nil, prof)
	if err != nil {
		t.Fatal(err)
	}
	if p.From != "07000000001" {
		t.Errorf("--from 이 우선이어야 한다, 실제 %q", p.From)
	}

	_, err = buildSendParams("01000000000", "", "본문", "", "", nil, &config.Profile{Name: "default"})
	if err == nil {
		t.Fatal("발신번호를 못 정하면 거절해야 한다")
	}
	if !strings.Contains(err.Error(), "--from") {
		t.Errorf("무엇을 하라는지 말해야 한다, 실제 %q", err.Error())
	}
}

// help 와 목록 표는 타입을 SMS 로 보여준다. 본 대로 쳤을 때 서버 enum(소문자)과
// 어긋나 400 이 나면 안 된다.
func TestBuildSendParamsNormalizesType(t *testing.T) {
	prof := &config.Profile{Name: "default", DefaultFrom: "07000000000"}
	for in, want := range map[string]string{
		"MMS": "mms", " Lms ": "lms", "sms": "sms", "": "",
	} {
		p, err := buildSendParams("01000000000", "", "본문", in, "", nil, prof)
		if err != nil {
			t.Fatal(err)
		}
		if p.Type != want {
			t.Errorf("--type %q → %q 여야 한다, 실제 %q", in, want, p.Type)
		}
	}
}

// 타입·제목·첨부의 조합 검증은 서버 몫이다. CLI 가 미리 거절하면 서버가 규칙을
// 바꿨을 때 CLI 만 홀로 틀린 답을 하게 된다.
func TestBuildSendParamsDoesNotValidateCombinations(t *testing.T) {
	prof := &config.Profile{Name: "default", DefaultFrom: "07000000000"}
	// sms + 제목 + 첨부는 서버가 400 으로 거절할 조합이지만, 여기서는 통과해야 한다.
	p, err := buildSendParams("01000000000", "", "본문", "sms", "제목",
		[]string{"https://example.com/a.jpg"}, prof)
	if err != nil {
		t.Fatalf("조합 판정은 서버 몫이다: %v", err)
	}
	if p.Subject != "제목" || len(p.MediaURL) != 1 {
		t.Error("사용자가 준 값을 그대로 실어야 한다")
	}
}

func quietWriter(t *testing.T) *output.Writer {
	t.Helper()
	w, err := output.New("table", output.Options{Quiet: true})
	if err != nil {
		t.Fatal(err)
	}
	return w
}

// statusServer 는 messages get 을 흉내낸다. n 번째 조회부터 종착 상태를 준다.
func statusServer(t *testing.T, terminalAfter int32, finalStatus string) (*api.Client, *int32) {
	t.Helper()
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		status := api.StatusQueued
		if n >= terminalAfter {
			status = finalStatus
		}
		_ = json.NewEncoder(w).Encode(api.Message{MessageID: "MG01", Status: status})
	}))
	t.Cleanup(srv.Close)
	return api.New(srv.URL, "AC00000000000000000000000000000000", staticToken("sk_test"), "test"), &calls
}

type staticToken string

func (s staticToken) Token(context.Context) (string, error) { return string(s), nil }

// 발송 API 의 200 은 "요청을 받았다" 까지다. queued 를 벗어날 때까지 되물어야 한다.
func TestWaitForTerminalPollsUntilSettled(t *testing.T) {
	client, calls := statusServer(t, 2, api.StatusSent)
	m, err := waitForTerminal(context.Background(), client, quietWriter(t), "MG01", 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if m.Status != api.StatusSent {
		t.Errorf("종착 상태를 돌려줘야 한다, 실제 %q", m.Status)
	}
	if got := atomic.LoadInt32(calls); got < 2 {
		t.Errorf("queued 면 다시 물어야 한다, 조회 %d 회", got)
	}
}

// 종결이 failed 면 스크립트가 알아야 한다. 여기서는 상태를 돌려주고,
// exit code 는 호출부가 낸다.
func TestWaitForTerminalReturnsFailedStatus(t *testing.T) {
	client, _ := statusServer(t, 1, api.StatusFailed)
	m, err := waitForTerminal(context.Background(), client, quietWriter(t), "MG01", 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if m.Status != api.StatusFailed {
		t.Errorf("failed 를 그대로 돌려줘야 한다, 실제 %q", m.Status)
	}
}

// 제한 시간을 넘겨도 "실패했다" 고 단정하지 않는다 — 결과를 아직 못 본 것뿐이다.
// 어디서 확인할지 알려주는 것이 할 수 있는 전부다.
func TestWaitForTerminalTimeoutDoesNotClaimFailure(t *testing.T) {
	client, _ := statusServer(t, 999, api.StatusSent) // 계속 queued
	_, err := waitForTerminal(context.Background(), client, quietWriter(t), "MG01", time.Millisecond)
	if err == nil {
		t.Fatal("제한 시간을 넘기면 에러여야 한다")
	}
	var ee *ExitError
	if !asExitError(err, &ee) || ee.Code != 1 {
		t.Fatalf("exit 1 이어야 한다, 실제 %v", err)
	}
	if strings.Contains(ee.Message, "실패") {
		t.Errorf("실패로 단정하면 안 된다, 실제 %q", ee.Message)
	}
	if !strings.Contains(ee.Message, "messages get MG01") {
		t.Errorf("확인 방법을 알려줘야 한다, 실제 %q", ee.Message)
	}
}

func asExitError(err error, target **ExitError) bool {
	e, ok := err.(*ExitError)
	if ok {
		*target = e
	}
	return ok
}
