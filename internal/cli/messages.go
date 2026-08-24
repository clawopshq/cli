package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mattn/go-runewidth"
	"github.com/spf13/cobra"

	"github.com/clawopshq/cli/internal/api"
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
		to       []string
		from     string
		bodyFile string
		subject  string
		mediaURL []string
		watch    bool
		dryRun   bool
	)
	cmd := &cobra.Command{
		Use:   "send [본문]",
		Short: "문자를 보낸다",
		Long: "문자를 보낸다. 본문은 위치 인자, --body-file, stdin 중 하나로 준다.\n\n" +
			"--from 은 프로필의 기본 발신번호가 있으면 생략할 수 있다.\n" +
			"길이와 첨부에 따라 SMS/LMS/MMS 는 서버가 판정한다 — CLI 가 추측하지 않는다.",
		Example: "  clawops messages send \"점검 안내\" --to 01000000000\n" +
			"  echo \"본문\" | clawops messages send --to 01000000000\n" +
			"  clawops messages send --body-file notice.txt --to 01000000000 --watch",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, w, err := resolveContext()
			if err != nil {
				return err
			}
			body, err := readBody(args, bodyFile, cmd.InOrStdin())
			if err != nil {
				return err
			}
			if strings.TrimSpace(body) == "" {
				return fmt.Errorf("본문이 비어 있습니다 (위치 인자, --body-file, stdin 중 하나)")
			}
			if len(to) == 0 {
				return fmt.Errorf("--to 가 필요합니다")
			}
			_ = w
			_, _, _, _, _ = from, subject, mediaURL, watch, dryRun

			// TODO(scaffold): POST /v1/accounts/{id}/messages
			//   요청 필드는 Twilio 호환 PascalCase — To / From / Body / Type / Subject / MediaUrl.
			//   --watch 는 종착 상태까지 폴링한다. 문자는 queued 에서 멎는 실패
			//   모드가 실재하므로 "보냈다" 와 "도착했다" 를 구분해 exit code 로 낸다.
			//   write:messages 스코프가 필요하다 (`clawops auth refresh -s write:messages`).
			return notImplemented("messages send")
		},
	}
	f := cmd.Flags()
	f.StringSliceVar(&to, "to", nil, "수신번호 (반복 지정 가능)")
	f.StringVar(&from, "from", "", "발신번호 (기본: 프로필의 기본 발신번호)")
	f.StringVar(&bodyFile, "body-file", "", "본문을 파일에서 읽는다")
	f.StringVar(&subject, "subject", "", "LMS/MMS 제목")
	f.StringSliceVar(&mediaURL, "media-url", nil, "MMS 첨부 URL")
	f.BoolVar(&watch, "watch", false, "종착 상태까지 기다린다 (실패면 exit 1)")
	f.BoolVar(&dryRun, "dry-run", false, "보내지 않고 검증만 한다")
	return cmd
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
