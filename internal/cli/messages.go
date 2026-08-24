package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/mattn/go-runewidth"
	"github.com/spf13/cobra"

	"github.com/clawopshq/cli/internal/api"
	"github.com/clawopshq/cli/internal/config"
	"github.com/clawopshq/cli/internal/output"
)

func newMessagesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "messages",
		Aliases: []string{"message", "msg"},
		Short:   "문자 발송·조회 (SMS/LMS/MMS)",
		RunE:    groupRunE,
	}
	cmd.AddCommand(newMessagesSendCmd(), newMessagesListCmd(), newMessagesGetCmd())
	return cmd
}

func newMessagesSendCmd() *cobra.Command {
	var (
		to          string
		from        string
		bodyFile    string
		msgType     string
		subject     string
		mediaURL    []string
		wait        bool
		waitTimeout time.Duration
		dryRun      bool
	)
	cmd := &cobra.Command{
		Use:   "send [본문]",
		Short: "문자를 보낸다",
		Long: "문자를 보낸다. 본문은 위치 인자, --body-file, stdin 중 하나로 준다. 수신자는 한 번에 한 명이다\n" +
			"— 여러 명에게 보내려면 셸에서 반복 호출한다(서버 API 자체가 건당 한 명만 받는다).\n\n" +
			"--from 은 프로필의 기본 발신번호가 있으면 생략할 수 있다.\n\n" +
			"--type 은 생략하면 서버 기본값 sms 다. **--subject 나 --media-url 을 쓰려면 --type 을\n" +
			"lms 또는 mms 로 명시해야 한다** — 서버는 본문 길이나 첨부 유무로 타입을 추측해 올려주지\n" +
			"않는다(그러면 사용자가 모르는 새 단가가 바뀐다). 안 맞으면 서버가 400 으로 거절한다.",
		Example: "  clawops messages send \"점검 안내\" --to 01000000000\n" +
			"  echo \"본문\" | clawops messages send --to 01000000000\n" +
			"  clawops messages send --type lms --subject \"공지\" --body-file notice.txt --to 01000000000\n" +
			"  clawops messages send --type mms --media-url https://example.com/a.jpg --to 01000000000 \"사진\"\n" +
			"  clawops messages send \"인증번호는 482913입니다\" --to 01000000000 --wait",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := readBody(args, bodyFile, cmd.InOrStdin())
			if err != nil {
				return err
			}
			if strings.TrimSpace(body) == "" {
				return fmt.Errorf("본문이 비어 있습니다 (위치 인자, --body-file, stdin 중 하나)")
			}
			if strings.TrimSpace(to) == "" {
				return fmt.Errorf("--to 가 필요합니다")
			}

			// --dry-run 은 인증도 네트워크도 타지 않는다. 조립 결과를 보여 주는 것이
			// 전부다 — 서버가 할 검증(타입·길이·첨부 조합)을 여기서 흉내내면 CLI 가
			// "괜찮다" 한 것을 서버가 거절하는 순간부터 아무도 CLI 를 믿지 않는다.
			if dryRun {
				prof, w, err := resolveContext()
				if err != nil {
					return err
				}
				p, err := buildSendParams(to, from, body, msgType, subject, mediaURL, prof)
				if err != nil {
					return err
				}
				w.Info("보내지 않았습니다 (--dry-run). 아래는 서버로 갈 요청입니다.")
				return w.Data(p, func(out io.Writer) error { return renderSendRequest(out, p) })
			}

			client, prof, w, err := resolveClient(cmd)
			if err != nil {
				return err
			}
			p, err := buildSendParams(to, from, body, msgType, subject, mediaURL, prof)
			if err != nil {
				return err
			}
			// 403 이면 resolveClient 가 아니라 여기서 난다. api.Error 가
			// `auth refresh -s write:messages` 승격 명령을 안내한다.
			m, err := client.SendMessage(cmd.Context(), p)
			if err != nil {
				return err
			}
			if !wait {
				return w.Data(m, func(out io.Writer) error { return renderMessageDetail(out, m) })
			}

			final, err := waitForTerminal(cmd.Context(), client, w, m.MessageID, waitTimeout)
			if err != nil {
				return err
			}
			if err := w.Data(final, func(out io.Writer) error {
				return renderMessageDetail(out, final)
			}); err != nil {
				return err
			}
			// "보냈다" 와 "도착했다" 는 다르다. 종결이 failed 면 스크립트가 알아야 한다.
			if final.Status == api.StatusFailed {
				return &ExitError{Code: 1, Message: "발송이 실패로 종결됐습니다 (" + final.MessageID + ")"}
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&to, "to", "", "수신번호 (한 번에 한 명 — 여러 명은 반복 호출)")
	f.StringVar(&from, "from", "", "발신번호 (기본: 프로필의 기본 발신번호)")
	f.StringVar(&bodyFile, "body-file", "", "본문을 파일에서 읽는다")
	f.StringVar(&msgType, "type", "", "종류 (sms|lms|mms, 기본: 서버값 sms)")
	f.StringVar(&subject, "subject", "", "제목 (--type lms 또는 mms 에서만 허용)")
	f.StringSliceVar(&mediaURL, "media-url", nil, "첨부 이미지 URL, 최대 3개 (--type mms 에서만 허용)")
	f.BoolVar(&wait, "wait", false, "종착 상태까지 기다린다 (실패면 exit 1)")
	f.DurationVar(&waitTimeout, "wait-timeout", defaultWaitTimeout, "--wait 의 최대 대기 시간")
	f.BoolVar(&dryRun, "dry-run", false, "보내지 않고 조립된 요청만 출력한다")
	return cmd
}

// --wait 의 폴링 값. 서버에 대기용 API 가 따로 없어 messages get 을 되풀이한다.
const (
	defaultWaitTimeout = 2 * time.Minute
	waitFirstInterval  = 1 * time.Second
	waitMaxInterval    = 5 * time.Second
)

// buildSendParams 는 플래그를 요청으로 조립한다.
//
// 타입·제목·첨부의 조합이 맞는지는 검사하지 않는다 — 서버가 판정한다. 여기서 채우는
// 것은 CLI 만 아는 것 하나뿐이다: --from 을 생략했을 때의 프로필 기본 발신번호.
func buildSendParams(to, from, body, msgType, subject string, mediaURL []string, prof *config.Profile) (api.SendMessageParams, error) {
	sender := strings.TrimSpace(from)
	if sender == "" {
		sender = strings.TrimSpace(prof.DefaultFrom)
	}
	if sender == "" {
		return api.SendMessageParams{}, fmt.Errorf(
			"--from 이 필요합니다 (프로필 %q 에 기본 발신번호가 없습니다)", prof.Name)
	}
	return api.SendMessageParams{
		To:   strings.TrimSpace(to),
		From: sender,
		Body: body,
		// 서버 enum 은 소문자다. 목록과 마찬가지로 사용자가 help 에서 본 대로
		// 쳤을 때(--type SMS) 400 이 나지 않게 요청 직전에 맞춘다.
		Type:     strings.ToLower(strings.TrimSpace(msgType)),
		Subject:  subject,
		MediaURL: mediaURL,
	}, nil
}

// waitForTerminal 은 문자가 종착 상태에 이를 때까지 상태를 되묻는다.
//
// 발송 API 의 200 은 "요청을 받았다" 까지다. 결과는 서버가 통신사 webhook 으로 받아
// 나중에 sent | failed 로 종결하므로, 그때까지는 queued 다.
func waitForTerminal(ctx context.Context, client *api.Client, w *output.Writer, messageID string, timeout time.Duration) (*api.Message, error) {
	deadline := time.Now().Add(timeout)
	w.Info("발송 요청됨 (%s). 종착 상태까지 기다립니다…", messageID)

	interval := waitFirstInterval
	for {
		m, err := client.GetMessage(ctx, messageID)
		if err != nil {
			return nil, err
		}
		if api.IsTerminal(m.Status) {
			return m, nil
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			// 종결을 못 봤다는 것이지 실패했다는 뜻이 아니다 — 단정하지 않는다.
			return nil, &ExitError{Code: 1, Message: fmt.Sprintf(
				"%s 안에 종착 상태에 이르지 않았습니다 (마지막 상태: %s). "+
					"나중에 `clawops messages get %s` 로 확인하세요",
				timeout, m.Status, messageID)}
		}
		// 제한 시간을 넘겨 자지 않는다 — --wait-timeout 10s 를 준 사용자가 15s 를
		// 기다리면 그건 지킨 것이 아니다.
		sleep := min(interval, remaining)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(sleep):
		}
		interval = min(interval*2, waitMaxInterval)
	}
}

