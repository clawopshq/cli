package output

import (
	"fmt"
	"io"
	"strings"

	"github.com/mattn/go-runewidth"
)

// text/tabwriter 는 rune 개수로 폭을 재기 때문에 한글(표시 폭 2)이 섞이면
// 열이 어긋난다. 표시 폭 기준으로 직접 맞춘다.

// KV 는 레이블/값 쌍을 정렬해 출력한다.
func KV(out io.Writer, pairs [][2]string) error {
	width := 0
	for _, p := range pairs {
		if w := runewidth.StringWidth(p[0]); w > width {
			width = w
		}
	}
	for _, p := range pairs {
		pad := strings.Repeat(" ", width-runewidth.StringWidth(p[0])+2)
		if _, err := fmt.Fprintf(out, "%s%s%s\n", p[0], pad, p[1]); err != nil {
			return err
		}
	}
	return nil
}

// Table 은 헤더와 행들을 정렬해 출력한다.
func Table(out io.Writer, headers []string, rows [][]string) error {
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = runewidth.StringWidth(h)
	}
	for _, row := range rows {
		for i, cell := range row {
			if i < len(widths) {
				if w := runewidth.StringWidth(cell); w > widths[i] {
					widths[i] = w
				}
			}
		}
	}

	writeRow := func(cells []string) error {
		var b strings.Builder
		for i, cell := range cells {
			b.WriteString(cell)
			// 마지막 열은 오른쪽을 채우지 않는다 — 잘라 쓰기 좋게.
			if i < len(cells)-1 && i < len(widths) {
				b.WriteString(strings.Repeat(" ", widths[i]-runewidth.StringWidth(cell)+2))
			}
		}
		_, err := fmt.Fprintln(out, b.String())
		return err
	}

	if err := writeRow(headers); err != nil {
		return err
	}
	for _, row := range rows {
		if err := writeRow(row); err != nil {
			return err
		}
	}
	return nil
}
