package api

import (
	"strings"
	"testing"
)

// 서버의 `error` 는 문자열이다. 객체로 파싱하려 들면 통째로 실패해 JSON 원문이
// 그대로 사용자에게 노출된다 — 실제로 그랬다.
func TestParseErrorServiceMessage(t *testing.T) {
	raw := []byte(`{"error":"메시지를 찾을 수 없습니다"}`)
	e, ok := parseError(404, raw).(*Error)
	if !ok {
		t.Fatal("*Error 여야 한다")
	}
	if e.Message != "메시지를 찾을 수 없습니다" {
		t.Fatalf("문자열 error 를 못 읽었다: %q", e.Message)
	}
	if strings.Contains(e.Error(), "{") {
		t.Errorf("JSON 원문이 새면 안 된다: %s", e.Error())
	}
}

// validator 는 어느 파라미터가 틀렸는지 errors[] 로 알려 준다.
// "request/query/status must be ..." 통짜 문장보다 필드명이 쓸모 있다.
func TestParseErrorValidationListsFields(t *testing.T) {
	raw := []byte(`{"error":"request/query/status must be equal to one of the allowed values: queued, sent, failed, received","errors":[{"path":"/query/status","message":"must be equal to one of the allowed values: queued, sent, failed, received","errorCode":"enum.openapi.validation"}],"code":"VALIDATION"}`)
	e := parseError(400, raw).(*Error)

	if e.Code != "VALIDATION" {
		t.Errorf("code 를 읽어야 한다: %q", e.Code)
	}
	if !strings.HasPrefix(e.Message, "status: ") {
		t.Fatalf("필드명이 앞에 와야 한다: %q", e.Message)
	}
	if strings.Contains(e.Message, "request/query/") {
		t.Errorf("내부 경로 접두는 벗겨야 한다: %q", e.Message)
	}
	if strings.Contains(e.Error(), "errorCode") {
		t.Errorf("JSON 원문이 새면 안 된다: %s", e.Error())
	}
}

func TestParseErrorMultipleValidationFields(t *testing.T) {
	raw := []byte(`{"errors":[{"path":"/body/To","message":"is required"},{"path":"/body/Body","message":"is required"}],"code":"VALIDATION"}`)
	e := parseError(400, raw).(*Error)
	if !strings.Contains(e.Message, "To: is required") || !strings.Contains(e.Message, "Body: is required") {
		t.Fatalf("여러 위반을 모두 보여야 한다: %q", e.Message)
	}
}

// 혹시 다른 라우트가 객체 형태를 쓰더라도 원문 노출로 떨어지지 않아야 한다.
func TestParseErrorObjectShapeStillWorks(t *testing.T) {
	raw := []byte(`{"error":{"code":"rate_limited","message":"너무 많은 요청"}}`)
	e := parseError(429, raw).(*Error)
	if e.Message != "너무 많은 요청" {
		t.Fatalf("객체 형태도 읽어야 한다: %q", e.Message)
	}
	if e.Code != "rate_limited" {
		t.Errorf("객체의 code 도 읽어야 한다: %q", e.Code)
	}
}

// JSON 이 아니거나 예상 밖이면 원문이라도 보여 준다 — 조용히 삼키는 것보다 낫다.
func TestParseErrorFallsBackToRaw(t *testing.T) {
	e := parseError(502, []byte("upstream connect error")).(*Error)
	if e.Message != "upstream connect error" {
		t.Fatalf("원문 폴백: %q", e.Message)
	}
}

func TestErrorStringIncludesStatus(t *testing.T) {
	e := &Error{StatusCode: 404, Message: "없음"}
	if !strings.Contains(e.Error(), "404") {
		t.Errorf("상태 코드를 보여야 한다: %s", e.Error())
	}
}
