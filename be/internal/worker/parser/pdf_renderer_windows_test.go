//go:build windows

package parser

import (
	"context"
	"testing"
)

func TestWindowsPDFiumRendersPage(t *testing.T) {
	if _, err := loadWindowsPDFium(); err != nil {
		t.Skipf("PDFium DLL is not installed: %v", err)
	}

	data := makeTextPDF([]string{`BT /F1 12 Tf 72 700 Td (PDFium render test) Tj ET`})
	page, err := renderPDFPageWithWindowsPDFium(context.Background(), data, 0, formulaRenderDPI)
	if err != nil {
		t.Fatal(err)
	}
	if page.Bounds().Dx() <= 0 || page.Bounds().Dy() <= 0 {
		t.Fatalf("invalid rendered page bounds: %v", page.Bounds())
	}
}
