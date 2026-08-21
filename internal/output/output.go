// Package output 은 결과를 사람용 표 또는 JSON 으로 낸다.
//
// --json 은 CLI 가 SDK/MCP/대시보드와 겹치지 않는 유일한 영역이다 (파이프).
// 따라서 JSON 출력은 "부가 기능" 이 아니라 1급 계약으로 다룬다:
//   - JSON 모드에서는 진행 표시·색·안내문이 stdout 에 절대 섞이지 않는다.
//   - 사람용 메시지는 전부 stderr 로 간다.
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
)

type Format string

const (
	FormatTable Format = "table"
	FormatJSON  Format = "json"
)

type Options struct {
	NoColor bool
	Quiet   bool
}

type Writer struct {
	format Format
	opts   Options
	out    io.Writer // stdout — 데이터 전용
	err    io.Writer // stderr — 사람용 메시지 전용
}

func New(format string, opts Options) (*Writer, error) {
	f := Format(strings.ToLower(strings.TrimSpace(format)))
	switch f {
	case FormatTable, FormatJSON:
	default:
		return nil, fmt.Errorf("알 수 없는 출력 형식 %q (table 또는 json)", format)
	}
	return &Writer{format: f, opts: opts, out: os.Stdout, err: os.Stderr}, nil
}

func (w *Writer) IsJSON() bool { return w.format == FormatJSON }

// Data 는 결과를 stdout 으로 낸다.
// table 모드에서는 renderTable 이, json 모드에서는 v 가 그대로 쓰인다.
func (w *Writer) Data(v any, renderTable func(io.Writer) error) error {
	if w.format == FormatJSON {
		enc := json.NewEncoder(w.out)
		enc.SetIndent("", "  ")
		return enc.Encode(v)
	}
	if renderTable == nil {
		return nil
	}
	tw := tabwriter.NewWriter(w.out, 0, 4, 2, ' ', 0)
	if err := renderTable(tw); err != nil {
		return err
	}
	return tw.Flush()
}

// Info 는 사람용 안내를 stderr 로 낸다. JSON 파이프를 오염시키지 않는다.
func (w *Writer) Info(format string, args ...any) {
	if w.opts.Quiet {
		return
	}
	fmt.Fprintf(w.err, format+"\n", args...)
}
