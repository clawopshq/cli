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

	"github.com/learners-superpumped/clawops-cli/internal/api"
	"github.com/learners-superpumped/clawops-cli/internal/config"
	"github.com/learners-superpumped/clawops-cli/internal/oauth"
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
	apiBase string // REST 호스트 오버라이드 (dev/staging)
	noColor bool
	quiet   bool
}

var g globalFlags

// buildVersion 은 User-Agent 에 싣는다. main 이 ldflags 로 받은 값을 Execute 가 채운다.
var buildVersion = "dev"

// Execute 는 루트 커맨드를 만들고 실행한다.
func Execute(ctx context.Context, info BuildInfo) error {
	if info.Version != "" {
		buildVersion = info.Version
	}
	root := newRootCmd(info)
	return root.ExecuteContext(ctx)
}

func newRootCmd(info BuildInfo) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "clawops",
		Short: "ClawOps — 터미널에서 전화와 문자를 다룬다",
		Long: "clawops 는 ClawOps API 를 터미널에서 쓰는 공식 CLI 다.\n\n" +
			"시작하기:\n" +
			"  clawops auth login              브라우저로 로그인\n" +
			"  clawops auth status             지금 누구로 붙어 있는지\n" +
			"  clawops messages list           최근 문자 보기\n\n" +
			"CI·컨테이너처럼 브라우저가 없는 곳에서는 CLAWOPS_API_KEY=sk_... 를 쓴다\n" +
			"(환경변수가 있으면 OAuth 를 건너뛴다).\n\n" +
			"--json 을 붙이면 결과가 stdout 에 JSON 으로만 나가므로 jq 로 바로 넘길 수 있다.",
		Example: "  clawops messages list --status failed\n" +
			"  clawops messages list --limit 200 --json | jq -r '.[].to' | sort | uniq -c",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       fmt.Sprintf("%s (%s, %s)", info.Version, info.Commit, info.Date),
	}

	pf := cmd.PersistentFlags()
	pf.StringVar(&g.profile, "profile", "", "사용할 프로필 (기본: default 또는 CLAWOPS_PROFILE)")
	pf.StringVar(&g.format, "format", "table", "출력 형식: table | json")
	pf.StringVar(&g.issuer, "issuer", "", "OIDC issuer 오버라이드 (dev/staging 용)")
	pf.StringVar(&g.apiBase, "api-base", "", "REST API 호스트 오버라이드 (dev/staging 용)")
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
	if g.apiBase != "" {
		prof.APIBase = g.apiBase
	}
	w, err := output.New(g.format, output.Options{NoColor: g.noColor, Quiet: g.quiet})
	if err != nil {
		return nil, nil, err
	}
	return prof, w, nil
}

// resolveClient 는 인증까지 마친 API 클라이언트를 만든다.
//
// TokenSource 가 API 키와 OAuth 토큰을 같은 인터페이스로 감추므로 호출부는 어느
// 자격증명으로 도는지 알 필요가 없다. 계정 ID 는 프로필에 있는 값을 쓰는데,
// OAuth 로그인 때 access token 의 account_id claim 에서 채워 둔 것이다.
func resolveClient(cmd *cobra.Command) (*api.Client, *config.Profile, *output.Writer, error) {
	prof, w, err := resolveContext()
	if err != nil {
		return nil, nil, nil, err
	}
	if prof.APIKey == "" && prof.AccountID == "" {
		return nil, nil, nil, config.ErrNotAuthenticated
	}
	if prof.AccountID == "" {
		return nil, nil, nil, fmt.Errorf(
			"프로필 %q 에 계정 ID 가 없습니다. `clawops auth login` 을 다시 실행하세요", prof.Name)
	}
	ts := &oauth.TokenSource{
		Profile: prof,
		Notify:  func(format string, args ...any) { w.Info(format, args...) },
	}
	return api.New(prof.APIBase, prof.AccountID, ts, "clawops-cli/"+buildVersion), prof, w, nil
}

// groupRunE 는 하위 명령을 묶기만 하는 커맨드의 동작이다.
//
// cobra 는 기본적으로 알 수 없는 인자를 받아도 도움말을 보여주고 **0 으로 끝난다**.
// `clawops messages lst` 같은 오타가 스크립트에서 성공으로 보이므로 직접 거절한다.
func groupRunE(cmd *cobra.Command, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("알 수 없는 하위 명령 %q — `%s --help` 로 사용법을 볼 수 있습니다",
			args[0], cmd.CommandPath())
	}
	return cmd.Help()
}

// notImplemented 는 아직 배선되지 않은 커맨드가 돌려주는 에러다.
func notImplemented(what string) error {
	return fmt.Errorf("%s: 아직 구현되지 않았습니다 (스캐폴드)", what)
}
