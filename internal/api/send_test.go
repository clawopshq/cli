package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// captured 는 서버가 실제로 받은 요청이다. 계약이 틀리면 여기가 아니라 실서버에서
// 터지므로, 무엇을 보냈는지를 본다.
type captured struct {
	method string
	path   string
	body   map[string]any
}

func sendServer(t *testing.T, status int, respBody string) (*httptest.Server, *captured) {
	t.Helper()
	got := &captured{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.method = r.Method
		got.path = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&got.body)
		if status >= 400 && strings.Contains(respBody, "insufficient_scope") {
			w.Header().Set("WWW-Authenticate",
				`Bearer error="insufficient_scope", scope="write:messages"`)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(respBody))
	}))
	t.Cleanup(srv.Close)
	return srv, got
}

// 요청 필드는 Twilio 호환 PascalCase 다. Go 필드명(MediaURL)과 전송 이름(MediaUrl)이
// 다르므로 태그가 틀어지면 서버가 조용히 무시한다 — 첨부 없는 MMS 가 나간다.
func TestSendMessageWireFormat(t *testing.T) {
	srv, got := sendServer(t, 201, `{"messageId":"MG01","status":"queued"}`)
	m, err := newTestClient(srv.URL).SendMessage(context.Background(), SendMessageParams{
		To: "01000000000", From: "07000000000", Body: "본문",
		Type: "mms", Subject: "제목", MediaURL: []string{"https://example.com/a.jpg"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.method != "POST" {
		t.Errorf("POST 여야 한다, 실제 %s", got.method)
	}
	if !strings.HasSuffix(got.path, "/messages") {
		t.Errorf("/messages 로 가야 한다, 실제 %s", got.path)
	}
	for k, want := range map[string]any{
		"To": "01000000000", "From": "07000000000", "Body": "본문",
		"Type": "mms", "Subject": "제목",
	} {
		if got.body[k] != want {
			t.Errorf("%s 는 %v 여야 한다, 실제 %v", k, want, got.body[k])
		}
	}
	media, ok := got.body["MediaUrl"].([]any)
	if !ok || len(media) != 1 || media[0] != "https://example.com/a.jpg" {
		t.Errorf("MediaUrl 이 그대로 실려야 한다, 실제 %v", got.body["MediaUrl"])
	}
	if m.MessageID != "MG01" {
		t.Errorf("응답을 파싱해야 한다, 실제 %q", m.MessageID)
	}
}

// 빈 선택 필드를 보내면 서버 validator 가 enum 위반으로 400 을 준다.
// 특히 Type: "" 는 "서버 기본값(sms)에 맡긴다" 는 뜻이라 실리면 안 된다.
func TestSendMessageOmitsEmptyOptionalFields(t *testing.T) {
	srv, got := sendServer(t, 201, `{"messageId":"MG01","status":"queued"}`)
	_, err := newTestClient(srv.URL).SendMessage(context.Background(), SendMessageParams{
		To: "01000000000", From: "07000000000", Body: "본문",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"Type", "Subject", "MediaUrl"} {
		if _, present := got.body[k]; present {
			t.Errorf("비어 있는 %s 는 보내지 않아야 한다, 실제 %v", k, got.body[k])
		}
	}
	// 필수 셋은 비어 있어도 서버가 판정하도록 그대로 보낸다.
	for _, k := range []string{"To", "From", "Body"} {
		if _, present := got.body[k]; !present {
			t.Errorf("%s 는 항상 보내야 한다", k)
		}
	}
}

// 수신거부는 422 다. 이력조차 남지 않으므로 사용자가 이유를 알아야 재시도하지 않는다.
func TestSendMessageSurfacesRecipientBlocked(t *testing.T) {
	srv, _ := sendServer(t, 422, `{"error":"수신거부 명단에 등록된 번호입니다","code":"recipient_blocked"}`)
	_, err := newTestClient(srv.URL).SendMessage(context.Background(), SendMessageParams{
		To: "01000000000", From: "07000000000", Body: "본문",
	})
	if err == nil {
		t.Fatal("422 는 에러여야 한다")
	}
	if !strings.Contains(err.Error(), "수신거부") {
		t.Errorf("서버가 준 이유를 보여야 한다, 실제 %q", err.Error())
	}
}

// write:messages 는 기본 로그인에 없다(요금이 발생하는 권한). 403 이면 승격 명령을
// 그대로 띄워 주는 것이 사용자가 할 일의 전부다.
func TestSendMessageGuidesScopeUpgrade(t *testing.T) {
	srv, _ := sendServer(t, 403, `{"error":"insufficient_scope"}`)
	_, err := newTestClient(srv.URL).SendMessage(context.Background(), SendMessageParams{
		To: "01000000000", From: "07000000000", Body: "본문",
	})
	if err == nil {
		t.Fatal("403 은 에러여야 한다")
	}
	if !strings.Contains(err.Error(), "auth refresh -s write:messages") {
		t.Errorf("승격 명령을 안내해야 한다, 실제 %q", err.Error())
	}
}

// 종착 판정은 --wait 의 정지 조건이다. queued 를 종결로 보면 "보냈다" 를 "도착했다"
// 로 잘못 말하게 된다.
func TestIsTerminal(t *testing.T) {
	for status, want := range map[string]bool{
		StatusSent: true, StatusFailed: true,
		StatusQueued: false, "received": false, "": false,
	} {
		if got := IsTerminal(status); got != want {
			t.Errorf("IsTerminal(%q) = %v, 기대 %v", status, got, want)
		}
	}
}
