package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/learners-superpumped/clawops-cli/internal/config"
	"github.com/learners-superpumped/clawops-cli/internal/oauth"
	"github.com/learners-superpumped/clawops-cli/internal/output"
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
		Long: "브라우저를 열어 ClawOps 계정으로 로그인한다 (RFC 8252 native app).\n\n" +
			"기본 스코프는 read:* 전체다. 발신·발송 권한은 요금이 발생하므로\n" +
			"필요할 때 `clawops auth refresh -s write:messages` 로 승격시킨다.\n\n" +
			"브라우저를 열 수 없는 환경(SSH, 컨테이너)에서는 API 키를 쓴다:\n" +
			"  export CLAWOPS_API_KEY=sk_...",
		Example: "  clawops auth login\n" +
			"  clawops auth login --profile sandbox\n" +
			"  clawops auth login -s write:calls -s write:messages",
		RunE: func(cmd *cobra.Command, args []string) error {
			prof, w, err := resolveContext()
			if err != nil {
				return err
			}
			if prof.APIKey != "" {
				w.Info("%s 가 설정돼 있습니다. 이 환경에서는 로그인이 필요 없습니다.", config.EnvAPIKey)
				return nil
			}
			return runLogin(cmd, prof, w, scopes, noBrowser)
		},
	}
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "브라우저를 열지 않고 URL 만 출력한다")
	cmd.Flags().StringSliceVarP(&scopes, "scope", "s", nil, "추가로 요청할 스코프 (예: write:calls)")
	return cmd
}

func runLogin(cmd *cobra.Command, prof *config.Profile, w *output.Writer, extraScopes []string, noBrowser bool) error {
	if !noBrowser && !canOpenBrowser() {
		w.Info("브라우저를 열 수 있는 환경이 아닌 것 같습니다. URL 을 출력합니다.")
		noBrowser = true
	}

	tok, err := oauth.Login(cmd.Context(), oauth.LoginOptions{
		Issuer:      prof.Issuer,
		ClientID:    config.CLIClientID,
		ExtraScopes: extraScopes,
		NoBrowser:   noBrowser,
		Notify:      w.Info,
	})
	if err != nil {
		return fmt.Errorf("%w\n\n%s", err, headlessHint())
	}

	if err := config.SaveToken(prof.Name, tok); err != nil {
		return err
	}
	if tok.AccountID != "" {
		prof.AccountID = tok.AccountID
	}
	if err := config.SaveProfile(prof); err != nil {
		return err
	}

	who := tok.AccountID
	if tok.Email != "" {
		who = fmt.Sprintf("%s (%s)", tok.AccountID, tok.Email)
	}
	w.Info("로그인 완료 — %s · 프로필 %q", strings.TrimSpace(who), prof.Name)
	if !hasWriteScope(tok.Scopes) {
		w.Info("읽기 권한만 받았습니다. 발신·발송이 필요하면:\n" +
			"  clawops auth refresh -s write:calls -s write:messages")
	}
	return nil
}

func newAuthRefreshCmd() *cobra.Command {
	var (
		scopes    []string
		noBrowser bool
	)
	cmd := &cobra.Command{
		Use:   "refresh",
		Short: "스코프를 추가한다",
		Long: "이미 로그인한 프로필에 스코프를 추가한다.\n\n" +
			"발신·발송 권한(write:calls, write:messages)은 실제로 요금이 발생하므로\n" +
			"기본 로그인에 포함하지 않는다. 스코프 승격은 서버의 재인가가 필요해서\n" +
			"브라우저를 다시 연다.",
		Example: "  clawops auth refresh -s write:messages",
		RunE: func(cmd *cobra.Command, args []string) error {
			prof, w, err := resolveContext()
			if err != nil {
				return err
			}
			if prof.APIKey != "" {
				w.Info("%s 를 쓰는 중입니다. 스코프는 API 키에 묶여 있습니다.", config.EnvAPIKey)
				return nil
			}
			if len(scopes) == 0 {
				return errors.New("추가할 스코프를 -s 로 지정하세요 (예: -s write:messages)")
			}
			// 기존에 받은 스코프를 유지한 채 새 것을 더한다.
			if cur, err := config.LoadToken(prof.Name); err == nil {
				scopes = append(scopes, cur.Scopes...)
			}
			return runLogin(cmd, prof, w, scopes, noBrowser)
		},
	}
	cmd.Flags().StringSliceVarP(&scopes, "scope", "s", nil, "추가할 스코프")
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "브라우저를 열지 않고 URL 만 출력한다")
	return cmd
}

func newAuthLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "토큰을 폐기하고 로컬에서 지운다",
		Long:  "서버의 grant 를 revoke 하고(RFC 7009) 저장된 토큰을 삭제한다.",
		RunE: func(cmd *cobra.Command, args []string) error {
			prof, w, err := resolveContext()
			if err != nil {
				return err
			}
			tok, err := config.LoadToken(prof.Name)
			if errors.Is(err, config.ErrNoToken) {
				w.Info("프로필 %q 에 저장된 자격증명이 없습니다.", prof.Name)
				return nil
			}
			if err != nil {
				return err
			}

			// refresh token 을 폐기하면 grant 가 통째로 죽는다.
			// 서버 폐기가 실패해도 로컬 삭제는 계속한다 — 로그아웃이 서버
			// 상태 때문에 막히면 안 된다.
			if tok.RefreshToken != "" {
				if err := oauth.Revoke(cmd.Context(), prof.Issuer, tok.RefreshToken, "refresh_token"); err != nil {
					w.Info("서버 폐기에 실패했습니다 (%v). 로컬 자격증명은 삭제합니다.", err)
				}
			}
			if err := config.DeleteToken(prof.Name); err != nil {
				return err
			}
			w.Info("로그아웃했습니다 — 프로필 %q", prof.Name)
			return nil
		},
	}
}

