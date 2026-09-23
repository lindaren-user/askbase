package worker

import (
	"context"
	"strings"
	"testing"

	"askbase/be/internal/llm"
	"askbase/be/internal/model"
)

func TestPrepareBookAtomsKeepsChapterHierarchy(t *testing.T) {
	atoms := []parseAtom{{Text: "第一章 起点\n开篇正文\n第一节 背景\n背景正文"}}

	got := prepareAtomsForStrategy(atoms, model.ChunkStrategyBook)
	if len(got) != 2 {
		t.Fatalf("got %d atoms, want 2: %#v", len(got), got)
	}
	if got[0].SectionPath != "第一章 起点" || got[0].Text != "开篇正文" {
		t.Fatalf("unexpected chapter atom: %#v", got[0])
	}
	if got[1].SectionPath != "第一章 起点 / 第一节 背景" || got[1].Text != "背景正文" {
		t.Fatalf("unexpected section atom: %#v", got[1])
	}
}

func TestPreparePaperAtomsKeepsNumberedSections(t *testing.T) {
	atoms := []parseAtom{{Text: "Abstract\n摘要内容\n1 Introduction\n引言内容\n1.1 Background\n背景内容"}}

	got := prepareAtomsForStrategy(atoms, model.ChunkStrategyPaper)
	if len(got) != 3 {
		t.Fatalf("got %d atoms, want 3: %#v", len(got), got)
	}
	if got[2].SectionPath != "1 Introduction / 1.1 Background" {
		t.Fatalf("unexpected section path: %q", got[2].SectionPath)
	}
	if !got[0].NoSplit {
		t.Fatal("paper abstract should stay as one chunk")
	}
}

func TestShouldUseMinerUByRecipeAndContent(t *testing.T) {
	clean := []parseAtom{{Text: "clean text"}}
	scanned := []parseAtom{{Image: []byte("image"), NeedVision: true}}
	if shouldUseMinerU(model.ChunkStrategyResume, nil, clean, nil) {
		t.Fatal("clean resume should use local parser")
	}
	if !shouldUseMinerU(model.ChunkStrategyResume, nil, scanned, nil) {
		t.Fatal("scanned resume should upgrade to MinerU")
	}
	if !shouldUseMinerU(model.ChunkStrategyGeneral, nil, nil, errPDFEmpty) {
		t.Fatal("failed local parsing should upgrade to MinerU")
	}
	garbled := []parseAtom{{Text: "教育背景���项目经历���专业技能"}}
	if !shouldUseMinerU(model.ChunkStrategyResume, nil, garbled, nil) {
		t.Fatal("garbled resume should upgrade to MinerU")
	}
	bookWithImage := []byte("<< /Subtype /Image /Width 8 /Height 8 /Filter /DCTDecode /Length 4 >>\nstream\n\xff\xd8\xff\xd9\nendstream")
	if !shouldUseMinerU(model.ChunkStrategyBook, bookWithImage, clean, nil) {
		t.Fatal("book with embedded image should upgrade to MinerU")
	}
}

func TestResumeHeadingToleratesReplacementCharacter(t *testing.T) {
	level, title, ok := recipeHeading(model.ChunkStrategyResume, "教育背景�", "text")
	if !ok || level != 1 || title != "教育背景" {
		t.Fatalf("unexpected heading: level=%d title=%q ok=%v", level, title, ok)
	}
}

