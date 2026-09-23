package parser

import (
	"math"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// pdfPageIsBad 空页或乱码文本层则应走扫描图路径。
func pdfPageIsBad(glyphs []pdfGlyph) bool {
	var b strings.Builder
	for _, g := range glyphs {
		b.WriteString(g.s)
	}
	return pdfTextIsBad(b.String())
}

// pdfTextIsBad 非空白过少，或 PUA / U+FFFD / (cid:N) 占比达到阈值，视为假文本层。
func pdfTextIsBad(s string) bool {
	nonSpace, garbled := 0, 0
	for _, r := range s {
		if unicode.IsSpace(r) {
			continue
		}
		nonSpace++
		if pdfRuneIsGarbled(r) {
			garbled++
		}
	}
	for _, m := range pdfCIDPattern.FindAllString(s, -1) {
		garbled += utf8.RuneCountInString(m)
	}
	if nonSpace < pdfMinNonSpaceChars {
		return true
	}
	return float64(garbled)/float64(nonSpace) >= pdfGarbledRatio
}

// pdfRuneIsGarbled 识别字体未映射常见垃圾：替换符、控制符、私用区。
func pdfRuneIsGarbled(r rune) bool {
	switch {
	case r == '\uFFFD':
		return true
	case r < 0x20 && r != '\t' && r != '\n' && r != '\r':
		return true
	case r >= 0x80 && r <= 0x9F:
		return true
	case r >= 0xE000 && r <= 0xF8FF:
		return true
	case r >= 0xF0000 && r <= 0xFFFFF:
		return true
	case r >= 0x100000 && r <= 0x10FFFF:
		return true
	default:
		return unicode.Is(unicode.Cs, r) || unicode.Is(unicode.Co, r)
	}
}

// layoutPDFPage 聚行、拆栏、页眉带行内从左到右、正文先左后右，栏底小字脚注后置。
func layoutPDFPage(glyphs []pdfGlyph) []string {
	if len(glyphs) == 0 {
		return nil
	}
	medianH, medianW := glyphMedians(glyphs)
	pageW := glyphSpanX(glyphs)
	if pageW < 1 {
		pageW = 612
	}
	boxes := pdfGlyphsToBoxes(glyphs, medianH, medianW, pageW)
	if len(boxes) == 0 {
		return nil
	}
	notes, rest := extractPDFFootnotes(boxes, medianH)
	masthead, body := splitPDFMasthead(rest, pageW, medianH)
	assignPDFColumns(body, pageW)
	left, right := splitPDFBodyColumns(body)

	var lines []string
	lines = appendPDFSection(lines, masthead)
	lines = appendPDFSection(lines, left)
	lines = appendPDFSection(lines, right)
	return appendPDFSection(lines, notes)
}

// pdfGlyphsToBoxes 将字形先按视觉行分组，再按横向大间距切成版面文本框。
func pdfGlyphsToBoxes(glyphs []pdfGlyph, medianH, medianW, pageW float64) []pdfBox {
	gapThr := math.Max(2.5*medianW, 0.12*pageW)
	var boxes []pdfBox
	for _, line := range groupPDFLines(glyphs, medianH) {
		sort.Slice(line, func(i, j int) bool { return line[i].x0 < line[j].x0 })
		for _, frag := range splitLineByXGap(line, gapThr) {
			text := joinPDFGlyphs(frag, medianW)
			if strings.TrimSpace(text) == "" {
				continue
			}
			boxes = append(boxes, pdfBox{
				x0:   frag[0].x0,
				x1:   frag[len(frag)-1].x1,
				y:    frag[0].y,
				h:    fragmentHeight(frag),
				text: text,
			})
		}
	}
	return boxes
}

func appendPDFSection(dst []string, boxes []pdfBox) []string {
	return append(dst, dehyphenatePDFLines(pdfBoxesTexts(sortPDFBoxes(boxes)))...)
}

func fragmentHeight(frag []pdfGlyph) float64 {
	hs := make([]float64, 0, len(frag))
	for _, g := range frag {
		if g.h > 0 {
			hs = append(hs, g.h)
		}
	}
	h := pdfMedian(hs)
	if h <= 0 {
		return 10
	}
	return h
}

// sortPDFBoxes 按栏和阅读顺序排列文本框，避免双栏正文左右交错。
func sortPDFBoxes(boxes []pdfBox) []pdfBox {
	out := append([]pdfBox(nil), boxes...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].y != out[j].y {
			return out[i].y > out[j].y
		}
		return out[i].x0 < out[j].x0
	})
	return out
}

