package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type staticToken string

func (s staticToken) Token(context.Context) (string, error) { return string(s), nil }

// fakeServer 는 messages 라우트를 서버 계약대로 흉내 낸다 — page(0-based) + pageSize,
// 응답은 {data, meta}. 계약이 틀리면 여기가 아니라 실서버에서 터지므로 스펙을 따른다.
func fakeServer(t *testing.T, total int) (*httptest.Server, *[]string) {
	t.Helper()
	var queries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.RawQuery)
		page := atoiDefault(r.URL.Query().Get("page"), 0)
		size := atoiDefault(r.URL.Query().Get("pageSize"), 20)

		start := page * size
		data := []Message{}
		for i := start; i < start+size && i < total; i++ {
			data = append(data, Message{MessageID: fmt.Sprintf("MG%04d", i), To: "01000000000"})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": data,
			"meta": map[string]int{"page": page, "pageSize": size, "total": total},
		})
	}))
	t.Cleanup(srv.Close)
	return srv, &queries
}

func atoiDefault(s string, d int) int {
	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil {
		return d
	}
	return n
}

func newTestClient(url string) *Client {
	return New(url, "AC00000000000000000000000000000000", staticToken("sk_test"), "test")
}

func TestListMessagesDefaultLimit(t *testing.T) {
	srv, queries := fakeServer(t, 100)
	got, err := newTestClient(srv.URL).ListMessages(context.Background(), MessageListParams{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != defaultLimit {
		t.Fatalf("기본 limit 은 %d 건이어야 한다, 실제 %d", defaultLimit, len(got))
	}
	if len(*queries) != 1 {
		t.Fatalf("한 번만 호출해야 한다, 실제 %d회: %v", len(*queries), *queries)
	}
}

// --limit 이 서버 페이지 상한을 넘으면 CLI 가 나눠서 채운다 (twilio/gcloud 의 --limit 의미).
func TestListMessagesPaginatesBeyondServerCap(t *testing.T) {
	srv, queries := fakeServer(t, 500)
	got, err := newTestClient(srv.URL).ListMessages(context.Background(), MessageListParams{Limit: 250})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 250 {
		t.Fatalf("250 건을 채워야 한다, 실제 %d", len(got))
	}
	// 100 + 100 + 50
	if len(*queries) != 3 {
		t.Fatalf("3회 호출해야 한다, 실제 %d회: %v", len(*queries), *queries)
	}
	if !strings.Contains((*queries)[0], "pageSize=100") {
		t.Errorf("첫 요청은 상한까지 채워야 한다: %s", (*queries)[0])
	}
	if !strings.Contains((*queries)[2], "pageSize=50") {
		t.Errorf("마지막 요청은 남은 만큼만 요청해야 한다: %s", (*queries)[2])
	}
	if !strings.Contains((*queries)[1], "page=1") {
		t.Errorf("page 가 0-based 로 증가해야 한다: %s", (*queries)[1])
	}
	// 중복 없이 이어붙었는지 — 페이지 계산이 틀리면 같은 레코드가 반복된다.
	if got[0].MessageID == got[100].MessageID {
		t.Error("페이지가 겹쳤다")
	}
}

// 총계가 limit 보다 적으면 마지막 페이지에서 멈춰야 한다. 이 조건이 없으면
// 빈 페이지를 limit 에 도달할 때까지 영원히 요청한다.
func TestListMessagesStopsWhenServerRunsOut(t *testing.T) {
	srv, queries := fakeServer(t, 30)
	got, err := newTestClient(srv.URL).ListMessages(context.Background(), MessageListParams{Limit: 200})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 30 {
		t.Fatalf("있는 만큼만 돌려줘야 한다, 실제 %d", len(got))
	}
	if len(*queries) != 1 {
		t.Fatalf("서버가 요청보다 적게 주면 거기서 멈춰야 한다, 실제 %d회", len(*queries))
	}
}

func TestListMessagesSendsFilters(t *testing.T) {
	srv, queries := fakeServer(t, 5)
	_, err := newTestClient(srv.URL).ListMessages(context.Background(), MessageListParams{
		Status: "failed", Type: "lms", Number: "01000000000", Limit: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	q := (*queries)[0]
	for _, want := range []string{"status=failed", "type=lms", "number=01000000000"} {
		if !strings.Contains(q, want) {
			t.Errorf("%q 가 쿼리에 없다: %s", want, q)
		}
	}
}

func TestListMessagesOmitsEmptyFilters(t *testing.T) {
	srv, queries := fakeServer(t, 5)
	_, err := newTestClient(srv.URL).ListMessages(context.Background(), MessageListParams{Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	// 빈 필터를 보내면 서버 validator 가 enum 위반으로 400 을 준다.
	for _, bad := range []string{"status=", "type=", "number="} {
		if strings.Contains((*queries)[0], bad) {
			t.Errorf("빈 필터를 보내면 안 된다 (%s): %s", bad, (*queries)[0])
		}
	}
}

// scope 부족은 사용자가 할 일이 정해져 있는 유일한 에러다 — 무엇을 승격해야 하는지
// 헤더에서 읽어 전달해야 한다.
func TestInsufficientScopeCarriesRequiredScope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", `Bearer error="insufficient_scope", scope="read:messages"`)
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"접근 권한 없음"}`))
	}))
	defer srv.Close()

	_, err := newTestClient(srv.URL).ListMessages(context.Background(), MessageListParams{})
	if err == nil {
		t.Fatal("403 이면 에러여야 한다")
	}
	apiErr, ok := err.(*Error)
	if !ok {
		t.Fatalf("*api.Error 여야 한다, 실제 %T", err)
	}
	if apiErr.MissingScope != "read:messages" {
		t.Fatalf("필요한 scope 를 읽어야 한다, 실제 %q", apiErr.MissingScope)
	}
	if !strings.Contains(apiErr.Error(), "auth refresh -s read:messages") {
		t.Errorf("승격 명령을 안내해야 한다: %s", apiErr.Error())
	}
}

// scope 힌트가 없는 403(계정 불일치 등)까지 승격 안내를 띄우면 엉뚱한 지시가 된다.
func TestForbiddenWithoutScopeHintStaysPlain(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"접근 권한 없음"}`))
	}))
	defer srv.Close()

	_, err := newTestClient(srv.URL).ListMessages(context.Background(), MessageListParams{})
	apiErr, ok := err.(*Error)
	if !ok {
		t.Fatalf("*api.Error 여야 한다, 실제 %T", err)
	}
	if apiErr.MissingScope != "" {
		t.Errorf("scope 힌트가 없으면 비어 있어야 한다, 실제 %q", apiErr.MissingScope)
	}
	if strings.Contains(apiErr.Error(), "auth refresh") {
		t.Errorf("승격을 안내하면 안 된다: %s", apiErr.Error())
	}
}

func TestGetMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/messages/MG0001") {
			t.Errorf("경로가 다르다: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"messageId":"MG0001","status":"sent","to":"01000000000"}`))
	}))
	defer srv.Close()

	m, err := newTestClient(srv.URL).GetMessage(context.Background(), "MG0001")
	if err != nil {
		t.Fatal(err)
	}
	if m.MessageID != "MG0001" || m.Status != "sent" {
		t.Fatalf("파싱이 틀렸다: %+v", m)
	}
}
