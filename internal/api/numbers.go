package api

import (
	"context"
	"net/url"
)

// Number 는 계정이 보유한 전화번호 한 건. 필드는 openapi 의 NumberResponse 와 1:1 이다.
//
// 라우팅 대상 필드(SipEndpointID·ForwardTo·CallFlowID·AgentID·SipCredentialID)는
// RoutingType 에 해당하는 것 하나만 채워지고 나머지는 null 이다.
type Number struct {
	Number      string `json:"number"`
	Source      string `json:"source"`
	NumberType  string `json:"numberType"`  // did | representative
	RoutingType string `json:"routingType"` // webhook | sip | softphone | forward | callflow | agent

	WebhookURL     string `json:"webhookUrl"`
	WebhookMethod  string `json:"webhookMethod"` // POST | GET
	CallContextURL string `json:"callContextUrl"`

	SipEndpointID   string `json:"sipEndpointId"`
	SipCredentialID string `json:"sipCredentialId"`
	ForwardTo       string `json:"forwardTo"`
	CallFlowID      string `json:"callFlowId"`
	AgentID         string `json:"agentId"`

	WebhookHeaders       map[string]string `json:"webhookHeaders"`
	StatusCallback       string            `json:"statusCallback"`
	StatusCallbackEvents string            `json:"statusCallbackEvents"`
	DictionaryID         string            `json:"dictionaryId"`

	CreatedAt string `json:"createdAt"`
}

// 번호 유형.
const (
	NumberTypeDID            = "did"
	NumberTypeRepresentative = "representative"
)

// RoutingTarget 은 현재 라우팅이 실제로 가리키는 값을 돌려준다.
//
// 어느 필드가 유효한지는 RoutingType 이 정한다 — 서버가 그렇게 채워 보내므로
// CLI 는 표시할 것을 고르기만 한다. 판정이 아니라 표시 선택이다.
func (n Number) RoutingTarget() string {
	switch n.RoutingType {
	case "webhook":
		return n.WebhookURL
	case "agent":
		return n.AgentID
	case "callflow":
		return n.CallFlowID
	case "forward":
		return n.ForwardTo
	case "sip":
		return n.SipEndpointID
	case "softphone":
		return n.SipCredentialID
	}
	return ""
}

// ListNumbers 는 계정이 보유한 번호를 전부 돌려준다.
//
// 이 라우트에는 필터도 페이지네이션도 없다 (messages 와 다르다). 계약에 없는
// --limit 류를 CLI 가 지어내면 사용자는 걸러진 줄 안다.
func (c *Client) ListNumbers(ctx context.Context) ([]Number, error) {
	var resp struct {
		Data []Number `json:"data"`
	}
	if err := c.Do(ctx, "GET", "/numbers", nil, nil, &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// RegisterNumberParams 는 POST /numbers 의 본문이다. 전부 선택이다 —
// 번호 자체는 서버가 풀에서 고른다.
type RegisterNumberParams struct {
	WebhookURL           string            `json:"webhookUrl,omitempty"`
	WebhookMethod        string            `json:"webhookMethod,omitempty"`
	WebhookHeaders       map[string]string `json:"webhookHeaders,omitempty"`
	StatusCallback       string            `json:"statusCallback,omitempty"`
	StatusCallbackEvents string            `json:"statusCallbackEvents,omitempty"`
}

// RegisterNumber 는 번호 풀에서 번호 하나를 발급받는다.
// 계정 할당량을 넘기면 422 다.
func (c *Client) RegisterNumber(ctx context.Context, p RegisterNumberParams) (*Number, error) {
	var n Number
	if err := c.Do(ctx, "POST", "/numbers", nil, p, &n); err != nil {
		return nil, err
	}
	return &n, nil
}

// CreateRepresentativeNumber 는 대표번호를 발급받는다.
//
// 요청 본문이 없다 — webhook 설정을 함께 줄 수 없으므로 발급 후 UpdateNumber 로
// 설정한다. 부가서비스 미활성은 402, 풀 고갈·법인인증 미완료는 409 다.
func (c *Client) CreateRepresentativeNumber(ctx context.Context) (*Number, error) {
	var n Number
	if err := c.Do(ctx, "POST", "/representative-numbers", nil, nil, &n); err != nil {
		return nil, err
	}
	return &n, nil
}

// UpdateNumberParams 는 PUT /numbers/{number} 의 본문이다.
//
// 포인터인 이유: 서버가 "안 보냄"(그대로 둠)과 "빈 값 보냄"(해제)을 구분한다.
// nil 은 omitempty 로 빠지고, 빈 문자열을 가리키는 포인터는 ""(해제)로 나간다.
// 하나도 안 보내면 서버가 "수정할 필드 없음" 400 을 준다.
type UpdateNumberParams struct {
	RoutingType     *string `json:"routingType,omitempty"`
	WebhookURL      *string `json:"webhookUrl,omitempty"`
	WebhookMethod   *string `json:"webhookMethod,omitempty"`
	AgentID         *string `json:"agentId,omitempty"`
	CallFlowID      *string `json:"callFlowId,omitempty"`
	ForwardTo       *string `json:"forwardTo,omitempty"`
	SipEndpointID   *string `json:"sipEndpointId,omitempty"`
	SipCredentialID *string `json:"sipCredentialId,omitempty"`
	CallContextURL  *string `json:"callContextUrl,omitempty"`

	StatusCallback       *string `json:"statusCallback,omitempty"`
	StatusCallbackEvents *string `json:"statusCallbackEvents,omitempty"`
	DictionaryID         *string `json:"dictionaryId,omitempty"`
}

// UpdateNumber 는 번호의 라우팅·webhook 설정을 바꾼다.
func (c *Client) UpdateNumber(ctx context.Context, number string, p UpdateNumberParams) (*Number, error) {
	var n Number
	if err := c.Do(ctx, "PUT", "/numbers/"+url.PathEscape(number), nil, p, &n); err != nil {
		return nil, err
	}
	return &n, nil
}

// DeleteNumber 는 번호를 반납한다. 번호는 풀로 복귀하므로 같은 번호를 다시 받는다는
// 보장이 없다. 성공하면 204(본문 없음)다.
func (c *Client) DeleteNumber(ctx context.Context, number string) error {
	return c.Do(ctx, "DELETE", "/numbers/"+url.PathEscape(number), nil, nil, nil)
}
