package parser

import (
	"bytes"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	pdf "github.com/ledongthuc/pdf"
)

var (
	pngMagic      = []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	pdfDictWidth  = regexp.MustCompile(`/Width\s+(\d+)`)
	pdfDictHeight = regexp.MustCompile(`/Height\s+(\d+)`)
	pdfDictLength = regexp.MustCompile(`/Length\s+(\d+)`)
	pdfDictFilter = regexp.MustCompile(`/Filter\s*(?:\[\s*)?/([A-Za-z0-9]+)`)
	pdfCIDPattern = regexp.MustCompile(`\(cid\s*:\s*\d+\s*\)`)
)

const (
	pdfMinNonSpaceChars = 8    // 少于此非空白字数视为空页/扫描页
	pdfGarbledRatio     = 0.3  // 乱码占比达到此值则丢弃文本层
	pdfHeaderMaxRunes   = 40   // 可当作页眉页脚剥掉的短行上限
	pdfColMinWidthFrac  = 0.28 // 稳定双栏行每块最小宽度（相对页宽），作者短格子达不到
	pdfFootnoteHFrac    = 0.8  // 小于此倍中位字高视为脚注
	pdfFootnoteBandH    = 8    // 底带高度 = 此倍中位字高
)

var errPDFEmpty = fmt.Errorf("%w: 无法提取文本或扫描图", errPermanent)

// pdfPageResult 一页的解析结果：阅读顺序文本行，或扫描页抽出的图。
type pdfPageResult struct {
	lines []string
	image []byte
	ext   string
}

// pdfGlyph 带页内坐标的字形。Y 原点在页底，值越大越靠上。
type pdfGlyph struct {
	x0, x1, y, h float64
	s            string
}

// pdfBox 一行（或按 X 缝拆出的一栏片段）。
type pdfBox struct {
	x0, x1, y, h float64
	text         string
	col          int
}

// embeddedPDFImage 从 PDF 字节流扫到的内嵌图，供坏页回填。
type embeddedPDFImage struct {
	width, height int64
	data          []byte
	ext           string
}

// parsePDF 按页做坐标分栏、乱码/空页抽内嵌图，输出阅读顺序的文本/图片原子。
func parsePDF(data []byte) ([]parseAtom, error) {
	reader, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		// TODO: ledongthuc/pdf 不支持 AES-256（/Length 256，V=5/R=5–6）。
		// 用户口令为空的权限加密在阅读器里能直接打开，这里会报 malformed PDF: 256-bit encryption key。
		return nil, fmt.Errorf("%w: PDF 解析失败: %v", errPermanent, err)
	}
	n := reader.NumPage()
	if n <= 0 {
		return nil, errPDFEmpty
	}
	images := scanPDFImages(data)
	pages := make([]pdfPageResult, 0, n)
	for i := 1; i <= n; i++ {
		page := reader.Page(i)
		if page.V.IsNull() {
			continue
		}
		pages = append(pages, parsePDFPage(page, images))
	}
	stripPDFRunningMargins(pages)
	atoms := pdfPagesToAtoms(pages)
	if len(atoms) == 0 {
		return nil, errPDFEmpty
	}
	return atoms, nil
}

// parsePDFPage 干净文本层走坐标排版；空页或乱码则抽该页最大内嵌图。
func parsePDFPage(page pdf.Page, images []embeddedPDFImage) pdfPageResult {
	glyphs := pdfPageGlyphs(page)
	if !pdfPageIsBad(glyphs) {
		return pdfPageResult{lines: layoutPDFPage(glyphs)}
	}
	// TODO: 无光栅器，扫描页只抽最大内嵌 XObject（JPEG 按 /Length 切，避免 DCTDecode panic）。
	// 纯矢量/无图扫描页得到空结果，整份文档最终报「无法提取文本或扫描图」。
	img, ext := largestPageImage(page, images)
	if len(img) == 0 {
		return pdfPageResult{}
	}
	return pdfPageResult{image: img, ext: ext}
}

