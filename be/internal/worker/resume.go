package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"askbase/be/internal/llm"
	"askbase/be/internal/model"

	"go.uber.org/zap"
)

const (
	resumeSectionBasic     = "基本信息"
	resumeSectionEducation = "教育背景"
	resumeSectionSkills    = "专业技能"
	resumeSectionWork      = "工作经历"
	resumeSectionProject   = "项目经历"
	resumeSectionOther     = "其他经历"
)

var (
	resumePhonePattern      = regexp.MustCompile(`(?:\+?86[-\s]?)?1[3-9]\d{9}`)
	resumeEmailPattern      = regexp.MustCompile(`(?i)[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}`)
	resumeEntryPattern      = regexp.MustCompile(`^\s*(?:\d{1,2}[.、）)]|[一二三四五六七八九十]{1,3}[、.）)])\s*\S+`)
	resumeDateRangePattern  = regexp.MustCompile(`(?i)(?:19|20)\d{2}(?:[./年-]\d{1,2})?\s*(?:[-—–~至]|to)\s*(?:(?:19|20)\d{2}(?:[./年-]\d{1,2})?|至今|现在|present)`)
	resumeIntentPattern     = regexp.MustCompile(`(?i)^(?:求职意向|应聘职位|目标职位|期望职位|objective)\s*[:：]?\s*(.+)$`)
	resumeMarkdownPrefix    = regexp.MustCompile(`^#{1,6}\s*`)
	resumeDecorativePattern = regexp.MustCompile(`(?i)^\[(?:图片|图像|image)\]`)
	resumeThinkPattern      = regexp.MustCompile(`(?is)<think>.*?</think>`)
)

// resumeGroup 表示最终写入向量库的一个简历语义分组。
type resumeGroup struct {
	title  string
	lines  []string
	parser string
}

// resumeIdentity 保存需要冗余到每个分块的候选人身份字段。
type resumeIdentity struct {
	name   string
	phone  string
	email  string
	intent string
}

// resumeSource 是过滤图片后的行号化简历原文。
type resumeSource struct {
	lines  []string
	parser string
}

// resumeBasicSelection 是基本信息抽取任务返回的原文行索引与身份字段。
type resumeBasicSelection struct {
	Identity struct {
		Name   string `json:"name"`
		Phone  string `json:"phone"`
		Email  string `json:"email"`
		Intent string `json:"intent"`
	} `json:"identity"`
	Basic  []int `json:"basic"`
	Skills []int `json:"skills"`
	Other  []int `json:"other"`
}

// resumeEntrySelection 是教育、工作或项目任务返回的分条原文行索引。
type resumeEntrySelection struct {
	Entries [][]int `json:"entries"`
}

// materializeResumeChunks 优先用 LLM 行索引抽取语义域，失败时降级到标题与正则规则。
func materializeResumeChunks(ctx context.Context, doc model.Document, atoms []parseAtom, completer textCompleter) ([]model.Chunk, []string) {
	groups, identity, err := extractResumeGroups(ctx, atoms, completer)
	if err != nil {
		zap.L().Warn("简历 LLM 结构化失败，降级到规则分组", zap.Int64("documentId", doc.ID), zap.Error(err))
		groups, identity = groupResumeAtoms(atoms)
	}
	if len(groups) == 0 {
		return nil, nil
	}
	summary := resumeIdentitySummary(identity)
	chunks := make([]model.Chunk, 0, len(groups))
	embedTexts := make([]string, 0, len(groups))
	for _, group := range groups {
		body := strings.TrimSpace(strings.Join(group.lines, "\n"))
		if body == "" {
			continue
		}
		content := group.title + "\n\n" + body
		if summary != "" {
			content += "\n\n[候选人摘要] " + summary
		}
		parser := "resume-structured"
		if group.parser != "" {
			parser += "/" + group.parser
		}
		atom := parseAtom{ElementType: "text", Page: 1, Parser: parser}
		for _, piece := range splitResumeGroup(content, group.title, summary) {
			chunk := newElementChunk(doc, len(chunks), piece, "", atom)
			chunks = append(chunks, chunk)
			embedTexts = append(embedTexts, model.EmbedText(doc.Name, piece))
		}
	}
	return chunks, embedTexts
}