func TestPaperRequiresMinerU(t *testing.T) {
	w := &Worker{}
	doc := model.Document{ID: 1, Name: "paper.pdf"}

	_, err := w.parsePDFByStrategy(context.Background(), doc, nil, model.ChunkStrategyPaper)
	if err == nil || !strings.Contains(err.Error(), "论文模板需要启用 MinerU") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMaterializeStructuredChunksPrefixesSectionPath(t *testing.T) {
	w := &Worker{}
	doc := model.Document{ID: 10, DatasetID: 20, Name: "paper.pdf"}
	atoms := []parseAtom{{Text: "1 Introduction\n正文内容"}}

	chunks, embeds, err := w.materializeChunks(context.Background(), doc, atoms, model.ChunkStrategyPaper, nil)
	if err != nil {
		t.Fatalf("materializeChunks: %v", err)
	}
	if len(chunks) != 1 || len(embeds) != 1 {
		t.Fatalf("got %d chunks and %d embeds", len(chunks), len(embeds))
	}
	if chunks[0].Content != "1 Introduction\n\n正文内容" {
		t.Fatalf("unexpected content: %q", chunks[0].Content)
	}
}

func TestMaterializeResumeChunksGroupsFieldsAndDropsDecorativeImages(t *testing.T) {
	w := &Worker{}
	doc := model.Document{ID: 10, DatasetID: 20, Name: "张三简历.pdf"}
	atoms := []parseAtom{
		{Text: "张三\n求职意向：Golang 后端开发\n电话：13800000000\n邮箱：zhangsan@example.com", ElementType: "text", Parser: "mineru-vlm"},
		{Image: []byte("avatar"), ElementType: "image", Parser: "mineru-vlm"},
		{Text: "教育背景", ElementType: "title", Parser: "mineru-vlm"},
		{Text: "示例大学\n软件工程 本科", ElementType: "text", Parser: "mineru-vlm"},
		{Text: "专业技能", ElementType: "title", Parser: "mineru-vlm"},
		{Text: "Go、PostgreSQL、Redis", ElementType: "text", Parser: "mineru-vlm"},
		{Text: "项目经历", ElementType: "title", Parser: "mineru-vlm"},
		{Text: "1. 二手交易平台", ElementType: "text", Parser: "mineru-vlm"},
		{Text: "技术栈：Go、Vue\n- 使用 Redis 缓存", ElementType: "text", Parser: "mineru-vlm"},
		{Text: "2. 考试系统", ElementType: "text", Parser: "mineru-vlm"},
		{Text: "技术栈：Go、Svelte\n- 使用异步任务队列", ElementType: "text", Parser: "mineru-vlm"},
	}

	chunks, embeds, err := w.materializeChunks(context.Background(), doc, atoms, model.ChunkStrategyResume, nil)
	if err != nil {
		t.Fatalf("materializeChunks: %v", err)
	}
	if len(chunks) != 5 || len(embeds) != 5 {
		t.Fatalf("got %d chunks and %d embeds: %#v", len(chunks), len(embeds), chunks)
	}
	if !strings.Contains(chunks[0].Content, "基本信息") || !strings.Contains(chunks[0].Content, "姓名:张三") {
		t.Fatalf("unexpected basic info chunk: %q", chunks[0].Content)
	}
	if !strings.Contains(chunks[3].Content, "二手交易平台") || !strings.Contains(chunks[3].Content, "Redis 缓存") {
		t.Fatalf("first project was not kept intact: %q", chunks[3].Content)
	}
	if !strings.Contains(chunks[4].Content, "考试系统") || !strings.Contains(chunks[4].Content, "异步任务队列") {
		t.Fatalf("second project was not kept intact: %q", chunks[4].Content)
	}
	for _, chunk := range chunks {
		if chunk.ElementType == "image" || strings.Contains(chunk.Content, "avatar") {
			t.Fatalf("decorative image leaked into resume chunks: %#v", chunk)
		}
		if !strings.HasPrefix(chunk.ParserName, "resume-structured") {
			t.Fatalf("unexpected parser name: %q", chunk.ParserName)
		}
	}
}

type resumeCompleterStub struct{}

func (resumeCompleterStub) Complete(_ context.Context, messages []llm.ChatMessage, _ int) (string, error) {
	prompt := messages[len(messages)-1].Content
	switch {
	case strings.Contains(prompt, "抽取基本信息"):
		return `{"identity":{"name":"张三","phone":"13800000000","email":"zhangsan@example.com","intent":"Golang 后端开发"},"basic":[0,1,2,3],"skills":[7],"other":[]}`, nil
	case strings.Contains(prompt, "抽取教育经历"):
		return `{"entries":[[5]]}`, nil
	case strings.Contains(prompt, "抽取工作或实习经历"):
		return `{"entries":[]}`, nil
	case strings.Contains(prompt, "抽取项目经历"):
		return `{"entries":[[9,10,11],[12,13,14]]}`, nil
	default:
		return `{}`, nil
	}
}

func TestMaterializeResumeChunksUsesLLMLineSelections(t *testing.T) {
	w := &Worker{resumeLLM: resumeCompleterStub{}}
	doc := model.Document{ID: 11, DatasetID: 20, Name: "张三简历.pdf"}
	atoms := []parseAtom{{
		Text: strings.Join([]string{
			"张三", "求职意向：Golang 后端开发", "电话：13800000000", "邮箱：zhangsan@example.com",
			"教育背景", "示例大学 软件工程 本科", "专业技能", "Go、PostgreSQL、Redis", "项目经历",
			"1. 二手交易平台", "技术栈：Go、Vue", "- 使用 Redis 缓存",
			"2. 考试系统", "技术栈：Go、Svelte", "- 使用异步任务队列",
		}, "\n"),
		ElementType: "text",
		Parser:      "mineru-vlm",
	}}

	chunks, _, err := w.materializeChunks(context.Background(), doc, atoms, model.ChunkStrategyResume, nil)
	if err != nil {
		t.Fatalf("materializeChunks: %v", err)
	}
	if len(chunks) != 5 {
		t.Fatalf("got %d chunks: %#v", len(chunks), chunks)
	}
	if !strings.Contains(chunks[0].Content, "[候选人摘要] 姓名:张三") {
		t.Fatalf("identity summary missing: %q", chunks[0].Content)
	}
	if !strings.Contains(chunks[3].Content, "Redis 缓存") || strings.Contains(chunks[3].Content, "考试系统") {
		t.Fatalf("project selections were not isolated: %q", chunks[3].Content)
	}
}