// pdfPageGlyphs 从 Content() 取出带 X/Y/W 的字形，去掉换行控制符。
func pdfPageGlyphs(page pdf.Page) []pdfGlyph {
	content := safePageContent(page)
	out := make([]pdfGlyph, 0, len(content.Text))
	for _, t := range content.Text {
		s := strings.ReplaceAll(strings.ReplaceAll(t.S, "\n", ""), "\r", "")
		if s == "" {
			continue
		}
		h := t.FontSize
		if h <= 0 {
			h = 10
		}
		w := t.W
		if w <= 0 {
			w = h * 0.5 * float64(len([]rune(s)))
		}
		out = append(out, pdfGlyph{x0: t.X, x1: t.X + w, y: t.Y, h: h, s: s})
	}
	return out
}

// safePageContent 调用 ledongthuc Content；解析恐慌时返回空内容，不中断整份文档。
func safePageContent(page pdf.Page) (c pdf.Content) {
	defer func() { _ = recover() }()
	return page.Content()
}

// pdfPagesToAtoms 连续文本页合成一个原子（页间 \n\n）；扫描图插在对应页位置并标记 NeedVision。
func pdfPagesToAtoms(pages []pdfPageResult) []parseAtom {
	var atoms []parseAtom
	for _, p := range pages {
		if len(p.image) > 0 {
			ext := p.ext
			if ext == "" {
				ext = ".jpg"
			}
			atoms = append(atoms, parseAtom{Image: p.image, Ext: ext, NeedVision: true})
			continue
		}
		text := strings.TrimSpace(strings.Join(p.lines, "\n"))
		if text == "" {
			continue
		}
		if n := len(atoms); n > 0 && len(atoms[n-1].Image) == 0 && !atoms[n-1].Table {
			atoms[n-1].Text += "\n\n" + text
			continue
		}
		atoms = append(atoms, parseAtom{Text: text})
	}
	return atoms
}

// largestPageImage 在该页 Resources/XObject 中取面积最大的图；对不上目录则退回全文最大图。
func largestPageImage(page pdf.Page, catalog []embeddedPDFImage) ([]byte, string) {
	xobj := page.Resources().Key("XObject")
	if xobj.IsNull() {
		return pickLargestImage(catalog)
	}
	var best []byte
	var bestExt string
	var bestArea int64
	for _, name := range xobj.Keys() {
		obj := xobj.Key(name)
		if obj.IsNull() {
			continue
		}
		if sub := obj.Key("Subtype").Name(); sub != "" && sub != "Image" {
			continue
		}
		w, h := obj.Key("Width").Int64(), obj.Key("Height").Int64()
		area := w * h
		if area <= bestArea {
			continue
		}
		filter := pdfFilterName(obj.Key("Filter"))
		img, ext := matchCatalogImage(catalog, w, h, filter)
		if len(img) == 0 {
			img, ext = readFlateOrRawImage(obj, filter)
		}
		if len(img) == 0 {
			continue
		}
		best, bestExt, bestArea = img, ext, area
	}
	if len(best) > 0 {
		return best, bestExt
	}
	return pickLargestImage(catalog)
}

// pickLargestImage 取目录里面积最大的图；面积相同则取字节更多的。
func pickLargestImage(catalog []embeddedPDFImage) ([]byte, string) {
	var best embeddedPDFImage
	var bestArea int64
	for _, im := range catalog {
		area := im.width * im.height
		if area > bestArea || (area == bestArea && len(im.data) > len(best.data)) {
			bestArea, best = area, im
		}
	}
	if len(best.data) == 0 {
		return nil, ""
	}
	return best.data, best.ext
}

// matchCatalogImage 按宽高对齐目录中的图；DCTDecode 只接受 JPEG。
func matchCatalogImage(catalog []embeddedPDFImage, w, h int64, filter string) ([]byte, string) {
	wantJPG := filter == "DCTDecode"
	for _, im := range catalog {
		if w > 0 && h > 0 && (im.width != w || im.height != h) {
			continue
		}
		if wantJPG && im.ext != ".jpg" && im.ext != ".jpeg" {
			continue
		}
		if len(im.data) > 0 {
			return im.data, im.ext
		}
	}
	return nil, ""
}

// pdfFilterName 读取 PDF 流的单个或首个过滤器名称。
func pdfFilterName(v pdf.Value) string {
	switch v.Kind() {
	case pdf.Name:
		return v.Name()
	case pdf.Array:
		if v.Len() > 0 {
			return v.Index(0).Name()
		}
	}
	return ""
}

