package parser

import (
	"encoding/base64"
	"regexp"
	"strings"
	"unicode/utf8"

	"askbase/be/internal/worker/chunker"
)

const markdownImageMaxBytes = 20 << 20

var (
	mdFenceOpen    = regexp.MustCompile("^[ \\t]{0,3}(`{3,}|~{3,})")
	mdSepCell      = regexp.MustCompile(`^:?-+:?$`)
	htmlTableOpen  = regexp.MustCompile(`(?i)<table\b`)
	htmlTableClose = regexp.MustCompile(`(?i)</table>`)
	markdownImage  = regexp.MustCompile(`(?i)!\[[^\]\n]*\]\([^\n)]*\)|<img\b[^>]*>`)
	markdownImgURL = regexp.MustCompile(`(?i)^!\[([^\]\n]*)\]\(\s*<?([^\s)>]+)>?`)
	htmlTableTag   = regexp.MustCompile(`(?is)<table\b[^>]*>`)
	htmlTableRow   = regexp.MustCompile(`(?is)<tr\b[^>]*>.*?</tr\s*>`)
	htmlTableEnd   = regexp.MustCompile(`(?is)</table\s*>`)
	htmlCaption    = regexp.MustCompile(`(?is)<caption\b[^>]*>.*?</caption\s*>`)
	htmlHeaderCell = regexp.MustCompile(`(?is)<th\b`)
)

// extractMarkdownAtoms 按文档顺序拆出正文与表格原子。
// 表格抽成独立原子，代码围栏内的管道表不抽出。
func extractMarkdownAtoms(text string) []parseAtom {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if strings.TrimSpace(text) == "" {
		return nil
	}
	lines := strings.Split(text, "\n")
	var atoms []parseAtom
	var buf []string
	inFence := false
	fenceMarker := ""

	flushText := func() {
		s := strings.TrimSpace(strings.Join(buf, "\n"))
		buf = nil
		atoms = appendMarkdownTextAndImages(atoms, s)
	}

	i := 0
	for i < len(lines) {
		line := lines[i]
		if !inFence {
			if m := mdFenceOpen.FindString(line); m != "" {
				inFence = true
				fenceMarker = strings.TrimSpace(m)[:1]
				buf = append(buf, line)
				i++
				continue
			}
			if htmlTableOpen.MatchString(line) {
				flushText()
				end := i
				for end < len(lines) && !htmlTableClose.MatchString(lines[end]) {
					end++
				}
				if end < len(lines) {
					end++
				} else {
					end = len(lines)
				}
				block := strings.TrimSpace(strings.Join(lines[i:end], "\n"))
				if block != "" {
					atoms = append(atoms, tableAtom(block))
				}
				i = end
				continue
			}
			if isMarkdownTableStart(lines, i) {
				flushText()
				end := markdownTableEnd(lines, i)
				block := strings.TrimSpace(strings.Join(lines[i:end], "\n"))
				if block != "" {
					atoms = append(atoms, tableAtom(block))
				}
				i = end
				continue
			}
		} else if isClosingFence(line, fenceMarker) {
			inFence = false
			fenceMarker = ""
		}
		buf = append(buf, line)
		i++
	}
	flushText()
	return atoms
}

// appendMarkdownTextAndImages 把 Markdown 图片语法作为独立元素保留，避免普通字符分块切断图片标记。
func appendMarkdownTextAndImages(atoms []parseAtom, text string) []parseAtom {
	text = strings.TrimSpace(text)
	if text == "" {
		return atoms
	}
	last := 0
	for _, loc := range markdownImage.FindAllStringIndex(text, -1) {
		if before := strings.TrimSpace(text[last:loc[0]]); before != "" {
			atoms = append(atoms, parseAtom{Text: before})
		}
		atoms = append(atoms, markdownImageAtom(text[loc[0]:loc[1]]))
		last = loc[1]
	}
	if after := strings.TrimSpace(text[last:]); after != "" {
		atoms = append(atoms, parseAtom{Text: after})
	}
	return atoms
}

// markdownImageAtom 把内嵌 data URI 解码为图片原子；其它地址保留原始标记交给前端渲染。
func markdownImageAtom(markup string) parseAtom {
	atom := parseAtom{Text: markup, ElementType: "image"}
	parts := markdownImgURL.FindStringSubmatch(markup)
	if len(parts) != 3 {
		return atom
	}
	target := strings.TrimSpace(parts[2])
	comma := strings.IndexByte(target, ',')
	if !strings.HasPrefix(strings.ToLower(target), "data:image/") || comma < 0 {
		return atom
	}
	metadata := strings.ToLower(target[:comma])
	if !strings.HasSuffix(metadata, ";base64") {
		return atom
	}
	payload := target[comma+1:]
	if base64.StdEncoding.DecodedLen(len(payload)) > markdownImageMaxBytes {
		return atom
	}
	data, err := base64.StdEncoding.DecodeString(payload)
	if err != nil || len(data) == 0 {
		return atom
	}
	atom.Text = strings.TrimSpace(parts[1])
	atom.Image = data
	atom.Ext = imageExtFromDataURI(metadata)
	atom.Parser = "markdown"
	return atom
}

