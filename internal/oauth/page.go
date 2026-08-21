package oauth

import (
	"fmt"
	"html/template"
	"net/http"
)

// 로그인 결과 페이지. CLI 가 브라우저에 남기는 유일한 화면이라 최소한의
// 형태만 갖추고 외부 리소스를 전혀 쓰지 않는다 (오프라인·사내망에서도 뜬다).
var resultPage = template.Must(template.New("result").Parse(`<!doctype html>
<html lang="ko">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}} · ClawOps CLI</title>
<style>
  :root { color-scheme: light dark; }
  body {
    margin: 0; min-height: 100vh;
    display: grid; place-items: center;
    font: 15px/1.6 -apple-system, BlinkMacSystemFont, "Segoe UI",
          "Apple SD Gothic Neo", "Noto Sans KR", sans-serif;
    background: #fff; color: #111;
  }
  @media (prefers-color-scheme: dark) {
    body { background: #0b0b0c; color: #f2f2f2; }
    .detail { color: #9a9a9e; }
    .mark.ok { background: #10331f; color: #4ade80; }
    .mark.fail { background: #3a1a1a; color: #f87171; }
  }
  .card { text-align: center; padding: 2rem; max-width: 26rem; }
  .mark {
    width: 3rem; height: 3rem; border-radius: 50%;
    display: grid; place-items: center; margin: 0 auto 1.25rem;
    font-size: 1.5rem; line-height: 1;
  }
  .mark.ok { background: #dcfce7; color: #15803d; }
  .mark.fail { background: #fee2e2; color: #b91c1c; }
  h1 { font-size: 1.0625rem; font-weight: 600; margin: 0 0 .375rem; }
  .detail { margin: 0; color: #6b7280; font-size: .875rem; word-break: break-word; }
</style>
</head>
<body>
  <div class="card">
    <div class="mark {{if .OK}}ok{{else}}fail{{end}}">{{if .OK}}&check;{{else}}&times;{{end}}</div>
    <h1>{{.Title}}</h1>
    <p class="detail">{{.Detail}}</p>
  </div>
</body>
</html>
`))

func writeResultPage(w http.ResponseWriter, ok bool, title, detail string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// 인가 코드가 붙은 URL 이므로 캐시·색인을 막는다.
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if !ok {
		w.WriteHeader(http.StatusBadRequest)
	}
	data := struct {
		OK            bool
		Title, Detail string
	}{ok, title, detail}
	if err := resultPage.Execute(w, data); err != nil {
		fmt.Fprint(w, title)
	}
}
