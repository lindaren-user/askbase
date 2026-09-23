package parser

// Atom 文档解析原子：正文、表格或图片。
// 表格原子单独成块，不被递归分块按行切开。
type Atom struct {
	Text         string
	Image        []byte
	Ext          string
	Table        bool
	NeedVision   bool // PDF 扫描页等：无视觉模型时必须失败，不能静默跳过
	ElementType  string
	Page         int
	BBox         []float64
	Parser       string
	HeadingLevel int    // DOCX/Markdown 标题层级，QA 模板用它维护问题路径
	QAQuestion   string // 已由格式解析器确定的问题文本
	QAAnswer     string // 已由格式解析器确定的答案文本
	SectionPath  string // 领域模板识别出的章节路径，写入块正文补足召回上下文
	Boundary     bool   // 强制在该原子前后断块，避免跨章节合并
	NoSplit      bool   // 摘要等必须保持完整的领域原子不按长度二次切分
}

type parseAtom = Atom

// textAtoms 把整段正文收成一个原子；空串返回 nil。
func textAtoms(s string) []parseAtom {
	if s == "" {
		return nil
	}
	return []parseAtom{{Text: s}}
}

// tableAtom 表格原子，后续按表头切分而不是递归切行。
func tableAtom(s string) parseAtom {
	return parseAtom{Text: s, Table: true, ElementType: "table"}
}

// joinTextAtoms 拼接文本原子，跳过纯图；用于预览。
func joinTextAtoms(atoms []parseAtom, sep string) string {
	var b []byte
	for _, a := range atoms {
		if a.Text == "" {
			continue
		}
		if len(b) > 0 {
			b = append(b, sep...)
		}
		b = append(b, a.Text...)
	}
	return string(b)
}