func pdfBoxesTexts(boxes []pdfBox) []string {
	out := make([]string, len(boxes))
	for i, b := range boxes {
		out[i] = b.text
	}
	return out
}

// splitPDFBodyColumns 按 assignPDFColumns 的 col 拆左右栏；未分栏时全部算左栏。
// splitPDFBodyColumns 根据预先标记的栏位拆分左右栏正文。
func splitPDFBodyColumns(body []pdfBox) (left, right []pdfBox) {
	for _, b := range body {
		if b.col == 1 {
			right = append(right, b)
		} else {
			left = append(left, b)
		}
	}
	return left, right
}

// glyphMedians 计算稳健的字形宽高基准，供行聚类和间距判断使用。
func glyphMedians(glyphs []pdfGlyph) (height, width float64) {
	hs := make([]float64, 0, len(glyphs))
	ws := make([]float64, 0, len(glyphs))
	for _, g := range glyphs {
		if g.h > 0 {
			hs = append(hs, g.h)
		}
		if w := g.x1 - g.x0; w > 0 {
			ws = append(ws, w)
		}
	}
	height = pdfMedian(hs)
	if height <= 0 {
		height = 10
	}
	width = pdfMedian(ws)
	if width <= 0 {
		width = height * 0.5
	}
	return height, width
}

func glyphSpanX(glyphs []pdfGlyph) float64 {
	minX, maxX1 := glyphs[0].x0, glyphs[0].x1
	for _, g := range glyphs[1:] {
		if g.x0 < minX {
			minX = g.x0
		}
		if g.x1 > maxX1 {
			maxX1 = g.x1
		}
	}
	return maxX1 - minX
}

// groupPDFLines 按 Y（页底原点）从上到下聚行；垂直距离超过半个中位字高则换行。
// groupPDFLines 按纵向位置把字形聚类为视觉行，并在行内按横坐标排序。
func groupPDFLines(glyphs []pdfGlyph, medianH float64) [][]pdfGlyph {
	sorted := append([]pdfGlyph(nil), glyphs...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].y != sorted[j].y {
			return sorted[i].y > sorted[j].y
		}
		return sorted[i].x0 < sorted[j].x0
	})
	thresh := math.Max(medianH*0.5, 2)
	var lines [][]pdfGlyph
	var cur []pdfGlyph
	var curY float64
	for _, g := range sorted {
		if len(cur) > 0 && math.Abs(g.y-curY) > thresh {
			lines = append(lines, cur)
			cur = nil
		}
		if len(cur) == 0 {
			curY = g.y
		}
		cur = append(cur, g)
	}
	if len(cur) > 0 {
		lines = append(lines, cur)
	}
	return lines
}

// splitLineByXGap 行内间隙大于阈值则拆成左右片段（双栏中缝）。
// splitLineByXGap 用异常大的横向间距把同一视觉行拆成多个文本片段。
func splitLineByXGap(line []pdfGlyph, thr float64) [][]pdfGlyph {
	if len(line) == 0 {
		return nil
	}
	var frags [][]pdfGlyph
	cur := []pdfGlyph{line[0]}
	for i := 1; i < len(line); i++ {
		if line[i].x0-line[i-1].x1 > thr {
			frags = append(frags, cur)
			cur = []pdfGlyph{line[i]}
			continue
		}
		cur = append(cur, line[i])
	}
	return append(frags, cur)
}

// joinPDFGlyphs 拼字形；拉丁文在明显字距处补空格，CJK 不补。
func joinPDFGlyphs(glyphs []pdfGlyph, medianW float64) string {
	var b strings.Builder
	for i, g := range glyphs {
		if i > 0 && pdfShouldInsertSpace(glyphs[i-1].s, g.s, g.x0-glyphs[i-1].x1, medianW) {
			b.WriteByte(' ')
		}
		b.WriteString(g.s)
	}
	return strings.TrimSpace(b.String())
}

// pdfShouldInsertSpace 字距大于 0.25 中位字宽且两侧非 CJK 时插入空格（许多 PDF 不写空格 glyph）。
func pdfShouldInsertSpace(prev, next string, gap, medianW float64) bool {
	if gap <= 0.25*medianW {
		return false
	}
	pr, _ := utf8.DecodeLastRuneInString(prev)
	nr, _ := utf8.DecodeRuneInString(next)
	return !pdfIsCJK(pr) && !pdfIsCJK(nr)
}