func imageExtFromDataURI(metadata string) string {
	switch {
	case strings.Contains(metadata, "image/jpeg"):
		return ".jpg"
	case strings.Contains(metadata, "image/gif"):
		return ".gif"
	case strings.Contains(metadata, "image/webp"):
		return ".webp"
	case strings.Contains(metadata, "image/bmp"):
		return ".bmp"
	default:
		return ".png"
	}
}

func isClosingFence(line, fenceChar string) bool {
	if fenceChar == "" {
		return false
	}
	trimmed := strings.TrimLeft(line, " \t")
	if !strings.HasPrefix(trimmed, fenceChar+fenceChar+fenceChar) {
		return false
	}
	rest := strings.TrimLeft(trimmed, fenceChar)
	return strings.TrimSpace(rest) == ""
}

func markdownTableCells(line string) []string {
	stripped := strings.TrimSpace(line)
	if !strings.Contains(stripped, "|") {
		return nil
	}
	if strings.HasPrefix(stripped, "|") {
		stripped = stripped[1:]
	}
	if strings.HasSuffix(stripped, "|") {
		stripped = stripped[:len(stripped)-1]
	}
	raw := strings.Split(stripped, "|")
	cells := make([]string, len(raw))
	for i, c := range raw {
		cells[i] = strings.TrimSpace(c)
	}
	return cells
}

func isMarkdownTableRow(line string) bool {
	cells := markdownTableCells(line)
	if len(cells) < 2 {
		return false
	}
	for _, c := range cells {
		if c != "" {
			return true
		}
	}
	return false
}

func isMarkdownTableSeparator(line string) bool {
	cells := markdownTableCells(line)
	if len(cells) < 2 {
		return false
	}
	for _, c := range cells {
		compact := strings.ReplaceAll(c, " ", "")
		if !mdSepCell.MatchString(compact) {
			return false
		}
	}
	return true
}

func isMarkdownTableStart(lines []string, i int) bool {
	if i+1 >= len(lines) {
		return false
	}
	return isMarkdownTableRow(lines[i]) && isMarkdownTableSeparator(lines[i+1])
}

func markdownTableEnd(lines []string, start int) int {
	end := start + 2
	for end < len(lines) && isMarkdownTableRow(lines[end]) {
		end++
	}
	return end
}

// splitTableKeepHeader 表格超长时保留表头按行切分。
func splitTableKeepHeader(text string, target int) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if utf8.RuneCountInString(text) <= target {
		return []string{text}
	}
	if htmlTableOpen.MatchString(text) {
		return splitHTMLTableKeepHeader(text, target)
	}
	lines := strings.Split(text, "\n")
	if len(lines) < 3 || !isMarkdownTableSeparator(lines[1]) {
		return chunker.SplitRecursive(text, target, 0)
	}
	prefix := strings.Join(lines[:2], "\n")
	var (
		chunks []string
		body   []string
	)
	flush := func() {
		if len(body) == 0 {
			return
		}
		chunks = append(chunks, prefix+"\n"+strings.Join(body, "\n"))
		body = nil
	}
	for _, row := range lines[2:] {
		candidate := prefix + "\n" + strings.Join(body, "\n") + "\n" + row
		if utf8.RuneCountInString(candidate) > target && len(body) > 0 {
			flush()
		}
		body = append(body, row)
	}
	flush()
	return chunks
}

// splitHTMLTableKeepHeader 按完整表格行拆分 HTML 表格，并在每段重复表头。
// 无法识别行结构时整体保留，避免普通文本分块破坏 HTML 标签。
func splitHTMLTableKeepHeader(text string, target int) []string {
	tableTags := htmlTableTag.FindAllString(text, -1)
	if len(tableTags) != 1 {
		return []string{text}
	}
	rows := htmlTableRow.FindAllString(text, -1)
	if len(rows) == 0 || !htmlTableEnd.MatchString(text) {
		return []string{text}
	}

	caption := htmlCaption.FindString(text)
	var header strings.Builder
	bodyStart := 0
	for bodyStart < len(rows) && htmlHeaderCell.MatchString(rows[bodyStart]) {
		header.WriteString(rows[bodyStart])
		bodyStart++
	}
	if bodyStart >= len(rows) {
		return []string{text}
	}

	prefix := tableTags[0] + caption + header.String()
	suffix := "</table>"
	var (
		chunks []string
		body   []string
	)
	flush := func() {
		if len(body) == 0 {
			return
		}
		chunks = append(chunks, prefix+strings.Join(body, "")+suffix)
		body = nil
	}
	for _, row := range rows[bodyStart:] {
		candidate := prefix + strings.Join(body, "") + row + suffix
		if utf8.RuneCountInString(candidate) > target && len(body) > 0 {
			flush()
		}
		body = append(body, row)
	}
	flush()
	return chunks
}