// extractResumeGroups 并行抽取基本信息、教育、工作和项目，返回的索引始终映射回原文。
func extractResumeGroups(ctx context.Context, atoms []parseAtom, completer textCompleter) ([]resumeGroup, resumeIdentity, error) {
	if completer == nil {
		return nil, resumeIdentity{}, fmt.Errorf("对话模型未配置")
	}
	source := resumeSourceFromAtoms(atoms)
	if len(source.lines) == 0 {
		return nil, resumeIdentity{}, fmt.Errorf("简历正文为空")
	}
	indexedText := buildIndexedResumeText(source.lines)

	var (
		basic     resumeBasicSelection
		education resumeEntrySelection
		work      resumeEntrySelection
		project   resumeEntrySelection
		errs      [4]error
		wg        sync.WaitGroup
	)
	wg.Add(4)
	go func() {
		defer wg.Done()
		basic, errs[0] = completeResumeTask[resumeBasicSelection](ctx, completer, "basic", indexedText)
	}()
	go func() {
		defer wg.Done()
		education, errs[1] = completeResumeTask[resumeEntrySelection](ctx, completer, "education", indexedText)
	}()
	go func() {
		defer wg.Done()
		work, errs[2] = completeResumeTask[resumeEntrySelection](ctx, completer, "work", indexedText)
	}()
	go func() {
		defer wg.Done()
		project, errs[3] = completeResumeTask[resumeEntrySelection](ctx, completer, "project", indexedText)
	}()
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return nil, resumeIdentity{}, err
		}
	}

	identity := validateResumeIdentity(resumeIdentity{
		name: basic.Identity.Name, phone: basic.Identity.Phone,
		email: basic.Identity.Email, intent: basic.Identity.Intent,
	}, source.lines)
	groups := selectionsToResumeGroups(source, basic, education, work, project)
	if len(groups) == 0 {
		return nil, resumeIdentity{}, fmt.Errorf("LLM 未返回有效字段索引")
	}
	return groups, identity, nil
}

// completeResumeTask 调用结构化抽取任务，失败时重试一次。
func completeResumeTask[T any](ctx context.Context, completer textCompleter, task, indexedText string) (T, error) {
	prompt := resumeTaskPrompt(task, indexedText)
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		content, err := completer.Complete(ctx, []llm.ChatMessage{
			{Role: "system", Content: "你是简历结构化抽取器。只能选择原文行号，禁止改写、补充或推测。只返回合法 JSON。"},
			{Role: "user", Content: prompt},
		}, 1800)
		if err == nil {
			var result T
			result, err = decodeResumeJSON[T](content)
			if err == nil {
				return result, nil
			}
		}
		lastErr = err
	}
	var zero T
	return zero, fmt.Errorf("%s 抽取失败: %w", task, lastErr)
}

// resumeTaskPrompt 为不同语义域生成只允许返回原文行号的提示词。
func resumeTaskPrompt(task, indexedText string) string {
	var instruction string
	switch task {
	case "basic":
		instruction = `抽取基本信息、技能证书和其他经历。返回：{"identity":{"name":"","phone":"","email":"","intent":""},"basic":[行号],"skills":[行号],"other":[行号]}。identity 必须逐字来自原文；没有则为空字符串。`
	case "education":
		instruction = `抽取教育经历，每段学历经历对应一个行号数组。返回：{"entries":[[行号],[行号]]}。奖学金、在校课程和校园比赛可归入相应教育经历。`
	case "work":
		instruction = `抽取工作或实习经历，每家公司/岗位对应一个完整行号数组。返回：{"entries":[[行号],[行号]]}。没有则返回空数组。`
	case "project":
		instruction = `抽取项目经历，每个项目对应一个完整行号数组，项目名称、简介、技术栈、职责和亮点必须放在同一数组。返回：{"entries":[[行号],[行号]]}。`
	}
	return instruction + "\n行号从 0 开始；不要选择章节标题；同一任务中行号不要重复。\n\n原文：\n" + indexedText
}

