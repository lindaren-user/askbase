package worker

import (
	"strings"
	"testing"

	"askbase/be/internal/model"

	"github.com/xuri/excelize/v2"
)

func TestParseQADelimitedKeepsContinuationInAnswer(t *testing.T) {
	atoms, err := parseQADelimited("问题一\t答案第一行\n答案第二行\n问题二\t答案二", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(atoms) != 2 {
		t.Fatalf("atoms=%d: %#v", len(atoms), atoms)
	}
	if atoms[0].QAQuestion != "问题一" || atoms[0].QAAnswer != "答案第一行\n答案第二行" {
		t.Fatalf("unexpected first pair: %#v", atoms[0])
	}
}

func TestParseQAXlsxUsesFirstTwoNonEmptyCells(t *testing.T) {
	book := excelize.NewFile()
	defer func() { _ = book.Close() }()
	_ = book.SetSheetRow("Sheet1", "A1", &[]any{"问题一", "", "答案一", "忽略"})
	_ = book.SetSheetRow("Sheet1", "A2", &[]any{"问题二", "答案二"})
	buffer, err := book.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}

	atoms, err := parseQAXlsx(buffer.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(atoms) != 2 || atoms[0].QAQuestion != "问题一" || atoms[0].QAAnswer != "答案一" {
		t.Fatalf("unexpected atoms: %#v", atoms)
	}
}

func TestMaterializeQAChunksUsesHeadingPathAndEmbedsQuestion(t *testing.T) {
	doc := model.Document{ID: 1, DatasetID: 2, Name: "faq.md"}
	atoms := extractMarkdownAtoms("# 账户\n账户相关说明\n## 如何重置密码？\n进入设置页面重置。\n## 如何修改邮箱？\n联系管理员。")

	chunks, embeds, err := materializeQAChunks(doc, atoms)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 3 || len(embeds) != 3 {
		t.Fatalf("chunks=%d embeds=%d", len(chunks), len(embeds))
	}
	if !strings.Contains(chunks[1].Content, "问题：账户\n如何重置密码？") || !strings.Contains(chunks[1].Content, "回答：进入设置页面重置。") {
		t.Fatalf("unexpected nested QA chunk: %q", chunks[1].Content)
	}
	if strings.Contains(embeds[1], "进入设置页面") || !strings.Contains(embeds[1], "如何重置密码") {
		t.Fatalf("embedding should emphasize question only: %q", embeds[1])
	}
}

func TestMaterializeQAChunksRecognizesPDFQuestionBullets(t *testing.T) {
	doc := model.Document{ID: 1, DatasetID: 2, Name: "faq.pdf"}
	atoms := []parseAtom{{Text: "1. 如何退款\n提交退款申请。\n2. 多久到账\n通常三个工作日。", Page: 1, Parser: "mineru-vlm"}}

	chunks, _, err := materializeQAChunks(doc, atoms)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 2 || !strings.Contains(chunks[1].Content, "问题：多久到账") {
		t.Fatalf("unexpected chunks: %#v", chunks)
	}
}
