package parser

import "strings"

func markdownTable(header []string, rows [][]string) string {
	if len(header) == 0 && len(rows) == 0 {
		return ""
	}
	cols := len(header)
	for _, row := range rows {
		if len(row) > cols {
			cols = len(row)
		}
	}
	if cols == 0 {
		return ""
	}
	pad := func(cells []string) []string {
		out := make([]string, cols)
		for i := 0; i < cols; i++ {
			if i < len(cells) {
				out[i] = escapeMDCell(cells[i])
			}
		}
		return out
	}
	head := pad(header)
	var b strings.Builder
	b.WriteString("| ")
	b.WriteString(strings.Join(head, " | "))
	b.WriteString(" |\n| ")
	seps := make([]string, cols)
	for i := range seps {
		seps[i] = "---"
	}
	b.WriteString(strings.Join(seps, " | "))
	b.WriteString(" |")
	for _, row := range rows {
		b.WriteString("\n| ")
		b.WriteString(strings.Join(pad(row), " | "))
		b.WriteString(" |")
	}
	return b.String()
}

func escapeMDCell(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\n", "<br>")
	s = strings.ReplaceAll(s, "|", "\\|")
	return strings.TrimSpace(s)
}
