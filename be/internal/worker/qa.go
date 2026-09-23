package worker

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"path"
	"regexp"
	"strings"

	"askbase/be/internal/model"

	"github.com/xuri/excelize/v2"
)

var (
	qaQuestionPrefixPattern = regexp.MustCompile(`(?i)^\s*(?:问题|问|q|question)\s*[:：]\s*(.+)$`)
	qaAnswerPrefixPattern   = regexp.MustCompile(`(?i)^\s*(?:答案|回答|答|a|answer)\s*[:：]\s*(.*)$`)
	qaBulletQuestionPattern = regexp.MustCompile(`^\s*(?:\d{1,4}[.、）)]|[-*•])\s*(.+)$`)
	qaPrefixPattern         = regexp.MustCompile(`(?i)^\s*(?:问题|答案|回答|问|答|q|a|question|answer)\s*[:：\t ]+`)
)

// qaPair 保存一组已经确定边界的问题与答案。
type qaPair struct {
	question string
	answer   string
	page     int
	parser   string
}

// parseQADelimited 按 RAGFlow QA 约定从 TXT/CSV 提取两列问答，异常行续接到上一答案。
func parseQADelimited(text string, csvMode bool) ([]parseAtom, error) {
	delimiter := detectQADelimiter(text)
	reader := csv.NewReader(strings.NewReader(text))
	reader.Comma = delimiter
	reader.FieldsPerRecord = -1
	reader.LazyQuotes = true

	var atoms []parseAtom
	var question, answer string
	flush := func() {
		question = cleanQAField(question)
		answer = cleanQAField(answer)
		if question != "" && answer != "" {
			atoms = append(atoms, qaPairAtom(question, answer, "qa-delimited"))
		}
		question, answer = "", ""
	}
	for {
		record, err := reader.Read()
		if err != nil {
			break
		}
		if len(record) == 2 && strings.TrimSpace(record[0]) != "" && strings.TrimSpace(record[1]) != "" {
			flush()
			question, answer = record[0], record[1]
			continue
		}
		if question != "" {
			continuation := strings.TrimSpace(strings.Join(record, string(delimiter)))
			if continuation != "" {
				answer += "\n" + continuation
			}
		}
	}
	flush()
	if len(atoms) == 0 {
		kind := "TXT"
		if csvMode {
			kind = "CSV"
		}
		return nil, fmt.Errorf("%w: %s 中没有有效的两列问答数据", errPermanent, kind)
	}
	return atoms, nil
}

// detectQADelimiter 在制表符和逗号中选择更符合两列问答的分隔符。
func detectQADelimiter(text string) rune {
	var commaRows, tabRows int
	for _, line := range strings.Split(text, "\n") {
		if len(strings.Split(line, ",")) == 2 {
			commaRows++
		}
		if len(strings.Split(line, "\t")) == 2 {
			tabRows++
		}
	}
	if tabRows >= commaRows {
		return '\t'
	}
	return ','
}

// parseQAXlsx 按工作表逐行读取前两个非空单元格，每行生成一组问答。
func parseQAXlsx(data []byte) ([]parseAtom, error) {
	book, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%w: XLSX 解析失败: %v", errPermanent, err)
	}
	defer func() { _ = book.Close() }()
	var atoms []parseAtom
	for _, sheet := range book.GetSheetList() {
		rows, err := book.GetRows(sheet)
		if err != nil {
			continue
		}
		for _, row := range rows {
			values := firstQACells(row)
			if len(values) == 2 {
				atoms = append(atoms, qaPairAtom(values[0], values[1], "qa-xlsx"))
			}
		}
	}
	if len(atoms) == 0 {
		return nil, fmt.Errorf("%w: XLSX 中没有有效的两列问答数据", errPermanent)
	}
	return atoms, nil
}

// firstQACells 返回一行中的前两个非空单元格。
func firstQACells(row []string) []string {
	values := make([]string, 0, 2)
	for _, cell := range row {
		cell = strings.TrimSpace(cell)
		if cell == "" {
			continue
		}
		values = append(values, cell)
		if len(values) == 2 {
			break
		}
	}
	return values
}

// materializeQAChunks 保证每组问答独立成块，并仅用问题文本生成检索向量。
func materializeQAChunks(doc model.Document, atoms []parseAtom) ([]model.Chunk, []string, error) {
	pairs := qaPairsFromParsedAtoms(atoms)
	ext := strings.ToLower(path.Ext(doc.Name))
	switch ext {
	case ".md", ".markdown":
		pairs = append(pairs, qaPairsFromHeadingAtoms(atoms, false)...)
	case ".docx":
		pairs = append(pairs, qaPairsFromHeadingAtoms(atoms, true)...)
	case ".pdf":
		pairs = append(pairs, qaPairsFromQuestionLines(atoms)...)
	default:
		if len(pairs) == 0 {
			pairs = append(pairs, qaPairsFromQuestionLines(atoms)...)
		}
	}
	if len(pairs) == 0 {
		return nil, nil, fmt.Errorf("%w: 未识别到有效问答对", errPermanent)
	}

	chunks := make([]model.Chunk, 0, len(pairs))
	embedTexts := make([]string, 0, len(pairs))
	for _, pair := range pairs {
		question := cleanQAField(pair.question)
		answer := cleanQAField(pair.answer)
		if question == "" || answer == "" {
			continue
		}
		content := "问题：" + question + "\n\n回答：" + answer
		parser := pair.parser
		if parser == "" {
			parser = "qa-structured"
		}
		chunk := newElementChunk(doc, len(chunks), content, "", parseAtom{
			ElementType: "text",
			Page:        pair.page,
			Parser:      parser,
		})
		chunks = append(chunks, chunk)
		embedTexts = append(embedTexts, model.EmbedText(doc.Name, question))
	}
	if len(chunks) == 0 {
		return nil, nil, fmt.Errorf("%w: 问答对的问题或答案为空", errPermanent)
	}
	return chunks, embedTexts, nil
}

