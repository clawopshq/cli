// Package cli 는 clawops 커맨드 트리를 구성한다.
//
// 레이어 규칙 (README 의 설계 원칙과 동일):
//   - internal/api 는 HTTP 만 한다. 도메인 판단을 넣지 않는다.
//   - internal/cli 는 플래그 파싱 + 출력 선택만 한다.
//   - 검증·계산이 필요하면 서버 엔드포인트로 민다. CLI 안에 두 번째
//     진실의 원본을 만들지 않는다.
package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/learners-superpumped/clawops-cli/internal/config"
	"github.com/learners-superpumped/clawops-cli/internal/output"
)

// BuildInfo 는 main 이 ldflags 로 받은 빌드 메타데이터.
type BuildInfo struct {
	Version string
	Commit  string
	Date    string
}

// ExitError 는 종료 코드를 직접 지정하고 싶을 때 쓴다.
type ExitError struct {
	Code    int
	Message string
}

func (e *ExitError) Error() string { return e.Message }

// 전역 플래그. 모든 서브커맨드가 공유한다.
type globalFlags struct {
	profile string
	format  string // table | json
	issuer  string // discovery 대상 오버라이드 (dev/staging)
	noColor bool
	quiet   bool
}

var g globalFlags

// Execute 는 루트 커맨드를 만들고 실행한다.
func Execute(ctx context.Context, info BuildInfo) error {
	root := newRootCmd(info)
	return root.ExecuteContext(ctx)
}

func newRootCmd(info BuildInfo) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "clawops",
		Short: "ClawOps — 터미널에서 전화와 문자를 다룬다",
		Long: "clawops 는 ClawOps API 를 터미널에서 쓰는 공식 CLI 다.\n\n" +
			"인증:\n" +
			"  clawops auth login              브라우저로 로그인 (기본)\n" +
			"  CLAWOPS_API_KEY=sk_...          비대화형 환경 (CI, 컨테이너)\n\n" +
			"환경변수가 있으면 OAuth 를 건너뛴다.",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       fmt.Sprintf("%s (%s, %s)", info.Version, info.Commit, info.Date),
	}

	pf := cmd.PersistentFlags()
	pf.StringVar(&g.profile, "profile", "", "사용할 프로필 (기본: default 또는 CLAWOPS_PROFILE)")
	pf.StringVar(&g.format, "format", "table", "출력 형식: table | json")
	pf.StringVar(&g.issuer, "issuer", "", "OIDC issuer 오버라이드 (dev/staging 용)")
	pf.BoolVar(&g.noColor, "no-color", false, "색 출력 비활성화")
	pf.BoolVarP(&g.quiet, "quiet", "q", false, "사람용 출력 억제 (에러는 stderr 로 유지)")

	// --json 은 --format json 의 별칭. 파이프 사용이 압도적으로 흔해서 단축한다.
	var jsonShorthand bool
	pf.BoolVar(&jsonShorthand, "json", false, "--format json 과 동일")
	cobra.OnInitialize(func() {
		if jsonShorthand {
			g.format = "json"
		}
	})

	cmd.AddCommand(
		newAuthCmd(),
		newMessagesCmd(),
		newCallsCmd(),
		newNumbersCmd(),
	)
	return cmd
}

// resolveContext 는 서브커맨드가 공통으로 필요로 하는 것들을 모아 준다.
// 인증 해석 순서: CLAWOPS_API_KEY > 프로필 토큰 > 미인증 에러.
func resolveContext() (*config.Profile, *output.Writer, error) {
	prof, err := config.Load(g.profile)
	if err != nil {
		return nil, nil, err
	}
	if g.issuer != "" {
		prof.Issuer = g.issuer
	}
	w, err := output.New(g.format, output.Options{NoColor: g.noColor, Quiet: g.quiet})
	if err != nil {
		return nil, nil, err
	}
	return prof, w, nil
}
