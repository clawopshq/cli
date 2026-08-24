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