func renderSendRequest(out io.Writer, p api.SendMessageParams) error {
	pairs := [][2]string{
		{"수신", p.To},
		{"발신", p.From},
	}
	if p.Type != "" {
		pairs = append(pairs, [2]string{"타입", strings.ToUpper(p.Type)})
	} else {
		pairs = append(pairs, [2]string{"타입", "(미지정 — 서버 기본값 SMS)"})
	}
	if p.Subject != "" {
		pairs = append(pairs, [2]string{"제목", p.Subject})
	}
	for i, u := range p.MediaURL {
		pairs = append(pairs, [2]string{fmt.Sprintf("첨부 [%d]", i), u})
	}
	if err := output.KV(out, pairs); err != nil {
		return err
	}
	_, err := fmt.Fprintf(out, "\n%s\n", p.Body)
	return err
}

func newMessagesListCmd() *cobra.Command {
	var (
		status   string
		number   string
		typ      string
		limit    int
		pageSize int
	)
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "문자 목록을 본다",
		Long: "문자 목록을 최신순으로 가져온다.\n\n" +
			"--limit 은 가져올 총 건수다 (기본 20). 100 건을 넘으면 나눠서 요청해 채우므로\n" +
			"페이지를 직접 넘길 필요가 없다.\n\n" +
			"--status 와 --type 은 대소문자를 가리지 않는다 (SMS 도 sms 도 된다).",
		Example: "  clawops messages list                          최근 20건\n" +
			"  clawops messages list --status failed          실패한 것만\n" +
			"  clawops messages list --number 01000000000     특정 번호가 오간 문자\n" +
			"  clawops messages list --limit 200 --json | jq -r '.[].to'",
		RunE: func(cmd *cobra.Command, args []string) error {
			if limit < 1 {
				return fmt.Errorf("--limit 은 1 이상이어야 합니다 (받은 값: %d)", limit)
			}
			client, _, w, err := resolveClient(cmd)
			if err != nil {
				return err
			}
			msgs, err := client.ListMessages(cmd.Context(), api.MessageListParams{
				// 서버 enum 은 소문자다. 표는 SMS 처럼 대문자로 보여주고 help 도 그렇게
				// 읽히므로, 사용자가 본 대로 쳤을 때 400 이 나지 않게 여기서 맞춰 준다.
				Status:   strings.ToLower(strings.TrimSpace(status)),
				Type:     strings.ToLower(strings.TrimSpace(typ)),
				Number:   strings.TrimSpace(number),
				Limit:    limit,
				PageSize: pageSize,
			})
			if err != nil {
				return err
			}
			if len(msgs) == 0 {
				w.Info("조건에 맞는 문자가 없습니다.")
			}
			// JSON 은 배열 그대로 — `... --json | jq -r '.[].to'` 가 바로 되게 한다.
			return w.Data(msgs, func(out io.Writer) error {
				return renderMessageTable(out, msgs)
			})
		},
	}
	f := cmd.Flags()
	f.StringVar(&status, "status", "", "상태 (queued|sent|failed|received)")
	f.StringVar(&typ, "type", "", "종류 (sms|lms|mms)")
	f.StringVar(&number, "number", "", "번호 (발신·수신 어느 쪽이든)")
	f.IntVar(&limit, "limit", 20, "가져올 총 건수")
	f.IntVar(&pageSize, "page-size", 0, "요청 한 번에 받을 건수 (고급)")
	return cmd
}

func newMessagesGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "get <message-id>",
		Short:   "문자 한 건을 자세히 본다",
		Example: "  clawops messages get MG00000000000000000000000000000000",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := strings.TrimSpace(args[0])
			// 빈 ID 를 그대로 보내면 /messages/ 가 되어 목록 라우트에 걸리고,
			// 빈 상세 화면이 성공(exit 0)처럼 보인다.
			if id == "" {
				return fmt.Errorf("메시지 ID 가 비어 있습니다")
			}
			client, _, w, err := resolveClient(cmd)
			if err != nil {
				return err
			}
			m, err := client.GetMessage(cmd.Context(), id)
			if err != nil {
				return err
			}
			return w.Data(m, func(out io.Writer) error {
				return renderMessageDetail(out, m)
			})
		},
	}
}

// renderMessageTable 은 목록을 한 줄에 한 건씩 낸다.
//
// body 는 개행과 긴 본문이 흔해 표를 무너뜨리므로 한 줄로 접고 잘라 낸다.
// 전문이 필요하면 `messages get` 이나 --json 을 쓴다.
func renderMessageTable(out io.Writer, msgs []api.Message) error {
	headers := []string{"MESSAGE ID", "방향", "FROM", "TO", "타입", "상태", "생성", "본문"}
	rows := make([][]string, 0, len(msgs))
	for _, m := range msgs {
		rows = append(rows, []string{
			m.MessageID,
			directionLabel(m.Direction),
			m.From,
			m.To,
			strings.ToUpper(m.Type),
			m.Status,
			shortTime(m.DateCreated),
			truncate(oneLine(deref(m.Body)), 30),
		})
	}
	return output.Table(out, headers, rows)
}