// decodeResumeJSON 清理模型包装文本并解析 JSON 对象。
func decodeResumeJSON[T any](content string) (T, error) {
	var result T
	content = resumeThinkPattern.ReplaceAllString(content, "")
	content = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(content, "```json", ""), "```", ""))
	start, end := strings.Index(content, "{"), strings.LastIndex(content, "}")
	if start < 0 || end < start {
		return result, fmt.Errorf("响应中没有 JSON 对象")
	}
	if err := json.Unmarshal([]byte(content[start:end+1]), &result); err != nil {
		return result, fmt.Errorf("解析 JSON 失败: %w", err)
	}
	return result, nil
}

// resumeSourceFromAtoms 过滤装饰图片并按原顺序收集文本行。
func resumeSourceFromAtoms(atoms []parseAtom) resumeSource {
	source := resumeSource{}
	for _, atom := range atoms {
		if len(atom.Image) > 0 || atom.ElementType == "image" || atom.ElementType == "chart" || atom.ElementType == "formula" {
			continue
		}
		if source.parser == "" && atom.Parser != "" {
			source.parser = atom.Parser
		}
		for _, raw := range strings.Split(strings.ReplaceAll(atom.Text, "\r\n", "\n"), "\n") {
			line := cleanResumeLine(raw)
			if line != "" && !resumeDecorativePattern.MatchString(line) {
				source.lines = append(source.lines, line)
			}
		}
	}
	return source
}

// buildIndexedResumeText 给原文添加零起始行号，供模型返回索引指针。
func buildIndexedResumeText(lines []string) string {
	var builder strings.Builder
	for index, line := range lines {
		fmt.Fprintf(&builder, "[%d]: %s\n", index, line)
	}
	return strings.TrimSpace(builder.String())
}

// selectionsToResumeGroups 按模型返回的行号恢复原文并构造语义分组。
func selectionsToResumeGroups(source resumeSource, basic resumeBasicSelection, education, work, project resumeEntrySelection) []resumeGroup {
	used := make(map[int]bool)
	groups := make([]resumeGroup, 0, 8)
	appendGroup := func(title string, indexes []int) {
		lines, valid := resumeLinesAt(source.lines, indexes, used)
		if valid {
			groups = append(groups, resumeGroup{title: title, lines: lines, parser: source.parser})
		}
	}
	appendGroup(resumeSectionBasic, basic.Basic)
	appendGroup(resumeSectionEducation, flattenResumeEntries(education.Entries))
	appendGroup(resumeSectionSkills, basic.Skills)
	for i, indexes := range work.Entries {
		appendGroup(resumeSectionWork+" "+resumeSelectionTitle(source.lines, indexes, i+1), indexes)
	}
	for i, indexes := range project.Entries {
		appendGroup(resumeSectionProject+" "+resumeSelectionTitle(source.lines, indexes, i+1), indexes)
	}
	appendGroup(resumeSectionOther, basic.Other)

	var unassigned []int
	for index, line := range source.lines {
		if used[index] {
			continue
		}
		if _, heading := classifyResumeHeading(line); !heading {
			unassigned = append(unassigned, index)
		}
	}
	appendGroup(resumeSectionOther, unassigned)
	return groups
}

// resumeLinesAt 校验、排序并去重模型返回的原文行号。
func resumeLinesAt(lines []string, indexes []int, used map[int]bool) ([]string, bool) {
	indexes = append([]int(nil), indexes...)
	sort.Ints(indexes)
	out := make([]string, 0, len(indexes))
	for _, index := range indexes {
		if index < 0 || index >= len(lines) || used[index] {
			continue
		}
		used[index] = true
		out = append(out, lines[index])
	}
	return out, len(out) > 0
}

// flattenResumeEntries 合并需要汇总为单块的多段索引。
func flattenResumeEntries(entries [][]int) []int {
	var out []int
	for _, entry := range entries {
		out = append(out, entry...)
	}
	return out
}

// resumeSelectionTitle 从条目首个短行生成便于浏览的分块标题。
func resumeSelectionTitle(lines []string, indexes []int, fallback int) string {
	for _, index := range indexes {
		if index >= 0 && index < len(lines) && len([]rune(lines[index])) <= 36 {
			return lines[index]
		}
	}
	return strconv.Itoa(fallback)
}

