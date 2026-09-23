package parser

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"strings"
	"testing"
)

func TestLayoutPDFPageTwoColumns(t *testing.T) {
	glyphs := []pdfGlyph{
		{x0: 72, x1: 112, y: 700, h: 12, s: "Alpha"},
		{x0: 320, x1: 360, y: 700, h: 12, s: "Delta"},
		{x0: 72, x1: 112, y: 680, h: 12, s: "Bravo"},
		{x0: 320, x1: 360, y: 680, h: 12, s: "Echo"},
		{x0: 72, x1: 120, y: 660, h: 12, s: "Charlie"},
		{x0: 320, x1: 372, y: 660, h: 12, s: "Foxtrot"},
	}
	got := strings.Join(layoutPDFPage(glyphs), " ")
	requireIndexOrder(t, got, "Alpha", "Bravo", "Charlie", "Delta", "Echo", "Foxtrot")
}

func TestLayoutPDFPageMastheadBeforeColumns(t *testing.T) {
	glyphs := concatGlyphs(
		spanGlyphs(760, 14, 72, 540, "Attention Title"),
		spanGlyphs(740, 10, 72, 130, "Ashish"),
		spanGlyphs(740, 10, 400, 450, "Llion"),
		spanGlyphs(700, 12, 72, 250, "Abstract"),
		spanGlyphs(700, 12, 330, 540, "Recurrent"),
		spanGlyphs(680, 12, 72, 250, "We propose"),
		spanGlyphs(680, 12, 330, 540, "Attention"),
	)
	got := strings.Join(layoutPDFPage(glyphs), "\n")
	requireIndexOrder(t, got, "Attention Title", "Ashish", "Llion", "Abstract", "We propose", "Recurrent")
}

func TestLayoutPDFPageDehyphenate(t *testing.T) {
	glyphs := concatGlyphs(
		spanGlyphs(700, 12, 72, 250, "computa-"),
		spanGlyphs(700, 12, 330, 540, "English-"),
		spanGlyphs(680, 12, 72, 250, "tion"),
		spanGlyphs(680, 12, 330, 540, "to-German"),
	)
	got := strings.Join(layoutPDFPage(glyphs), "\n")
	if !strings.Contains(got, "computation") {
		t.Fatalf("want computation, got %q", got)
	}
	if !strings.Contains(got, "English-to-German") {
		t.Fatalf("want English-to-German, got %q", got)
	}
}

func TestLayoutPDFPageFootnotesAfterColumns(t *testing.T) {
	glyphs := concatGlyphs(
		spanGlyphs(700, 12, 72, 250, "LeftBody"),
		spanGlyphs(700, 12, 330, 540, "RightBody"),
		spanGlyphs(680, 12, 72, 250, "LeftMore"),
		spanGlyphs(680, 12, 330, 540, "RightMore"),
		spanGlyphs(40, 8, 72, 220, "*Equal contribution."),
	)
	got := strings.Join(layoutPDFPage(glyphs), "\n")
	requireIndexOrder(t, got, "LeftBody", "LeftMore", "RightBody", "*Equal contribution.")
}