// qaPairsFromParsedAtoms 读取 TXT/CSV/XLSX 已确定边界的问答原子。
func qaPairsFromParsedAtoms(atoms []parseAtom) []qaPair {
	var pairs []qaPair
	for _, atom := range atoms {
		if atom.QAQuestion == "" && atom.QAAnswer == "" {
			continue
		}
		pairs = append(pairs, qaPair{question: atom.QAQuestion, answer: atom.QAAnswer, page: atom.Page, parser: atom.Parser})
	}
	return pairs
}

// qaPairsFromHeadingAtoms 将 Markdown/DOCX 标题路径作为问题，标题正文作为答案。
func qaPairsFromHeadingAtoms(atoms []parseAtom, tableAsPairs bool) []qaPair {
	var pairs []qaPair
	var stack []string
	var levels []int
	var answer []string
	inFence := false
	flush := func() {
		question := strings.TrimSpace(strings.Join(stack, "\n"))
		body := strings.TrimSpace(strings.Join(answer, "\n"))
		if question != "" && body != "" {
			pairs = append(pairs, qaPair{question: question, answer: body, parser: "qa-heading"})
		}
		answer = nil
	}
	pushHeading := func(level int, title string) {
		flush()
		for len(levels) > 0 && level <= levels[len(levels)-1] {
			levels = levels[:len(levels)-1]
			stack = stack[:len(stack)-1]
		}
		levels = append(levels, level)
		stack = append(stack, strings.TrimSpace(title))
	}

	for _, atom := range atoms {
		if atom.QAQuestion != "" {
			continue
		}
		if len(atom.Image) > 0 {
			// TODO: 使用视觉模型解析内嵌图片，并将识别结果追加到当前问题的答案中。
			continue
		}
		if atom.Table {
			if tableAsPairs {
				flush()
				pairs = append(pairs, qaPairsFromMarkdownTable(atom.Text)...)
			} else if len(stack) > 0 {
				answer = append(answer, atom.Text)
			}
			continue
		}
		if atom.ElementType == "title" && atom.HeadingLevel > 0 {
			pushHeading(atom.HeadingLevel, atom.Text)
			continue
		}
		for _, line := range strings.Split(strings.ReplaceAll(atom.Text, "\r\n", "\n"), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "```") {
				inFence = !inFence
			}
			if !inFence {
				if match := markdownHeadingPattern.FindStringSubmatch(line); len(match) == 3 {
					pushHeading(len(match[1]), match[2])
					continue
				}
			}
			if len(stack) > 0 {
				answer = append(answer, line)
			}
		}
	}
	flush()
	return pairs
}

// qaPairsFromMarkdownTable 将表格每行的前两个非空单元格解释为问题和答案。
func qaPairsFromMarkdownTable(text string) []qaPair {
	var pairs []qaPair
	for _, line := range strings.Split(text, "\n") {
		if isMarkdownTableSeparator(line) {
			continue
		}
		cells := firstQACells(markdownTableCells(line))
		if len(cells) == 2 {
			pairs = append(pairs, qaPair{question: cells[0], answer: cells[1], parser: "qa-table"})
		}
	}
	return pairs
}

// qaPairsFromQuestionLines 从 PDF 等线性文本中识别问题前缀、答案前缀和编号问句。
// TODO: 优化 QA PDF 识别，结合版面结构区分问题与答案中的编号/项目符号，并支持从表格、图片中提取问答。
func qaPairsFromQuestionLines(atoms []parseAtom) []qaPair {
	var pairs []qaPair
	var question string
	var answer []string
	page := 0
	flush := func() {
		body := strings.TrimSpace(strings.Join(answer, "\n"))
		if strings.TrimSpace(question) != "" && body != "" {
			pairs = append(pairs, qaPair{question: question, answer: body, page: page, parser: "qa-bullets"})
		}
		question, answer, page = "", nil, 0
	}
	for _, atom := range atoms {
		if atom.QAQuestion != "" || len(atom.Image) > 0 || atom.Table {
			continue
		}
		for _, raw := range strings.Split(strings.ReplaceAll(atom.Text, "\r\n", "\n"), "\n") {
			line := strings.TrimSpace(raw)
			if line == "" {
				continue
			}
			if match := qaQuestionPrefixPattern.FindStringSubmatch(line); len(match) == 2 {
				flush()
				question, page = match[1], atom.Page
				continue
			}
			if match := qaBulletQuestionPattern.FindStringSubmatch(line); len(match) == 2 {
				flush()
				question, page = match[1], atom.Page
				continue
			}
			if strings.HasSuffix(line, "?") || strings.HasSuffix(line, "？") {
				flush()
				question, page = line, atom.Page
				continue
			}
			if match := qaAnswerPrefixPattern.FindStringSubmatch(line); len(match) == 2 {
				line = match[1]
			}
			if question != "" && line != "" {
				answer = append(answer, line)
			}
		}
	}
	flush()
	return pairs
}

// qaPairAtom 将已确定边界的问答保存为解析原子，交由统一物化阶段处理。
func qaPairAtom(question, answer, parser string) parseAtom {
	return parseAtom{QAQuestion: question, QAAnswer: answer, ElementType: "text", Parser: parser}
}

// cleanQAField 清理字段两端空白和可选的问答标签前缀。
func cleanQAField(value string) string {
	return strings.TrimSpace(qaPrefixPattern.ReplaceAllString(strings.TrimSpace(value), ""))
}
