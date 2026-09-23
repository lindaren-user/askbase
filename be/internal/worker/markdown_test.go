package worker

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"
	"unicode/utf8"

	"askbase/be/internal/model"
)

func TestExtractMarkdownAtomsKeepsPipeTableAtomic(t *testing.T) {
	src := "前言\n\n| 列A | 列B |\n| --- | --- |\n| 1 | 2 |\n| 3 | 4 |\n\n后记\n"
	atoms := extractMarkdownAtoms(src)
	if len(atoms) != 3 {
		t.Fatalf("atoms=%d %#v", len(atoms), atoms)
	}
	if atoms[0].Table || !strings.Contains(atoms[0].Text, "前言") {
		t.Fatalf("preface=%#v", atoms[0])
	}
	if !atoms[1].Table {
		t.Fatalf("table not atomic: %#v", atoms[1])
	}
	if !strings.Contains(atoms[1].Text, "| 列A | 列B |") || !strings.Contains(atoms[1].Text, "| 3 | 4 |") {
		t.Fatalf("table=%q", atoms[1].Text)
	}
	if atoms[2].Table || !strings.Contains(atoms[2].Text, "后记") {
		t.Fatalf("suffix=%#v", atoms[2])
	}
	if n := len(splitText(atoms[1].Text)); n != 1 {
		t.Fatalf("small table should fit one text window, got %d", n)
	}
	kept := splitTableKeepHeader(atoms[1].Text, chunkTargetRunes)
	if len(kept) != 1 || kept[0] != atoms[1].Text {
		t.Fatalf("keep=%#v", kept)
	}
}

func TestExtractMarkdownAtomsSkipsFencedTable(t *testing.T) {
	src := "```\n| A | B |\n| --- | --- |\n| 1 | 2 |\n```\n"
	atoms := extractMarkdownAtoms(src)
	if len(atoms) != 1 || atoms[0].Table {
		t.Fatalf("want one prose atom, got %#v", atoms)
	}
	if !strings.Contains(atoms[0].Text, "| A | B |") {
		t.Fatalf("fence lost: %q", atoms[0].Text)
	}
}

func TestExtractMarkdownAtomsHTMLTable(t *testing.T) {
	src := "p\n<table><tr><td>x</td></tr></table>\nq\n"
	atoms := extractMarkdownAtoms(src)
	if len(atoms) != 3 || !atoms[1].Table {
		t.Fatalf("%#v", atoms)
	}
	if atoms[1].ElementType != "table" {
		t.Fatalf("elementType=%q", atoms[1].ElementType)
	}
}

func TestExtractMarkdownAtomsKeepsImageAtomic(t *testing.T) {
	src := "图片前文字 ![架构图](https://example.com/diagram.png) 图片后文字"
	atoms := extractMarkdownAtoms(src)
	if len(atoms) != 3 {
		t.Fatalf("atoms=%d %#v", len(atoms), atoms)
	}
	if atoms[1].ElementType != "image" || atoms[1].Text != "![架构图](https://example.com/diagram.png)" {
		t.Fatalf("image=%#v", atoms[1])
	}
}

func TestExtractMarkdownAtomsDecodesDataImage(t *testing.T) {
	payload := tinyPNG()
	src := "![示意图](data:image/png;base64," + base64.StdEncoding.EncodeToString(payload) + ")"
	atoms := extractMarkdownAtoms(src)
	if len(atoms) != 1 {
		t.Fatalf("atoms=%d %#v", len(atoms), atoms)
	}
	if atoms[0].ElementType != "image" || atoms[0].Text != "示意图" {
		t.Fatalf("image=%#v", atoms[0])
	}
	if atoms[0].Ext != ".png" || atoms[0].Parser != "markdown" || string(atoms[0].Image) != string(payload) {
		t.Fatalf("decoded image=%#v", atoms[0])
	}
}

func TestSplitTableKeepHeaderRetainsHeader(t *testing.T) {
	header := "| H1 | H2 |\n| --- | --- |"
	var b strings.Builder
	b.WriteString(header)
	for i := 0; i < 40; i++ {
		b.WriteString("\n| ")
		b.WriteString(strings.Repeat("单元格内容", 8))
		b.WriteString(" | y |")
	}
	parts := splitTableKeepHeader(b.String(), 80)
	if len(parts) < 2 {
		t.Fatalf("want split, got %d size=%d", len(parts), utf8.RuneCountInString(b.String()))
	}
	for i, p := range parts {
		if !strings.HasPrefix(p, header+"\n") {
			t.Fatalf("part %d missing header: %q", i, p)
		}
	}
}

func TestSplitHTMLTableKeepHeaderRetainsCompleteRows(t *testing.T) {
	table := `<table class="report"><caption>季度数据</caption><tr><th>季度</th><th>收入</th></tr>` +
		`<tr><td>第一季度</td><td>100</td></tr><tr><td>第二季度</td><td>200</td></tr>` +
		`<tr><td>第三季度</td><td>300</td></tr></table>`
	parts := splitTableKeepHeader(table, 150)
	if len(parts) < 2 {
		t.Fatalf("want split, got %d: %#v", len(parts), parts)
	}
	for i, part := range parts {
		if !strings.HasPrefix(part, `<table class="report"><caption>季度数据</caption><tr><th>`) {
			t.Fatalf("part %d missing caption or header: %q", i, part)
		}
		if !strings.HasSuffix(part, "</table>") {
			t.Fatalf("part %d has broken closing tag: %q", i, part)
		}
		if strings.Count(part, "<tr") != strings.Count(part, "</tr>") {
			t.Fatalf("part %d has broken row: %q", i, part)
		}
	}
}

func TestSplitHTMLTableWithoutRowsStaysWhole(t *testing.T) {
	table := `<table><tbody>无法识别的表格内容</tbody></table>`
	parts := splitTableKeepHeader(table, 10)
	if len(parts) != 1 || parts[0] != table {
		t.Fatalf("parts=%#v", parts)
	}
}

func TestMaterializeChunksKeepsTableWhole(t *testing.T) {
	w := &Worker{}
	doc := model.Document{ID: 1, DatasetID: 2, Name: "doc.md"}
	table := "| 列A | 列B |\n| --- | --- |\n| 1 | 2 |\n| 3 | 4 |"
	prose := strings.Repeat("这是一段会超长的说明文字。", 40)
	atoms := []parseAtom{{Text: prose}, tableAtom(table), {Text: prose}}
	chunks, _, err := w.materializeChunks(context.Background(), doc, atoms, model.ChunkStrategyGeneral, nil)
	if err != nil {
		t.Fatal(err)
	}
	var found string
	for _, c := range chunks {
		if strings.Contains(c.Content, "| 列A | 列B |") {
			found = c.Content
			break
		}
	}
	if found == "" {
		t.Fatalf("table missing from %d chunks", len(chunks))
	}
	if !strings.Contains(found, "| 1 | 2 |") || !strings.Contains(found, "| 3 | 4 |") {
		t.Fatalf("table shredded: %q", found)
	}
	if strings.Contains(found, "说明文字") {
		t.Fatalf("table mixed with prose: %q", found)
	}
}