// validateResumeIdentity 拒绝原文中不存在的模型字段，并用规则抽取结果补空。
func validateResumeIdentity(identity resumeIdentity, lines []string) resumeIdentity {
	text := strings.Join(lines, "\n")
	if identity.name != "" && !strings.Contains(text, identity.name) {
		identity.name = ""
	}
	if identity.phone != "" && !strings.Contains(text, identity.phone) {
		identity.phone = ""
	}
	if identity.email != "" && !strings.Contains(strings.ToLower(text), strings.ToLower(identity.email)) {
		identity.email = ""
	}
	if identity.intent != "" && !strings.Contains(text, identity.intent) {
		identity.intent = ""
	}
	fallback := extractResumeIdentity(lines, lines)
	if identity.name == "" {
		identity.name = fallback.name
	}
	if identity.phone == "" {
		identity.phone = fallback.phone
	}
	if identity.email == "" {
		identity.email = fallback.email
	}
	if identity.intent == "" {
		identity.intent = fallback.intent
	}
	return identity
}

// groupResumeAtoms 将解析器原子归并为基本信息、教育、技能、工作和项目等语义组。
func groupResumeAtoms(atoms []parseAtom) ([]resumeGroup, resumeIdentity) {
	sections := make(map[string][]string)
	sectionParsers := make(map[string]string)
	order := make([]string, 0, 6)
	current := resumeSectionBasic
	ensureSection := func(section string) {
		if _, ok := sections[section]; ok {
			return
		}
		sections[section] = nil
		order = append(order, section)
	}
	ensureSection(current)

	for _, atom := range atoms {
		// 简历里的图片绝大多数是头像、联系方式图标或二维码；字段文本由正文/OCR 提供。
		if len(atom.Image) > 0 || atom.ElementType == "image" || atom.ElementType == "chart" || atom.ElementType == "formula" {
			continue
		}
		for _, raw := range strings.Split(strings.ReplaceAll(atom.Text, "\r\n", "\n"), "\n") {
			line := cleanResumeLine(raw)
			if line == "" || resumeDecorativePattern.MatchString(line) {
				continue
			}
			if section, ok := classifyResumeHeading(line); ok {
				current = section
				ensureSection(current)
				if atom.Parser != "" && sectionParsers[current] == "" {
					sectionParsers[current] = atom.Parser
				}
				continue
			}
			ensureSection(current)
			sections[current] = append(sections[current], line)
			if atom.Parser != "" && sectionParsers[current] == "" {
				sectionParsers[current] = atom.Parser
			}
		}
	}

	allLines := make([]string, 0)
	for _, section := range order {
		allLines = append(allLines, sections[section]...)
	}
	identity := extractResumeIdentity(sections[resumeSectionBasic], allLines)
	groups := make([]resumeGroup, 0, len(order)+4)
	for _, section := range order {
		lines := compactResumeLines(sections[section])
		if len(lines) == 0 {
			continue
		}
		if section == resumeSectionWork || section == resumeSectionProject {
			entries := splitResumeEntries(lines)
			for i, entry := range entries {
				title := section
				if len(entries) > 1 {
					title += " " + resumeEntryTitle(entry, i+1)
				}
				groups = append(groups, resumeGroup{title: title, lines: entry, parser: sectionParsers[section]})
			}
			continue
		}
		groups = append(groups, resumeGroup{title: section, lines: lines, parser: sectionParsers[section]})
	}
	return groups, identity
}

// classifyResumeHeading 仅供 LLM 不可用时的规则降级链路识别常见标题。
func classifyResumeHeading(line string) (string, bool) {
	normalized := strings.ToLower(strings.TrimSpace(strings.TrimRight(line, " :：")))
	switch normalized {
	case "个人信息", "基本信息", "个人简介", "联系方式", "profile", "personal information":
		return resumeSectionBasic, true
	case "教育背景", "教育经历", "学历信息", "education":
		return resumeSectionEducation, true
	case "专业技能", "技能清单", "技能特长", "证书", "语言能力", "skills", "certificates":
		return resumeSectionSkills, true
	case "工作经历", "实习经历", "任职经历", "employment", "work experience", "experience":
		return resumeSectionWork, true
	case "项目经历", "项目经验", "projects", "project experience":
		return resumeSectionProject, true
	case "获奖经历", "校园经历", "社会实践", "自我评价", "个人总结", "awards", "summary":
		return resumeSectionOther, true
	default:
		return "", false
	}
}