func pdfIsCJK(r rune) bool {
	if r == utf8.RuneError {
		return false
	}
	return unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r) || unicode.Is(unicode.Hangul, r)
}

// splitPDFMasthead 把稳定双栏起点以上的盒子切成页眉带（行内从左到右）；找不到双栏则全部当正文。
// splitPDFMasthead 分离跨栏页眉和正文，避免页眉参与双栏排序。
func splitPDFMasthead(boxes []pdfBox, pageW, medianH float64) (masthead, body []pdfBox) {
	ySplit, ok := pdfTwoColumnBodyY(boxes, pageW, medianH)
	if !ok {
		return nil, boxes
	}
	for _, b := range boxes {
		if b.y > ySplit {
			masthead = append(masthead, b)
		} else {
			body = append(body, b)
		}
	}
	if len(body) == 0 {
		return nil, boxes
	}
	return masthead, body
}

// pdfTwoColumnBodyY 从上往下找连续 ≥2 条宽双栏行，返回第一条的 Y（该行起算正文）。
// pdfTwoColumnBodyY 定位页面开始呈现稳定双栏结构的纵坐标。
func pdfTwoColumnBodyY(boxes []pdfBox, pageW, medianH float64) (float64, bool) {
	if len(boxes) == 0 || pageW <= 0 {
		return 0, false
	}
	minW := pdfColMinWidthFrac * pageW
	bands := groupPDFBoxesByY(boxes, math.Max(medianH*0.5, 2)) // 已从上到下，band[0] 是该带最高点
	run := 0
	var firstY float64
	for _, band := range bands {
		if !pdfBandIsTwoColumn(band, minW) {
			run = 0
			continue
		}
		if run == 0 {
			firstY = band[0].y
		}
		run++
		if run >= 2 {
			return firstY, true
		}
	}
	return 0, false
}

func groupPDFBoxesByY(boxes []pdfBox, thresh float64) [][]pdfBox {
	sorted := sortPDFBoxes(boxes)
	var bands [][]pdfBox
	var cur []pdfBox
	var curY float64
	for _, b := range sorted {
		if len(cur) > 0 && math.Abs(b.y-curY) > thresh {
			bands = append(bands, cur)
			cur = nil
		}
		if len(cur) == 0 {
			curY = b.y
		}
		cur = append(cur, b)
	}
	if len(cur) > 0 {
		bands = append(bands, cur)
	}
	return bands
}

// pdfBandIsTwoColumn 同一 Y 带左右都有足够宽的块，视为正文双栏行而非作者短格子。
// pdfBandIsTwoColumn 判断同一水平带是否同时覆盖左右两栏。
func pdfBandIsTwoColumn(band []pdfBox, minW float64) bool {
	if len(band) < 2 {
		return false
	}
	minX, maxX1 := pdfBoxesXRange(band)
	mid := (minX + maxX1) / 2
	left, right := false, false
	for _, b := range band {
		if b.x1-b.x0 < minW {
			continue
		}
		if b.x0 < mid {
			left = true
		} else {
			right = true
		}
	}
	return left && right
}

// extractPDFFootnotes 抽出页底带内明显小于中位字高的盒子，避免脚注插在左栏末、右栏正文前。
// extractPDFFootnotes 将页面底部的小字号脚注从正文阅读顺序中分离。
func extractPDFFootnotes(boxes []pdfBox, medianH float64) (notes, rest []pdfBox) {
	if len(boxes) == 0 || medianH <= 0 {
		return nil, boxes
	}
	minY := boxes[0].y
	for _, b := range boxes[1:] {
		if b.y < minY {
			minY = b.y
		}
	}
	bandTop := minY + pdfFootnoteBandH*medianH
	hThr := pdfFootnoteHFrac * medianH
	for _, b := range boxes {
		if b.y <= bandTop && b.h > 0 && b.h < hThr {
			notes = append(notes, b)
			continue
		}
		rest = append(rest, b)
	}
	if len(rest) == 0 {
		return nil, boxes
	}
	return notes, rest
}

// dehyphenatePDFLines 合并同栏相邻行的栏末断词：computa- + tion → computation；English- + to-German 保留连字符。
// dehyphenatePDFLines 合并英文排版造成的跨行连字符，不处理语义性短横线。
func dehyphenatePDFLines(lines []string) []string {
	if len(lines) == 0 {
		return nil
	}
	out := []string{lines[0]}
	for i := 1; i < len(lines); i++ {
		merged, ok := pdfMergeHyphen(out[len(out)-1], lines[i])
		if ok {
			out[len(out)-1] = merged
			continue
		}
		out = append(out, lines[i])
	}
	return out
}

