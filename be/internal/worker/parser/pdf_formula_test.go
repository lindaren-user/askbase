package parser

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestAttachPDFFormulaScreenshotsUsesZeroBasedPage(t *testing.T) {
	originalRenderer := renderPDFPageImage
	defer func() { renderPDFPageImage = originalRenderer }()

	calledPage := -1
	renderPDFPageImage = func(_ context.Context, _ []byte, pageIndex int, _ float64) (image.Image, error) {
		calledPage = pageIndex
		return image.NewRGBA(image.Rect(0, 0, 200, 100)), nil
	}
	atoms := []parseAtom{{
		Text:        "$$E=mc^2$$",
		ElementType: "formula",
		Page:        2,
		BBox:        []float64{100, 100, 400, 500},
	}}

	got := attachPDFFormulaScreenshots(context.Background(), []byte("pdf"), atoms)
	if calledPage != 1 {
		t.Fatalf("pageIndex=%d want 1", calledPage)
	}
	if len(got[0].Image) == 0 || got[0].Ext != ".png" {
		t.Fatalf("formula screenshot missing: %+v", got[0])
	}
}

func TestCropFormulaImageUsesBBoxAndPadding(t *testing.T) {
	page := image.NewRGBA(image.Rect(0, 0, 200, 100))
	for y := 0; y < 100; y++ {
		for x := 0; x < 200; x++ {
			page.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), A: 255})
		}
	}

	data, err := cropFormulaImage(page, []float64{100, 100, 400, 500})
	if err != nil {
		t.Fatal(err)
	}
	crop, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if crop.Bounds().Dx() != 76 || crop.Bounds().Dy() != 56 {
		t.Fatalf("bounds=%v", crop.Bounds())
	}
}

func TestGroupFormulaIndexesByPageSkipsExistingAsset(t *testing.T) {
	atoms := []parseAtom{
		{ElementType: "formula", Page: 1, BBox: []float64{1, 2, 3, 4}},
		{ElementType: "formula", Page: 2, BBox: []float64{1, 2, 3, 4}, Image: []byte("image")},
		{ElementType: "text", Page: 3, BBox: []float64{1, 2, 3, 4}},
	}
	indexesByPage := groupFormulaIndexesByPage(atoms)
	if len(indexesByPage) != 1 {
		t.Fatalf("indexesByPage=%v", indexesByPage)
	}
	indexes := indexesByPage[1]
	if len(indexes) != 1 || indexes[0] != 0 {
		t.Fatalf("page 1 indexes=%v", indexes)
	}
}