// cleanResumeLine 清理 Markdown 标题符和解析器残留字符。
func cleanResumeLine(line string) string {
	line = strings.TrimSpace(strings.Trim(line, "�"))
	line = resumeMarkdownPrefix.ReplaceAllString(line, "")
	line = strings.ReplaceAll(line, `\- `, "- ")
	return strings.TrimSpace(line)
}

// compactResumeLines 移除空行和相邻重复行。
func compactResumeLines(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || (len(out) > 0 && out[len(out)-1] == line) {
			continue
		}
		out = append(out, line)
	}
	return out
}

// splitResumeEntries 在规则降级链路中按编号或日期拆分经历条目。
func splitResumeEntries(lines []string) [][]string {
	var entries [][]string
	var current []string
	for _, line := range lines {
		startsEntry := resumeEntryPattern.MatchString(line)
		if !startsEntry && len(current) > 0 && resumeDateRangePattern.MatchString(line) {
			startsEntry = resumeDateRangePattern.MatchString(current[0])
		}
		if startsEntry && len(current) > 0 {
			entries = append(entries, current)
			current = nil
		}
		current = append(current, line)
	}
	if len(current) > 0 {
		entries = append(entries, current)
	}
	return entries
}

// resumeEntryTitle 为规则降级产生的经历条目选择短标题。
func resumeEntryTitle(lines []string, fallback int) string {
	if len(lines) == 0 {
		return strconv.Itoa(fallback)
	}
	title := strings.TrimSpace(lines[0])
	if len([]rune(title)) > 36 {
		return strconv.Itoa(fallback)
	}
	return title
}

// extractResumeIdentity 在规则降级链路中抽取可验证的身份字段。
func extractResumeIdentity(basicLines, allLines []string) resumeIdentity {
	identity := resumeIdentity{}
	for _, line := range allLines {
		if identity.phone == "" {
			identity.phone = resumePhonePattern.FindString(line)
		}
		if identity.email == "" {
			identity.email = resumeEmailPattern.FindString(line)
		}
		if identity.intent == "" {
			if match := resumeIntentPattern.FindStringSubmatch(line); len(match) == 2 {
				identity.intent = strings.TrimSpace(match[1])
			}
		}
	}
	for _, line := range basicLines {
		candidate := strings.TrimSpace(line)
		if candidate == "" || strings.ContainsAny(candidate, ":：@0123456789") || len([]rune(candidate)) > 16 {
			continue
		}
		letters := 0
		for _, r := range candidate {
			if unicode.IsLetter(r) || r == '·' || r == ' ' {
				letters++
			}
		}
		if letters == len([]rune(candidate)) {
			identity.name = candidate
			break
		}
	}
	return identity
}

// resumeIdentitySummary 生成冗余到每个分块的候选人摘要。
func resumeIdentitySummary(identity resumeIdentity) string {
	parts := make([]string, 0, 4)
	if identity.name != "" {
		parts = append(parts, "姓名:"+identity.name)
	}
	if identity.phone != "" {
		parts = append(parts, "电话:"+identity.phone)
	}
	if identity.email != "" {
		parts = append(parts, "邮箱:"+identity.email)
	}
	if identity.intent != "" {
		parts = append(parts, "求职意向:"+identity.intent)
	}
	return strings.Join(parts, " | ")
}

// splitResumeGroup 对超长语义组二次切分，并为每段保留标题与身份摘要。
func splitResumeGroup(content, title, summary string) []string {
	if len([]rune(content)) <= chunkTargetRunes {
		return []string{content}
	}
	body := strings.TrimPrefix(content, title+"\n\n")
	if summary != "" {
		body = strings.TrimSuffix(body, "\n\n[候选人摘要] "+summary)
	}
	pieces := splitRecursive(body, chunkTargetRunes, chunkOverlapRunes)
	for i := range pieces {
		pieces[i] = title + "\n\n" + pieces[i]
		if summary != "" {
			pieces[i] += "\n\n[候选人摘要] " + summary
		}
	}
	return pieces
}