func pdfMergeHyphen(prev, next string) (string, bool) {
	if next == "" || !strings.HasSuffix(prev, "-") || strings.HasSuffix(prev, "--") {
		return "", false
	}
	stem := strings.TrimSuffix(prev, "-")
	pr, _ := utf8.DecodeLastRuneInString(stem)
	nr, _ := utf8.DecodeRuneInString(next)
	if !unicode.IsLetter(pr) || !unicode.IsLetter(nr) {
		return "", false
	}
	if unicode.IsLower(pr) && unicode.IsLower(nr) && pdfHyphenContinuation(next) {
		return stem + next, true
	}
	return prev + next, true
}

// pdfHyphenContinuation 下一行是纯小写续写（tion、wise），不是 English-to-German 这类带连字符的复合词。
func pdfHyphenContinuation(next string) bool {
	if next == "" {
		return false
	}
	for _, r := range next {
		if unicode.IsSpace(r) {
			return true
		}
		if r == '-' || !unicode.IsLower(r) {
			return false
		}
	}
	return true
}

// assignPDFColumns 仅当左右两簇都有足够片段且中心间距 > 0.2 页宽时标两栏，避免缩进被切成假双栏。
// assignPDFColumns 根据文本框中心点标记左栏、右栏或跨栏元素。
func assignPDFColumns(boxes []pdfBox, pageW float64) {
	if len(boxes) < 4 || pageW <= 0 {
		return
	}
	minX, maxX1 := pdfBoxesXRange(boxes)
	mid := (minX + maxX1) / 2
	var nLeft, nRight int
	var sumLeft, sumRight float64
	for _, b := range boxes {
		if b.x0 < mid {
			nLeft++
			sumLeft += b.x0
		} else {
			nRight++
			sumRight += b.x0
		}
	}
	if nLeft < 2 || nRight < 2 {
		return
	}
	if sumRight/float64(nRight)-sumLeft/float64(nLeft) <= 0.2*pageW {
		return
	}
	for i := range boxes {
		if boxes[i].x0 < mid {
			boxes[i].col = 0
		} else {
			boxes[i].col = 1
		}
	}
}

// stripPDFRunningMargins 去掉出现在至少一半文本页上的短首行/末行（页眉页脚）。页数少于 2 时不处理。
// stripPDFRunningMargins 删除多页重复出现的短页眉和页脚。
func stripPDFRunningMargins(pages []pdfPageResult) {
	n := 0
	for _, p := range pages {
		if len(p.lines) > 0 {
			n++
		}
	}
	if n < 2 {
		return
	}
	firstN, lastN := map[string]int{}, map[string]int{}
	for _, p := range pages {
		if len(p.lines) == 0 {
			continue
		}
		countShortLine(firstN, p.lines[0])
		countShortLine(lastN, p.lines[len(p.lines)-1])
	}
	dropFirst := majorityKeys(firstN, n)
	dropLast := majorityKeys(lastN, n)
	if len(dropFirst) == 0 && len(dropLast) == 0 {
		return
	}
	for i := range pages {
		lines := pages[i].lines
		if len(lines) == 0 {
			continue
		}
		if dropFirst[strings.TrimSpace(lines[0])] {
			lines = lines[1:]
		}
		if len(lines) > 0 && dropLast[strings.TrimSpace(lines[len(lines)-1])] {
			lines = lines[:len(lines)-1]
		}
		pages[i].lines = lines
	}
}

func countShortLine(counts map[string]int, line string) {
	s := strings.TrimSpace(line)
	if s != "" && utf8.RuneCountInString(s) < pdfHeaderMaxRunes {
		counts[s]++
	}
}

func majorityKeys(counts map[string]int, pages int) map[string]bool {
	out := map[string]bool{}
	for s, c := range counts {
		if c*2 >= pages {
			out[s] = true
		}
	}
	return out
}

func pdfMedian(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	cp := append([]float64(nil), vals...)
	sort.Float64s(cp)
	return cp[len(cp)/2]
}

func pdfBoxesXRange(boxes []pdfBox) (minX, maxX1 float64) {
	minX, maxX1 = boxes[0].x0, boxes[0].x1
	for _, b := range boxes[1:] {
		if b.x0 < minX {
			minX = b.x0
		}
		if b.x1 > maxX1 {
			maxX1 = b.x1
		}
	}
	return minX, maxX1
}
