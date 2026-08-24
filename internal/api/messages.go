package api

import (
	"context"
	"net/url"
	"strconv"
)

// Message 는 문자 한 건. 필드는 openapi 의 MessageResponse 와 1:1 이다.
type Message struct {
	MessageID   string   `json:"messageId"`
	AccountID   string   `json:"accountId"`
	Direction   string   `json:"direction"` // outbound | inbound
	Status      string   `json:"status"`    // queued | sent | failed | received
	Type        string   `json:"type"`      // sms | lms | mms | rcs | kakao
	From        string   `json:"from"`
	To          string   `json:"to"`
	Subject     *string  `json:"subject"`
	Body        *string  `json:"body"`
	NumMedia    int      `json:"numMedia"`
	MediaURL    []string `json:"mediaUrl"`
	DateCreated string   `json:"dateCreated"`
	DateUpdated *string  `json:"dateUpdated"`
}

// 발신 문자의 상태값. 서버 enum 그대로다.
const (
	StatusQueued = "queued"
	StatusSent   = "sent"
	StatusFailed = "failed"
)

// IsTerminal 은 발신 문자가 더 바뀌지 않는 상태인지 알려준다.
//
// 서버는 발송 결과를 webhook 으로 받아 sent | failed 로만 종결하고, 그 뒤의 전이를
// DB CAS(`WHERE status NOT IN (sent, failed)`)로 막는다. queued 는 아직 결과가 오지
// 않은 것이다. 수신거부는 여기 끼지 않는다 — 422 로 거절되고 이력 자체가 남지 않는다.
//
// 이건 "판단" 이 아니라 서버 status enum 을 읽는 것이다. 무엇을 sent 로 볼지는
// 여전히 서버가 정하고, CLI 는 그 값이 종결인지만 본다.
func IsTerminal(status string) bool {
	return status == StatusSent || status == StatusFailed
}

// SendMessageParams 는 POST /messages 의 요청 본문이다.
// 필드명이 Twilio 호환 PascalCase 라 태그로 고정한다 (Go 이름과 다르다).
//
// Type 이 비면 서버 기본값 sms 다. 본문 길이나 첨부를 보고 CLI 가 채우지 않는다 —
// 서버(resolveMessageType)도 REST 계약에서는 명시값을 그대로 쓰고 길이로 승격하지
// 않는다. 추측해서 올리면 사용자가 모르는 사이에 단가가 바뀐다.
type SendMessageParams struct {
	To       string   `json:"To"`
	From     string   `json:"From"`
	Body     string   `json:"Body"`
	Type     string   `json:"Type,omitempty"`
	Subject  string   `json:"Subject,omitempty"`
	MediaURL []string `json:"MediaUrl,omitempty"`
}

// SendMessage 는 문자 한 건을 발송 요청한다. 성공하면 201 의 MessageResponse 다.
//
// 이 라우트는 수신자를 한 명만 받는다. 여러 명은 호출측이 반복한다 — 부분 실패를
// 어떻게 집계할지는 CLI 가 정할 문제가 아니다.
//
// 응답의 status 는 대개 queued 다. "요청을 받았다" 이지 "도착했다" 가 아니다.
func (c *Client) SendMessage(ctx context.Context, p SendMessageParams) (*Message, error) {
	var m Message
	if err := c.Do(ctx, "POST", "/messages", nil, p, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// MessageListParams 는 GET /messages 의 필터다.
//
// 서버가 지원하는 것만 둔다. 시간 범위 필터는 이 라우트에 없으므로 --since 류를
// 만들지 않는다 — CLI 가 받아 놓고 무시하면 사용자는 걸러진 줄 안다.
type MessageListParams struct {
	Status string // queued | sent | failed | received
	Type   string // sms | lms | mms
	Number string

	// Limit 은 **총 가져올 건수**다. PageSize 를 넘으면 여러 페이지를 이어 받는다
	// (twilio/gcloud/aws CLI 의 --limit 과 같은 의미).
	Limit int
	// PageSize 는 요청당 건수. 0 이면 Limit 과 서버 상한(100) 중 작은 값.
	PageSize int
}

const (
	// maxPageSize 는 서버 spec 의 pageSize 최대값이다. 넘기면 validator 가 400 을 준다
	// (service 에 Math.min 이 있지만 validator 가 먼저 돈다).
	maxPageSize = 100
	// defaultLimit 은 --limit 미지정 시 가져올 건수.
	defaultLimit = 20
)

// ListMessages 는 필요한 만큼 페이지를 이어 받아 Limit 건까지 돌려준다.
//
// 서버 페이지네이션은 page(0-based) + pageSize 다. 라우트마다 계약이 달라
// (pageSize / page_size / limit / page) 공통 파서로 통일하지 않는다 — 이 라우트의
// 계약을 그대로 따른다.
func (c *Client) ListMessages(ctx context.Context, p MessageListParams) ([]Message, error) {
	limit := p.Limit
	if limit <= 0 {
		limit = defaultLimit
	}
	pageSize := p.PageSize
	if pageSize <= 0 {
		pageSize = limit
	}
	if pageSize > maxPageSize {
		pageSize = maxPageSize
	}

	out := make([]Message, 0, limit)
	for page := 0; len(out) < limit; page++ {
		want := limit - len(out)
		size := pageSize
		if want < size {
			size = want
		}

		q := url.Values{}
		q.Set("page", strconv.Itoa(page))
		q.Set("pageSize", strconv.Itoa(size))
		if p.Status != "" {
			q.Set("status", p.Status)
		}
		if p.Type != "" {
			q.Set("type", p.Type)
		}
		if p.Number != "" {
			q.Set("number", p.Number)
		}

		var resp struct {
			Data []Message `json:"data"`
			Meta struct {
				Page     int `json:"page"`
				PageSize int `json:"pageSize"`
				Total    int `json:"total"`
			} `json:"meta"`
		}
		if err := c.Do(ctx, "GET", "/messages", q, nil, &resp); err != nil {
			return nil, err
		}
		out = append(out, resp.Data...)

		// 받은 게 요청보다 적으면 마지막 페이지다. 이 조건이 없으면 총계가 limit 보다
		// 작을 때 빈 페이지를 영원히 요청한다.
		if len(resp.Data) < size {
			break
		}
	}
	return out, nil
}

// GetMessage 는 문자 한 건을 조회한다.
func (c *Client) GetMessage(ctx context.Context, messageID string) (*Message, error) {
	var m Message
	if err := c.Do(ctx, "GET", "/messages/"+url.PathEscape(messageID), nil, nil, &m); err != nil {
		return nil, err
	}
	return &m, nil
}
