package worker

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

func TestParseDocxParagraphTableImage(t *testing.T) {
	png := tinyPNG()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	mustZip(t, zw, "word/document.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
  <w:body>
    <w:p><w:r><w:t>Hello para</w:t></w:r></w:p>
    <w:tbl>
      <w:tr>
        <w:tc><w:p><w:r><w:t>A</w:t></w:r></w:p></w:tc>
        <w:tc><w:p><w:r><w:t>B</w:t></w:r></w:p></w:tc>
      </w:tr>
      <w:tr>
        <w:tc><w:p><w:r><w:t>1</w:t></w:r></w:p></w:tc>
        <w:tc><w:p><w:r><w:t>2</w:t></w:r></w:p></w:tc>
      </w:tr>
    </w:tbl>
    <w:p>
      <w:r><w:t>before</w:t></w:r>
      <w:r><w:drawing><a:blip r:embed="rId1"/></w:drawing></w:r>
      <w:r><w:t>after</w:t></w:r>
    </w:p>
  </w:body>
</w:document>`)
	mustZip(t, zw, "word/_rels/document.xml.rels", `<?xml version="1.0" encoding="UTF-8"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="media/image1.png"/>
</Relationships>`)
	mustZipBytes(t, zw, "word/media/image1.png", png)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	atoms, err := parseDocx(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(atoms) != 5 {
		t.Fatalf("atoms=%d want 5: %+v", len(atoms), summaries(atoms))
	}
	if atoms[0].Text != "Hello para" {
		t.Fatalf("para=%q", atoms[0].Text)
	}
	if !atoms[1].Table || !strings.Contains(atoms[1].Text, "| A | B |") || !strings.Contains(atoms[1].Text, "| 1 | 2 |") {
		t.Fatalf("table=%q table=%v", atoms[1].Text, atoms[1].Table)
	}
	if atoms[2].Text != "before" {
		t.Fatalf("before=%q", atoms[2].Text)
	}
	if len(atoms[3].Image) == 0 || atoms[3].Ext != ".png" {
		t.Fatalf("image missing: %+v", atoms[3])
	}
	if atoms[4].Text != "after" {
		t.Fatalf("after=%q", atoms[4].Text)
	}

	md, err := ExtractDocxPreview(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(md, "| A | B |") || !strings.Contains(md, "Hello para") {
		t.Fatalf("preview md=%q", md)
	}
	if !strings.Contains(md, "data:image/png;base64,") {
		t.Fatalf("preview missing image: %q", md)
	}
}

func TestParseDocxKeepsHeadingLevelForQA(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	mustZip(t, zw, "word/document.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:body>
    <w:p><w:pPr><w:pStyle w:val="Heading2"/></w:pPr><w:r><w:t>如何退款？</w:t></w:r></w:p>
    <w:p><w:r><w:t>提交退款申请。</w:t></w:r></w:p>
  </w:body>
</w:document>`)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	atoms, err := parseDocx(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(atoms) != 2 || atoms[0].ElementType != "title" || atoms[0].HeadingLevel != 2 {
		t.Fatalf("heading metadata missing: %#v", atoms)
	}
}

func summaries(atoms []parseAtom) []string {
	out := make([]string, len(atoms))
	for i, a := range atoms {
		if len(a.Image) > 0 {
			out[i] = "img:" + a.Ext
			continue
		}
		out[i] = a.Text
	}
	return out
}

func mustZip(t *testing.T, zw *zip.Writer, name, body string) {
	t.Helper()
	mustZipBytes(t, zw, name, []byte(body))
}

func mustZipBytes(t *testing.T, zw *zip.Writer, name string, body []byte) {
	t.Helper()
	w, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(body); err != nil {
		t.Fatal(err)
	}
}

func tinyPNG() []byte {
	return []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d,
		0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4, 0x89, 0x00, 0x00, 0x00,
		0x0a, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00,
		0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00, 0x00, 0x00, 0x00, 0x49,
		0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
	}
}