func renderMessageDetail(out io.Writer, m *api.Message) error {
	pairs := [][2]string{
		{"ID", m.MessageID},
		{"방향", directionLabel(m.Direction)},
		{"상태", m.Status},
		{"타입", strings.ToUpper(m.Type)},
		{"발신", m.From},
		{"수신", m.To},
	}
	if s := deref(m.Subject); s != "" {
		pairs = append(pairs, [2]string{"제목", s})
	}
	pairs = append(pairs, [2]string{"생성", m.DateCreated})
	if u := deref(m.DateUpdated); u != "" {
		pairs = append(pairs, [2]string{"갱신", u})
	}
	if m.NumMedia > 0 {
		pairs = append(pairs, [2]string{"첨부", fmt.Sprintf("%d개", m.NumMedia)})
		for i, u := range m.MediaURL {
			pairs = append(pairs, [2]string{fmt.Sprintf("  [%d]", i), u})
		}
	}
	if err := output.KV(out, pairs); err != nil {
		return err
	}
	// 본문은 표에 섞지 않고 아래에 원문 그대로 — 개행이 살아 있어야 읽을 수 있다.
	if b := deref(m.Body); b != "" {
		if _, err := fmt.Fprintf(out, "\n%s\n", b); err != nil {
			return err
		}
	}
	return nil
}

func directionLabel(d string) string {
	switch d {
	case "outbound":
		return "발신"
	case "inbound":
		return "수신"
	default:
		return d
	}
}

// shortTime 은 ISO 8601 에서 날짜와 분까지만 남긴다 (표 폭을 아끼려고).
func shortTime(s string) string {
	if len(s) >= 16 {
		return strings.Replace(s[:16], "T", " ", 1)
	}
	return s
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// truncate 는 표시 폭 기준으로 자른다 — 한글은 한 글자가 두 칸이다.
func truncate(s string, width int) string {
	if runewidth.StringWidth(s) <= width {
		return s
	}
	return runewidth.Truncate(s, width, "…")
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// readBody 는 본문을 위치 인자 → --body-file → stdin 순으로 읽는다.
func readBody(args []string, bodyFile string, stdin io.Reader) (string, error) {
	if len(args) > 0 && bodyFile != "" {
		return "", fmt.Errorf("본문을 위치 인자와 --body-file 로 동시에 줄 수 없습니다")
	}
	if len(args) > 0 {
		return args[0], nil
	}
	if bodyFile != "" {
		b, err := os.ReadFile(bodyFile)
		if err != nil {
			return "", fmt.Errorf("본문 파일을 읽을 수 없습니다: %w", err)
		}
		return string(b), nil
	}
	if isPiped(stdin) {
		b, err := io.ReadAll(io.LimitReader(stdin, 1<<20))
		if err != nil {
			return "", err
		}
		return strings.TrimRight(string(b), "\n"), nil
	}
	return "", nil
}

func isPiped(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return true // 테스트에서 주입한 버퍼 등
	}
	st, err := f.Stat()
	if err != nil {
		return false
	}
	return st.Mode()&os.ModeCharDevice == 0
}