// readFlateOrRawImage 仅在 FlateDecode 且载荷已是 PNG 时返回；JPEG 走字节扫描，避免 Reader 对 DCTDecode panic。
func readFlateOrRawImage(obj pdf.Value, filter string) (data []byte, ext string) {
	defer func() { _ = recover() }()
	if filter != "" && filter != "FlateDecode" {
		return nil, ""
	}
	rc := obj.Reader()
	if rc == nil {
		return nil, ""
	}
	defer rc.Close()
	raw, err := io.ReadAll(io.LimitReader(rc, 12<<20))
	if err != nil || !isPNG(raw) {
		return nil, ""
	}
	return raw, ".png"
}

// scanPDFImages 扫描 PDF 字节里的 Image XObject 流。ledongthuc 不能安全读 DCTDecode，故按 /Length 直接切 JPEG/PNG。
func scanPDFImages(src []byte) []embeddedPDFImage {
	var out []embeddedPDFImage
	for i := 0; i < len(src); {
		rel := bytes.Index(src[i:], []byte("stream"))
		if rel < 0 {
			break
		}
		abs := i + rel
		img, next, ok := tryPDFImageStream(src, abs)
		if ok {
			out = append(out, img)
		}
		i = next
	}
	return out
}

// tryPDFImageStream 把 abs 处的 stream 当成 Image XObject 解析。next 始终前进，避免死循环。
func tryPDFImageStream(src []byte, abs int) (embeddedPDFImage, int, bool) {
	next := abs + len("stream")
	if !isBareStreamKeyword(src, abs) {
		return embeddedPDFImage{}, next, false
	}
	dictStart := bytes.LastIndex(src[:abs], []byte("<<"))
	if dictStart < 0 {
		return embeddedPDFImage{}, next, false
	}
	dict := src[dictStart:abs]
	if !bytes.Contains(dict, []byte("/Image")) {
		return embeddedPDFImage{}, next, false
	}
	length := pdfDictInt(dict, pdfDictLength)
	k := skipStreamNewline(src, next)
	if length <= 0 || k+int(length) > len(src) {
		return embeddedPDFImage{}, next, false
	}
	end := k + int(length)
	payload := src[k:end]
	filter := ""
	if m := pdfDictFilter.FindSubmatch(dict); len(m) == 2 {
		filter = string(m[1])
	}
	ext := streamImageExt(filter, payload)
	if ext == "" {
		return embeddedPDFImage{}, end, false
	}
	return embeddedPDFImage{
		width:  pdfDictInt(dict, pdfDictWidth),
		height: pdfDictInt(dict, pdfDictHeight),
		data:   payload,
		ext:    ext,
	}, end, true
}

// isBareStreamKeyword 排除 endstream，以及名称里碰巧含 stream 的 token。
func isBareStreamKeyword(src []byte, abs int) bool {
	if abs >= 3 && bytes.Equal(src[abs-3:abs], []byte("end")) {
		return false
	}
	return abs == 0 || !isPDFNameChar(src[abs-1])
}

func skipStreamNewline(src []byte, k int) int {
	if k < len(src) && src[k] == '\r' {
		k++
	}
	if k < len(src) && src[k] == '\n' {
		k++
	}
	return k
}

// streamImageExt 根据 PDF 过滤器和文件签名推断内嵌图片扩展名。
func streamImageExt(filter string, payload []byte) string {
	switch filter {
	case "DCTDecode":
		return ".jpg"
	case "FlateDecode":
		if isPNG(payload) {
			return ".png"
		}
	default:
		if isJPEG(payload) {
			return ".jpg"
		}
		if isPNG(payload) {
			return ".png"
		}
	}
	return ""
}

func isPNG(b []byte) bool {
	return len(b) >= len(pngMagic) && bytes.Equal(b[:len(pngMagic)], pngMagic)
}

func isJPEG(b []byte) bool {
	return len(b) >= 2 && b[0] == 0xff && b[1] == 0xd8
}

func pdfDictInt(dict []byte, re *regexp.Regexp) int64 {
	m := re.FindSubmatch(dict)
	if len(m) < 2 {
		return 0
	}
	n, _ := strconv.ParseInt(string(m[1]), 10, 64)
	return n
}

func isPDFNameChar(b byte) bool {
	return unicode.IsLetter(rune(b)) || unicode.IsDigit(rune(b))
}
