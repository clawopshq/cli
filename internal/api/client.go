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
}

func (e *Error) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("%s (HTTP %d)", e.Message, e.StatusCode)
	}
	return fmt.Sprintf("HTTP %d", e.StatusCode)
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
		return parseError(resp.StatusCode, raw)
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, out)
}

func parseError(status int, raw []byte) error {
	e := &Error{StatusCode: status, Raw: raw}
	var envelope struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &envelope); err == nil {
		e.Code = envelope.Error.Code
		e.Message = envelope.Error.Message
		if e.Message == "" {
			e.Message = envelope.Message
		}
	}
	if e.Message == "" {
		e.Message = strings.TrimSpace(string(raw))
	}
	return e
}
