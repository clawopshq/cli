// Package api 는 ClawOps REST API 를 호출하는 얇은 HTTP 클라이언트다.
//
// 이 패키지는 HTTP 만 한다. 페이로드 검증, VoiceML 파싱, 요금 계산 같은
// "판단" 은 넣지 않는다 — 그건 서버에 이미 있고, 여기에 다시 만들면 두 개의
// 진실이 생긴다.
//
// 최종형은 app/src/swagger/openapi.bundled.json 을 oapi-codegen 에 먹여
// 생성한 타입드 클라이언트로 대체하는 것이다 (operationId 112 개, 태그 15 개).
// 지금은 스캐폴드라 손으로 최소한만 둔다.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const defaultTimeout = 30 * time.Second

// TokenSource 는 요청마다 Bearer 값을 공급한다.
// OAuth 토큰과 API 키가 같은 인터페이스를 쓰므로 호출부는 둘을 구분하지 않는다.
type TokenSource interface {
	Token(ctx context.Context) (string, error)
}

type Client struct {
	BaseURL   string
	AccountID string
	Tokens    TokenSource
	UserAgent string

	HTTP *http.Client
}

func New(baseURL, accountID string, ts TokenSource, userAgent string) *Client {
	return &Client{
		BaseURL:   strings.TrimRight(baseURL, "/"),
		AccountID: accountID,
		Tokens:    ts,
		UserAgent: userAgent,
		HTTP:      &http.Client{Timeout: defaultTimeout},
	}
}

// Error 는 API 가 돌려준 오류다.
//
// 서버 검증이 두 층(요청 validator → 도메인 핸들러)이라 같은 400 이라도 형태가
// 다를 수 있다. 원문을 버리지 않고 Raw 에 보관한다.
type Error struct {
	StatusCode int
	Code       string
	Message    string
	Raw        json.RawMessage

	// MissingScope 는 서버가 scope 부족으로 거절하면서 알려 준 필요 scope 다
	// (RFC 6750 의 WWW-Authenticate: Bearer error="insufficient_scope", scope="...").
	// 사용자가 할 일이 정해져 있는 유일한 에러라 따로 보관한다 — 승격 명령을 안내한다.
	MissingScope string
}

func (e *Error) Error() string {
	if e.MissingScope != "" {
		return fmt.Sprintf("권한이 없습니다 (%s). 승격하세요:\n  clawops auth refresh -s %s",
			e.MissingScope, e.MissingScope)
	}
	if e.Message != "" {
		return fmt.Sprintf("%s (HTTP %d)", e.Message, e.StatusCode)
	}
	return fmt.Sprintf("HTTP %d", e.StatusCode)
}

// scopeRe 는 WWW-Authenticate 에서 scope="..." 를 뽑는다.
var scopeRe = regexp.MustCompile(`scope="([^"]+)"`)

// missingScopeFrom 은 insufficient_scope 응답에서 필요한 scope 를 읽는다.
// 다른 이유의 403(계정 불일치 등)은 scope 힌트가 없으므로 빈 문자열이 된다.
func missingScopeFrom(header string) string {
	if !strings.Contains(header, "insufficient_scope") {
		return ""
	}
	if m := scopeRe.FindStringSubmatch(header); len(m) == 2 {
		return m[1]
	}
	return ""
}

// Do 는 /v1/accounts/{accountId} 하위 경로를 호출한다.
func (c *Client) Do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	u := c.BaseURL + "/v1/accounts/" + url.PathEscape(c.AccountID) + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}

	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("요청 본문 인코딩 실패: %w", err)
		}
		rdr = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, u, rdr)
	if err != nil {
		return err
	}
	token, err := c.Tokens.Token(ctx)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", c.UserAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		apiErr := parseError(resp.StatusCode, raw)
		if e, ok := apiErr.(*Error); ok {
			e.MissingScope = missingScopeFrom(resp.Header.Get("WWW-Authenticate"))
		}
		return apiErr
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, out)
}

// parseError 는 서버가 돌려준 오류 본문을 사람이 읽을 문장으로 만든다.
//
// ⚠️ `error` 는 **문자열**이다 — 객체가 아니다. 검증이 두 층이라 형태가 둘인데
// 둘 다 문자열을 쓴다:
//
//	업무 규칙 위반(service):  {"error":"메시지를 찾을 수 없습니다"}
//	형식 위반(validator):     {"error":"request/query/status must be ...",
//	                           "errors":[{"path":"/query/status","message":"..."}],
//	                           "code":"VALIDATION"}
//
// 객체로 파싱하려 들면 타입 불일치로 통째로 실패해 JSON 원문이 그대로 사용자에게
// 노출된다. 옛 구현이 실제로 그랬다. 혹시 모를 객체 형태도 함께 받아 둔다.
func parseError(status int, raw []byte) error {
	e := &Error{StatusCode: status, Raw: raw}

	var envelope struct {
		Error  json.RawMessage `json:"error"`
		Errors []struct {
			Path    string `json:"path"`
			Message string `json:"message"`
		} `json:"errors"`
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &envelope); err == nil {
		e.Code = envelope.Code

		var asString string
		if err := json.Unmarshal(envelope.Error, &asString); err == nil {
			e.Message = asString
		} else {
			var asObject struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			}
			if err := json.Unmarshal(envelope.Error, &asObject); err == nil {
				e.Message = asObject.Message
				if e.Code == "" {
					e.Code = asObject.Code
				}
			}
		}
		if e.Message == "" {
			e.Message = envelope.Message
		}

		// validator 의 필드별 위반은 어느 파라미터가 문제인지 말해 준다.
		// "request/query/status must be ..." 라는 통짜 문장보다 이쪽이 쓸모 있다.
		if len(envelope.Errors) > 0 {
			parts := make([]string, 0, len(envelope.Errors))
			for _, v := range envelope.Errors {
				field := strings.TrimPrefix(v.Path, "/query/")
				field = strings.TrimPrefix(field, "/body/")
				field = strings.TrimPrefix(field, "/")
				if field == "" {
					parts = append(parts, v.Message)
					continue
				}
				parts = append(parts, fmt.Sprintf("%s: %s", field, v.Message))
			}
			e.Message = strings.Join(parts, "; ")
		}
	}

	if e.Message == "" {
		e.Message = strings.TrimSpace(string(raw))
	}
	return e
}
