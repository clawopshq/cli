// Command clawops is the ClawOps command-line interface.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/learners-superpumped/clawops-cli/internal/cli"
)

// version 은 goreleaser 가 -ldflags 로 주입한다.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := cli.Execute(ctx, cli.BuildInfo{Version: version, Commit: commit, Date: date}); err != nil {
		// cli.ExitError 는 종료 코드를 직접 지정한다 (예: --watch 실패 = 1).
		var ee *cli.ExitError
		if errors.As(err, &ee) {
			if ee.Message != "" {
				fmt.Fprintln(os.Stderr, "clawops:", ee.Message)
			}
			os.Exit(ee.Code)
		}
		fmt.Fprintln(os.Stderr, "clawops:", err)
		os.Exit(1)
	}
}