// authStatus 는 --json 출력의 형태다.
type authStatus struct {
	Profile   string   `json:"profile"`
	Method    string   `json:"method"` // oauth | api_key | none
	Issuer    string   `json:"issuer"`
	AccountID string   `json:"account_id,omitempty"`
	Email     string   `json:"email,omitempty"`
	Scopes    []string `json:"scopes,omitempty"`
	ExpiresAt string   `json:"expires_at,omitempty"`
	Expired   bool     `json:"expired"`
	// refresh token 이 없으면 access token 만료 = 재로그인이다. 겉으로는 정상
	// 로그인과 구분되지 않아서 만료 시점에야 드러나므로 상태에 노출한다.
	HasRefreshToken bool `json:"has_refresh_token"`
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

			st := authStatus{Profile: prof.Name, Issuer: prof.Issuer, Method: "none"}
			switch {
			case prof.APIKey != "":
				st.Method = "api_key"
				st.AccountID = prof.AccountID
			default:
				tok, err := config.LoadToken(prof.Name)
				if err == nil {
					st.Method = "oauth"
					st.AccountID = tok.AccountID
					st.Email = tok.Email
					st.Scopes = tok.Scopes
					st.Expired = tok.Expired()
					st.HasRefreshToken = tok.RefreshToken != ""
					if !tok.Expiry.IsZero() {
						st.ExpiresAt = tok.Expiry.Format(time.RFC3339)
					}
				} else if !errors.Is(err, config.ErrNoToken) {
					return err
				}
			}

			err = w.Data(st, func(out io.Writer) error {
				pairs := [][2]string{
					{"프로필", st.Profile},
					{"issuer", st.Issuer},
				}
				switch st.Method {
				case "api_key":
					pairs = append(pairs, [2]string{"인증", "API 키 (" + config.EnvAPIKey + ")"})
				case "oauth":
					pairs = append(pairs, [2]string{"인증", "브라우저 로그인"})
					if st.AccountID != "" {
						pairs = append(pairs, [2]string{"계정", st.AccountID})
					}
					if st.Email != "" {
						pairs = append(pairs, [2]string{"사용자", st.Email})
					}
					if st.ExpiresAt != "" {
						// 만료됐을 때 무슨 일이 일어나는지는 아래 "자동 갱신" 줄이
						// 말한다 — 여기서 같이 설명하면 두 줄이 같은 말을 한다.
						state := "유효"
						if st.Expired {
							state = "만료"
						}
						pairs = append(pairs, [2]string{"만료", fmt.Sprintf("%s (%s)", st.ExpiresAt, state)})
					}
					if st.HasRefreshToken {
						pairs = append(pairs, [2]string{"자동 갱신", "가능"})
					} else {
						pairs = append(pairs, [2]string{"자동 갱신", "불가 — 만료 시 재로그인"})
					}
					if len(st.Scopes) > 0 {
						pairs = append(pairs, [2]string{"스코프", summarizeScopes(st.Scopes)})
					}
				default:
					pairs = append(pairs, [2]string{"인증", "안 됨 — `clawops auth login`"})
				}
				return output.KV(out, pairs)
			})
			if err != nil {
				return err
			}
			if st.Method == "none" && !w.IsJSON() {
				return &ExitError{Code: 1}
			}
			return nil
		},
	}
}

// summarizeScopes 는 read:* 16 개를 그대로 늘어놓지 않고 요약한다.
func summarizeScopes(scopes []string) string {
	var reads, writes, others int
	var writeList []string
	for _, s := range scopes {
		switch {
		case strings.HasPrefix(s, "read:"):
			reads++
		case strings.HasPrefix(s, "write:"):
			writes++
			writeList = append(writeList, s)
		default:
			others++
		}
	}
	parts := []string{fmt.Sprintf("read %d개", reads)}
	if writes > 0 {
		parts = append(parts, strings.Join(writeList, ", "))
	} else {
		parts = append(parts, "write 없음")
	}
	return strings.Join(parts, " · ")
}

func hasWriteScope(scopes []string) bool {
	for _, s := range scopes {
		if strings.HasPrefix(s, "write:") {
			return true
		}
	}
	return false
}

// canOpenBrowser 는 브라우저를 열 수 있는 환경인지 추정한다.
//
// 원격 셸에서는 브라우저를 열어도 콜백이 원격 머신의 127.0.0.1 로 가므로
// 애초에 URL 을 출력해 주는 편이 낫다.
func canOpenBrowser() bool {
	if os.Getenv("SSH_CONNECTION") != "" || os.Getenv("SSH_TTY") != "" {
		return false
	}
	if runtime.GOOS == "linux" && os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		return false
	}
	return true
}

func headlessHint() string {
	return "브라우저를 쓸 수 없는 환경이라면 API 키로 인증하세요:\n" +
		"  로컬에서  clawops api-keys create --name ci\n" +
		"  또는      https://platform.claw-ops.com/settings/api-keys\n" +
		"  export CLAWOPS_API_KEY=sk_..."
}
