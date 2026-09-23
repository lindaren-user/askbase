package worker

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

func TestParseMinerUArchiveKeepsTableAndFormulaImage(t *testing.T) {
	var archive bytes.Buffer
	zw := zip.NewWriter(&archive)
	mustZip(t, zw, "result/content_list.json", `[
  {
    "type": "table",
    "page_idx": 0,
    "bbox": [10, 20, 300, 400],
    "table_body": "<table><tr><th>A</th></tr><tr><td>1</td></tr></table>"
  },
  {
    "type": "interline_equation",
    "page_idx": 1,
    "bbox": [30, 40, 500, 180],
    "text": "E = mc^2",
    "equation_img_path": "images/formula.png"
  }
]`)
	mustZipBytes(t, zw, "result/images/formula.png", tinyPNG())
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	atoms, err := parseMinerUArchive(archive.Bytes(), "vlm")
	if err != nil {
		t.Fatal(err)
	}
	if len(atoms) != 2 {
		t.Fatalf("atoms=%d: %+v", len(atoms), summaries(atoms))
	}
	if !atoms[0].Table || atoms[0].ElementType != "table" {
		t.Fatalf("table=%+v", atoms[0])
	}
	if atoms[1].ElementType != "formula" || len(atoms[1].Image) == 0 {
		t.Fatalf("formula=%+v", atoms[1])
	}
	if !strings.Contains(atoms[1].Text, "E = mc^2") || atoms[1].Page != 2 {
		t.Fatalf("formula text or page=%+v", atoms[1])
	}
}
