package worker

import (
	"regexp"
	"strconv"
	"strings"

	"askbase/be/internal/model"
)

var (
	markdownHeadingPattern = regexp.MustCompile(`^(#{1,6})\s+(.+?)\s*$`)
	bookHeadingPattern     = regexp.MustCompile(`(?i)^(第[0-9一二三四五六七八九十百千零〇两]+[编篇卷章节回部]|chapter\s+[0-9ivxlcdm]+|part\s+[0-9ivxlcdm]+)(?:[\s　:：.、-]+.*)?$`)
	paperHeadingPattern    = regexp.MustCompile(`(?i)^(?:[1-9]\d*(?:\.\d+)*[\s　.、]+.{1,80}|(?:abstract|摘要|introduction|引言|绪论|related\s+works?|相关工作|methods?|methodology|材料与方法|方法|experiments?|实验|results?|结果|discussion|讨论|conclusions?|结论|references|参考文献|acknowledg(?:e)?ments?|致谢)[\s　:：]*)$`)
	resumeHeadingPattern   = regexp.MustCompile(`(?i)^(?:个人信息|基本信息|个人简介|个人总结|求职意向|教育(?:背景|经历)|工作经历|实习经历|项目经历|技能(?:清单|特长)?|专业技能|证书|获奖经历|校园经历|自我评价|联系方式|profile|summary|objective|education|work\s+experience|employment|experience|projects?|skills?|certificates?|awards?)[\s　:：]*$`)
)

// prepareAtomsForStrategy 按知识库模板识别章节边界并为分块补充章节路径。
func prepareAtomsForStrategy(atoms []parseAtom, strategy string) []parseAtom {
	switch strategy {
	case model.ChunkStrategyBook, model.ChunkStrategyPaper:
		return prepareStructuredAtoms(atoms, strategy)
	default:
		return atoms
	}
}

// prepareStructuredAtoms 把标题与后续正文组合为带上下文的结构化原子。
func prepareStructuredAtoms(atoms []parseAtom, strategy string) []parseAtom {
	var (
		out      []parseAtom
		headings = map[int]string{}
		body     []string
		base     parseAtom
		hasBase  bool
	)
	flush := func() {
		text := strings.TrimSpace(strings.Join(body, "\n"))
		body = nil
		if text == "" {
			return
		}
		base.Text = text
		base.ElementType = "text"
		base.SectionPath = headingPath(headings)
		if strategy == model.ChunkStrategyResume && base.SectionPath == "" {
			base.SectionPath = "基本信息"
		}
		base.Boundary = true
		base.NoSplit = strategy == model.ChunkStrategyPaper && isPaperAbstract(base.SectionPath)
		out = append(out, base)
		hasBase = false
	}
	setHeading := func(level int, title string) {
		flush()
		for existing := range headings {
			if existing >= level {
				delete(headings, existing)
			}
		}
		headings[level] = strings.TrimSpace(title)
	}

	for _, atom := range atoms {
		if !isStructuredTextAtom(atom) {
			flush()
			atom.SectionPath = headingPath(headings)
			atom.Boundary = true
			out = append(out, atom)
			continue
		}
		lines := strings.Split(strings.ReplaceAll(atom.Text, "\r\n", "\n"), "\n")
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				if len(body) > 0 && body[len(body)-1] != "" {
					body = append(body, "")
				}
				continue
			}
			if level, title, ok := recipeHeading(strategy, trimmed, atom.ElementType); ok {
				setHeading(level, title)
				continue
			}
			if !hasBase {
				base = atom
				hasBase = true
			}
			body = append(body, trimmed)
		}
		// MinerU 原子带独立页码与坐标，不能跨原子合并后丢失来源定位。
		if atom.Parser != "" {
			flush()
		}
	}
	flush()
	return out
}

func isPaperAbstract(sectionPath string) bool {
	last := sectionPath
	if index := strings.LastIndex(sectionPath, " / "); index >= 0 {
		last = sectionPath[index+3:]
	}
	last = strings.TrimSpace(strings.TrimRight(last, " :："))
	return strings.EqualFold(last, "abstract") || last == "摘要"
}

func isStructuredTextAtom(atom parseAtom) bool {
	if len(atom.Image) > 0 || atom.Table {
		return false
	}
	switch atom.ElementType {
	case "", "text", "title", "paragraph", "section_header", "list":
		return true
	default:
		return false
	}
}

// recipeHeading 结合显式标题元素和领域规则识别章节标题及层级。
func recipeHeading(strategy, line, elementType string) (int, string, bool) {
	line = strings.TrimSpace(strings.Trim(strings.TrimSpace(line), "�"))
	if match := markdownHeadingPattern.FindStringSubmatch(line); len(match) == 3 {
		return len(match[1]), strings.TrimSpace(match[2]), true
	}
	if elementType == "title" || elementType == "section_header" {
		return 1, strings.TrimSpace(strings.TrimLeft(line, "# ")), true
	}
	if len([]rune(line)) > 100 {
		return 0, "", false
	}
	switch strategy {
	case model.ChunkStrategyBook:
		if bookHeadingPattern.MatchString(line) {
			return bookHeadingLevel(line), line, true
		}
	case model.ChunkStrategyPaper:
		if paperHeadingPattern.MatchString(line) {
			return paperHeadingLevel(line), line, true
		}
	case model.ChunkStrategyResume:
		if resumeHeadingPattern.MatchString(line) {
			return 1, strings.TrimRight(line, " :："), true
		}
	}
	return 0, "", false
}

func bookHeadingLevel(line string) int {
	lower := strings.ToLower(strings.TrimSpace(line))
	if strings.Contains(lower, "节") {
		return 2
	}
	return 1
}

func paperHeadingLevel(line string) int {
	first := strings.Fields(strings.TrimSpace(line))
	if len(first) == 0 {
		return 1
	}
	number := strings.TrimRight(first[0], ".、")
	parts := strings.Split(number, ".")
	for _, part := range parts {
		if _, err := strconv.Atoi(part); err != nil {
			return 1
		}
	}
	if len(parts) > 6 {
		return 6
	}
	return len(parts)
}

// headingPath 按标题层级生成稳定的章节路径。
func headingPath(headings map[int]string) string {
	parts := make([]string, 0, 6)
	for level := 1; level <= 6; level++ {
		if heading := strings.TrimSpace(headings[level]); heading != "" {
			parts = append(parts, heading)
		}
	}
	return strings.Join(parts, " / ")
}

// withSectionPath 将章节路径前置到正文，为独立分块补足检索上下文。
func withSectionPath(sectionPath, content string) string {
	sectionPath = strings.TrimSpace(sectionPath)
	content = strings.TrimSpace(content)
	if sectionPath == "" || content == "" {
		return content
	}
	return sectionPath + "\n\n" + content
}