func concatGlyphs(parts ...[]pdfGlyph) []pdfGlyph {
	var out []pdfGlyph
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// spanGlyphs 把字符串拆成窄字形铺满 [x0,x1]，避免单块过宽抬高中位字宽、拆不开栏缝。
func spanGlyphs(y, h, x0, x1 float64, text string) []pdfGlyph {
	runes := []rune(text)
	if len(runes) == 0 || x1 <= x0 {
		return nil
	}
	w := (x1 - x0) / float64(len(runes))
	out := make([]pdfGlyph, len(runes))
	for i, r := range runes {
		out[i] = pdfGlyph{
			x0: x0 + float64(i)*w,
			x1: x0 + float64(i+1)*w,
			y:  y,
			h:  h,
			s:  string(r),
		}
	}
	return out
}

func TestParsePDFTwoColumns(t *testing.T) {
	data := makeTextPDF([]string{`
BT /F1 12 Tf 72 700 Td (Alpha) Tj ET
BT /F1 12 Tf 320 700 Td (Delta) Tj ET
BT /F1 12 Tf 72 680 Td (Bravo) Tj ET
BT /F1 12 Tf 320 680 Td (Echo) Tj ET
BT /F1 12 Tf 72 660 Td (Charlie) Tj ET
BT /F1 12 Tf 320 660 Td (Foxtrot) Tj ET
`})
	atoms, err := parsePDF(data)
	if err != nil {
		t.Fatal(err)
	}
	requireIndexOrder(t, joinTextAtoms(atoms, "\n"), "Alpha", "Charlie", "Delta")
}

func TestPDFTextIsBad(t *testing.T) {
	if !pdfTextIsBad("ab") {
		t.Fatal("too few chars should be bad")
	}
	if pdfTextIsBad("AskBase PDF Parsing Test document") {
		t.Fatal("clean text should be good")
	}
	pua := strings.Repeat("\uE000", 10)
	if !pdfTextIsBad(pua) {
		t.Fatal("PUA should be garbled")
	}
	cid := strings.Repeat("(cid:12)", 10)
	if !pdfTextIsBad(cid) {
		t.Fatal("CID placeholders should be garbled")
	}
}

func TestParsePDFStripsRunningHeaderFooter(t *testing.T) {
	pages := []string{
		headerFooterContent("BodyOne"),
		headerFooterContent("BodyTwo"),
		headerFooterContent("BodyThree"),
	}
	atoms, err := parsePDF(makeTextPDF(pages))
	if err != nil {
		t.Fatal(err)
	}
	text := joinTextAtoms(atoms, "\n")
	if strings.Count(text, "CONFIDENTIAL") > 0 {
		t.Fatalf("header not stripped: %q", text)
	}
	if strings.Count(text, "PageFooter") > 0 {
		t.Fatalf("footer not stripped: %q", text)
	}
	if !strings.Contains(text, "BodyOne") || !strings.Contains(text, "BodyThree") {
		t.Fatalf("body lost: %q", text)
	}
}

func TestParsePDFScanImageNeedVision(t *testing.T) {
	jpg := tinyJPEG(t)
	data := makeImagePDF(jpg, 8, 8)
	atoms, err := parsePDF(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(atoms) != 1 || len(atoms[0].Image) == 0 || !atoms[0].NeedVision {
		t.Fatalf("want one NeedVision image atom, got %+v", summaries(atoms))
	}
	if atoms[0].Ext != ".jpg" && atoms[0].Ext != ".jpeg" {
		t.Fatalf("ext=%s", atoms[0].Ext)
	}
	if !bytes.Equal(atoms[0].Image, jpg) && !bytes.HasPrefix(atoms[0].Image, []byte{0xff, 0xd8}) {
		t.Fatalf("image payload not jpeg (%d bytes)", len(atoms[0].Image))
	}

}

func TestParsePDFSampleTextAndEmptyScan(t *testing.T) {
	sample := makeTextPDF([]string{`
BT /F1 14 Tf 72 760 Td (AskBase PDF Parsing Test) Tj ET
BT /F1 11 Tf 72 730 Td (The document pipeline has four stages: parsing, chunking, embedding and indexing.) Tj ET
BT /F1 11 Tf 72 710 Td (PDF files are parsed with a pure Go library that extracts the text layer page by page.) Tj ET
BT /F1 11 Tf 72 690 Td (Retrieval uses pgvector cosine similarity over chunk embeddings.) Tj ET
`})
	atoms, err := parsePDF(sample)
	if err != nil {
		t.Fatal(err)
	}
	text := joinTextAtoms(atoms, "\n")
	if !strings.Contains(text, "AskBase PDF Parsing Test") {
		t.Fatalf("sample text=%q", text)
	}
	if !strings.Contains(text, "pgvector") {
		t.Fatalf("sample missing later sentence: %q", text)
	}

	empty := makePDF([]string{""}, nil, 0, 0)
	_, err = parsePDF(empty)
	if err == nil || !strings.Contains(err.Error(), "无法提取文本或扫描图") {
		t.Fatalf("want empty-scan failure, got %v", err)
	}
}

func headerFooterContent(body string) string {
	return fmt.Sprintf(`
BT /F1 10 Tf 72 760 Td (CONFIDENTIAL) Tj ET
BT /F1 12 Tf 72 700 Td (%s) Tj ET
BT /F1 10 Tf 72 40 Td (PageFooter) Tj ET
`, body)
}

func tinyJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.Set(x, y, color.RGBA{R: 200, G: 10, B: 10, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func makeTextPDF(pageStreams []string) []byte {
	return makePDF(pageStreams, nil, 0, 0)
}

func makeImagePDF(img []byte, width, height int) []byte {
	return makePDF([]string{"q 612 0 0 792 0 0 cm /Im1 Do Q\n"}, img, width, height)
}

func makePDF(pageStreams []string, img []byte, imgW, imgH int) []byte {
	n := len(pageStreams)
	fontObj := 3 + 2*n
	imgObj := 0
	if len(img) > 0 {
		imgObj = fontObj + 1
	}
	type obj struct {
		id   int
		body string
	}
	var objs []obj
	kids := make([]string, 0, n)
	for i := 0; i < n; i++ {
		kids = append(kids, fmt.Sprintf("%d 0 R", 3+i))
	}
	objs = append(objs, obj{1, "<< /Type /Catalog /Pages 2 0 R >>"})
	objs = append(objs, obj{2, fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), n)})
	for i := 0; i < n; i++ {
		pageID := 3 + i
		contentID := 3 + n + i
		res := fmt.Sprintf("<< /Font << /F1 %d 0 R >>", fontObj)
		if imgObj > 0 {
			res += fmt.Sprintf(" /XObject << /Im1 %d 0 R >>", imgObj)
		}
		res += " >>"
		objs = append(objs, obj{pageID, fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources %s /Contents %d 0 R >>", res, contentID)})
	}
	for i, stream := range pageStreams {
		contentID := 3 + n + i
		body := strings.TrimSpace(stream) + "\n"
		objs = append(objs, obj{contentID, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(body), body)})
	}
	objs = append(objs, obj{fontObj, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>"})
	if imgObj > 0 {
		objs = append(objs, obj{imgObj, fmt.Sprintf(
			"<< /Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /DCTDecode /Length %d >>\nstream\n%s\nendstream",
			imgW, imgH, len(img), img,
		)})
	}
	var sb strings.Builder
	sb.WriteString("%PDF-1.4\n")
	offsets := make(map[int]int, len(objs))
	maxID := 0
	for _, o := range objs {
		offsets[o.id] = sb.Len()
		if o.id > maxID {
			maxID = o.id
		}
		fmt.Fprintf(&sb, "%d 0 obj\n%s\nendobj\n", o.id, o.body)
	}
	xrefPos := sb.Len()
	fmt.Fprintf(&sb, "xref\n0 %d\n", maxID+1)
	sb.WriteString("0000000000 65535 f \n")
	for id := 1; id <= maxID; id++ {
		off, ok := offsets[id]
		if !ok {
			sb.WriteString("0000000000 65535 f \n")
			continue
		}
		fmt.Fprintf(&sb, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&sb, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", maxID+1, xrefPos)
	return []byte(sb.String())
}

func requireIndexOrder(t *testing.T, text string, words ...string) {
	t.Helper()
	prev := -1
	for _, w := range words {
		i := strings.Index(text, w)
		if i < 0 {
			t.Fatalf("missing %q in %q", w, text)
		}
		if i < prev {
			t.Fatalf("order %v in %q", words, text)
		}
		prev = i
	}
}
