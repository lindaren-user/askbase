package parser

import (
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestParseXlsxSheets(t *testing.T) {
	f := excelize.NewFile()
	defer f.Close()
	_, err := f.NewSheet("销量")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.SetSheetRow("Sheet1", "A1", &[]string{"h1", "h2"}); err != nil {
		t.Fatal(err)
	}
	if err := f.SetSheetRow("Sheet1", "A2", &[]string{"a", "b"}); err != nil {
		t.Fatal(err)
	}
	if err := f.SetSheetRow("销量", "A1", &[]string{"品类", "数量"}); err != nil {
		t.Fatal(err)
	}
	if err := f.SetSheetRow("销量", "A2", &[]string{"书", "3"}); err != nil {
		t.Fatal(err)
	}
	buf, err := f.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	atoms, err := parseXlsx(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(atoms) != 2 {
		t.Fatalf("atoms=%d %#v", len(atoms), atoms)
	}
	if !atoms[0].Table || !strings.Contains(atoms[0].Text, "Sheet1") || !strings.Contains(atoms[0].Text, "| a | b |") {
		t.Fatalf("sheet1=%q table=%v", atoms[0].Text, atoms[0].Table)
	}
	if !atoms[1].Table || !strings.Contains(atoms[1].Text, "销量") || !strings.Contains(atoms[1].Text, "| 书 | 3 |") {
		t.Fatalf("销量=%q table=%v", atoms[1].Text, atoms[1].Table)
	}
}
