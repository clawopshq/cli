package cli

import (
	"github.com/spf13/cobra"
)

func newCallsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "calls",
		Aliases: []string{"call"},
		Short:   "발신·통화 조회",
		RunE:    groupRunE,
	}
	cmd.AddCommand(newCallsCreateCmd(), newCallsListCmd(), newCallsGetCmd())
	return cmd
}

func newCallsCreateCmd() *cobra.Command {
	var (
		to, from   string
		url        string
		agentID    string
		callFlowID string
		timeout    int
		wait       bool
	)
	cmd := &cobra.Command{
		Use:   "create",
		Short: "전화를 건다",
		Long: "발신한다. 통화 내용은 --url(VoiceML), --agent, --flow 중 하나로 지정한다.\n\n" +
			"수신 차단(DNC) 검사는 서버의 createCall 관문에서 이뤄진다 — CLI 는 흉내내지 않는다.",
		Example: "  clawops calls create --to 01000000000 --agent AG00000000\n" +
			"  clawops calls create --to 01000000000 --url https://example.com/voice --wait",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, _, err := resolveContext()
			if err != nil {
				return err
			}
			_, _, _, _, _, _, _ = to, from, url, agentID, callFlowID, timeout, wait
			// TODO(scaffold): POST /v1/accounts/{id}/calls — To/From/Url/AgentId/CallFlowId/Timeout.
			return notImplemented("calls create")
		},
	}
	f := cmd.Flags()
	f.StringVar(&to, "to", "", "수신번호")
	f.StringVar(&from, "from", "", "발신번호 (기본: 프로필의 기본 발신번호)")
	f.StringVar(&url, "url", "", "VoiceML 문서 URL")
	f.StringVar(&agentID, "agent", "", "AI 에이전트 ID")
	f.StringVar(&callFlowID, "flow", "", "콜플로우 ID")
	f.IntVar(&timeout, "timeout", 0, "응답 대기 초")
	f.BoolVar(&wait, "wait", false, "통화가 끝날 때까지 기다린다 (실패면 exit 1)")
	return cmd
}

func newCallsListCmd() *cobra.Command {
	var (
		status string
		since  string
		number string
		limit  int
	)
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "통화 목록을 조회한다",
		Long: "통화 목록을 조회한다.\n\n" +
			"--json 으로 내면 jq 로 바로 집계할 수 있다. 장애 조사에서 DB 를 직접\n" +
			"붙지 않아도 되는 것이 이 명령의 존재 이유다.",
		Example: "  clawops calls list --status failed --since 1h --json | jq -r '.[].hangupCause' | sort | uniq -c",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, _, err := resolveContext()
			if err != nil {
				return err
			}
			_, _, _, _ = status, since, number, limit
			return notImplemented("calls list")
		},
	}
	f := cmd.Flags()
	f.StringVar(&status, "status", "", "상태 필터")
	f.StringVar(&since, "since", "", "기간 (예: 1h, 24h, 7d)")
	f.StringVar(&number, "number", "", "번호 필터")
	f.IntVar(&limit, "limit", 20, "가져올 개수")
	return cmd
}

func newCallsGetCmd() *cobra.Command {
	var withEvents bool
	cmd := &cobra.Command{
		Use:     "get <call-id>",
		Short:   "통화 한 건을 조회한다",
		Example: "  clawops calls get CA00000000000000000000000000000000 --events --json",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = withEvents
			return notImplemented("calls get")
		},
	}
	cmd.Flags().BoolVar(&withEvents, "events", false, "통화 이벤트를 함께 가져온다")
	return cmd
}

func newNumbersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "numbers",
		Aliases: []string{"number"},
		Short:   "전화번호 조회·관리",
		RunE:    groupRunE,
	}
	list := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "보유 번호를 조회한다",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, _, err := resolveContext()
			if err != nil {
				return err
			}
			return notImplemented("numbers list")
		},
	}
	cmd.AddCommand(list)
	return cmd
}
