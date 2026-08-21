package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

func newMessagesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "messages",
		Aliases: []string{"message", "msg"},
		Short:   "문자 발송·조회 (SMS/LMS/MMS)",
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
		status string
		number string
		typ    string
		limit  int
	)
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "문자 목록을 조회한다",
		Example: "  clawops messages list --status failed --json | jq '.[].to'",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, _, err := resolveContext()
			if err != nil {
				return err
			}
			_, _, _, _ = status, number, typ, limit
			// TODO(scaffold): GET /v1/accounts/{id}/messages
			//   페이지네이션 파라미터가 라우트마다 다르다(pageSize/page_size/limit/page).
			//   공통 파서로 통일하지 말고 이 라우트의 계약을 그대로 따른다.
			return notImplemented("messages list")
		},
	}
	f := cmd.Flags()
	f.StringVar(&status, "status", "", "상태 필터")
	f.StringVar(&number, "number", "", "번호 필터")
	f.StringVar(&typ, "type", "", "SMS | LMS | MMS")
	f.IntVar(&limit, "limit", 20, "가져올 개수")
	return cmd
}

func newMessagesGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <message-id>",
		Short: "문자 한 건을 조회한다",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return notImplemented("messages get")
		},
	}
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
