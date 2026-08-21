package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/learners-superpumped/clawops-cli/internal/config"
)

func newAuthCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "로그인·로그아웃·인증 상태",
	}
	cmd.AddCommand(newAuthLoginCmd(), newAuthLogoutCmd(), newAuthStatusCmd(), newAuthRefreshCmd())
	return cmd
}

func newAuthLoginCmd() *cobra.Command {
	var (
		noBrowser bool
		scopes    []string
	)
	cmd := &cobra.Command{
		Use:   "login",
		Short: "브라우저로 로그인한다",
		Long: "브라우저를 열어 ClawOps 계정으로 로그인한다.\n\n" +
			"흐름 (RFC 8252 native app):\n" +
			"  1. {issuer}/.well-known/openid-configuration 으로 엔드포인트 discovery\n" +
			"  2. 127.0.0.1 의 임의 포트에 콜백 서버를 띄운다\n" +
			"  3. PKCE(S256) + state 로 /authorize 열기\n" +
			"  4. code 수신 → /token 교환 → 키체인 저장\n\n" +
			"브라우저를 열 수 없는 환경(SSH, 컨테이너)에서는 API 키를 쓴다:\n" +
			"  clawops api-keys create --name ci\n" +
			"  export CLAWOPS_API_KEY=sk_...",
		Example: "  clawops auth login\n" +
			"  clawops auth login --profile sandbox\n" +
			"  clawops auth login --scope write:calls --scope write:messages",
		RunE: func(cmd *cobra.Command, args []string) error {
			prof, w, err := resolveContext()
			if err != nil {
				return err
			}
			if prof.APIKey != "" {
				w.Info("%s 가 설정돼 있습니다. 이 환경에서는 로그인이 필요 없습니다.", config.EnvAPIKey)
				return nil
			}

			// TODO(scaffold): PKCE + loopback 흐름.
			//   - 서버는 redirect_uri 로 http://127.0.0.1:*/cb 를 이미 허용한다
			//     (app/src/oidc/redirect-policy.ts, RFC 8252 §7.3 wildcard).
			//   - client_id 는 config.CLIClientID, auth_method 는 none.
			//   - 기본 스코프는 read:* + offline_access. write 는 필요할 때
			//     `clawops auth refresh -s write:calls` 로 승격시킨다.
			_ = noBrowser
			_ = scopes
			return notImplemented("auth login")
		},
	}
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "브라우저를 열지 않고 URL 만 출력한다")
	cmd.Flags().StringSliceVarP(&scopes, "scope", "s", nil, "추가로 요청할 스코프 (예: write:calls)")
	return cmd
}

func newAuthRefreshCmd() *cobra.Command {
	var scopes []string
	cmd := &cobra.Command{
		Use:   "refresh",
		Short: "스코프를 추가하거나 토큰을 갱신한다",
		Long: "이미 로그인한 프로필에 스코프를 추가한다.\n\n" +
			"발신·발송 권한(write:calls, write:messages)은 실제로 요금이 발생하므로\n" +
			"기본 로그인에 포함하지 않는다. 필요할 때 이 명령으로 승격시킨다.",
		Example: "  clawops auth refresh -s write:messages",
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = scopes
			return notImplemented("auth refresh")
		},
	}
	cmd.Flags().StringSliceVarP(&scopes, "scope", "s", nil, "추가할 스코프")
	return cmd
}

func newAuthLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "토큰을 폐기하고 로컬에서 지운다",
		Long:  "서버의 grant 를 revoke 하고(RFC 7009) 키체인에서 토큰을 삭제한다.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return notImplemented("auth logout")
		},
	}
}

func newAuthStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "현재 인증 상태를 보여준다",
		RunE: func(cmd *cobra.Command, args []string) error {
			prof, w, err := resolveContext()
			if err != nil {
				return err
			}
			if prof.APIKey != "" {
				w.Info("인증: API 키 (%s)", config.EnvAPIKey)
				w.Info("issuer: %s", prof.Issuer)
				return nil
			}
			return notImplemented("auth status")
		},
	}
}

func notImplemented(what string) error {
	return fmt.Errorf("%s: 아직 구현되지 않았습니다 (스캐폴드)", what)
}
