package cli

import (
	"io"

	"github.com/spf13/cobra"

	"github.com/clawopshq/cli/internal/api"
	"github.com/clawopshq/cli/internal/output"
)

func newNumbersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "numbers",
		Aliases: []string{"number"},
		Short:   "전화번호 조회·관리",
		RunE:    groupRunE,
	}
	cmd.AddCommand(newNumbersListCmd())
	return cmd
}

func newNumbersListCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "보유 번호를 조회한다",
		Long: "계정이 보유한 번호를 전부 보여준다.\n\n" +
			"이 라우트에는 필터도 페이지네이션도 없다 — 걸러 보려면 --json 으로 내려 jq 를 쓴다.",
		Example: "  clawops numbers list\n" +
			"  clawops numbers list --json | jq -r '.[] | select(.routingType==\"agent\") | .number'",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, _, w, err := resolveClient(cmd)
			if err != nil {
				return err
			}
			nums, err := client.ListNumbers(cmd.Context())
			if err != nil {
				return err
			}
			if len(nums) == 0 {
				w.Info("보유한 번호가 없습니다.")
			}
			return w.Data(nums, func(out io.Writer) error {
				return renderNumberTable(out, nums)
			})
		},
	}
}

// renderNumberTable 은 "이 번호로 걸려오면 어디로 가는가" 를 한 눈에 보이게 낸다.
// 라우팅 대상은 routingType 마다 다른 필드에 들어 있어 한 칸으로 모은다.
func renderNumberTable(out io.Writer, nums []api.Number) error {
	headers := []string{"번호", "유형", "라우팅", "연결 대상", "생성"}
	rows := make([][]string, 0, len(nums))
	for _, n := range nums {
		rows = append(rows, []string{
			n.Number,
			numberTypeLabel(n.NumberType),
			n.RoutingType,
			truncate(n.RoutingTarget(), 40),
			shortTime(n.CreatedAt),
		})
	}
	return output.Table(out, headers, rows)
}

func numberTypeLabel(t string) string {
	switch t {
	case api.NumberTypeDID:
		return "일반"
	case api.NumberTypeRepresentative:
		return "대표"
	}
	return t
}
