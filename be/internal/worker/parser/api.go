package parser

import (
	"context"
	"errors"
)

// ErrPermanent 标记无需重试的文档格式或内容错误。
var ErrPermanent = errors.New("permanent parse error")

var errPermanent = ErrPermanent

// ErrPDFEmpty 表示 PDF 无法提取有效文本或扫描图。
var ErrPDFEmpty = errPDFEmpty

// TextAtoms 把正文转换为解析原子。
func TextAtoms(text string) []Atom {
	return textAtoms(text)
}

// TableAtom 创建表格解析原子。
func TableAtom(text string) Atom {
	return tableAtom(text)
}

// JoinTextAtoms 拼接解析原子中的文本内容。
func JoinTextAtoms(atoms []Atom, separator string) string {
	return joinTextAtoms(atoms, separator)
}

// ParseDocx 解析 DOCX 文档。
func ParseDocx(data []byte) ([]Atom, error) {
	return parseDocx(data)
}

// ParseMarkdown 解析 Markdown 文档。
func ParseMarkdown(text string) []Atom {
	return extractMarkdownAtoms(text)
}

// ParsePDF 解析 PDF 文档的本地文本层与内嵌图片。
func ParsePDF(data []byte) ([]Atom, error) {
	return parsePDF(data)
}

// HasPDFImages 判断 PDF 是否包含可识别的内嵌图片。
func HasPDFImages(data []byte) bool {
	return len(scanPDFImages(data)) > 0
}

// AttachPDFFormulaScreenshots 根据公式坐标补充 PDF 页面截图。
func AttachPDFFormulaScreenshots(ctx context.Context, data []byte, atoms []Atom) []Atom {
	return attachPDFFormulaScreenshots(ctx, data, atoms)
}

// ParseXlsx 解析 XLSX 工作簿。
func ParseXlsx(data []byte) ([]Atom, error) {
	return parseXlsx(data)
}

// SplitTableKeepHeader 拆分超长表格并在每段保留表头。
func SplitTableKeepHeader(text string, target int) []string {
	return splitTableKeepHeader(text, target)
}

// MarkdownTableCells 解析 Markdown 表格行的单元格。
func MarkdownTableCells(line string) []string {
	return markdownTableCells(line)
}

// IsMarkdownTableSeparator 判断一行是否为 Markdown 表头分隔线。
func IsMarkdownTableSeparator(line string) bool {
	return isMarkdownTableSeparator(line)
}
