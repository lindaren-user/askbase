package parser

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"math"

	"go.uber.org/zap"
)

const minerUBBoxScale = 1000.0

// attachPDFFormulaScreenshots 按 MinerU 页码和归一化 bbox 从原 PDF 裁出公式图。
// MinerU 已直接提供公式图片时复用原图；当前构建不支持 PDFium 时保留公式文本降级展示。
func attachPDFFormulaScreenshots(ctx context.Context, data []byte, atoms []parseAtom) []parseAtom {
	formulaIndexesByPage := groupFormulaIndexesByPage(atoms)
	for page, indexes := range formulaIndexesByPage {
		pageImage, err := renderPDFPageImage(ctx, data, page-1, formulaRenderDPI)
		if err != nil {
			zap.L().Warn("PDFium 渲染公式页失败", zap.Int("pageNumber", page), zap.Error(err))
			continue
		}
		for _, index := range indexes {
			crop, err := cropFormulaImage(pageImage, atoms[index].BBox)
			if err != nil {
				zap.L().Warn("裁剪 PDF 公式失败", zap.Int("pageNumber", page), zap.Error(err))
				continue
			}
			atoms[index].Image = crop
			atoms[index].Ext = ".png"
		}
	}
	return atoms
}

func groupFormulaIndexesByPage(atoms []parseAtom) map[int][]int {
	indexesByPage := make(map[int][]int)
	for index, atom := range atoms {
		if formulaNeedsScreenshot(atom) {
			indexesByPage[atom.Page] = append(indexesByPage[atom.Page], index)
		}
	}
	return indexesByPage
}

func formulaNeedsScreenshot(atom parseAtom) bool {
	return atom.ElementType == "formula" && atom.Page > 0 && len(atom.BBox) >= 4 && len(atom.Image) == 0
}

// cropFormulaImage 把 MinerU 的 [0,1000] 页面坐标映射到渲染图，并保留少量边距。
func cropFormulaImage(page image.Image, bbox []float64) ([]byte, error) {
	if len(bbox) < 4 {
		return nil, fmt.Errorf("bbox 坐标不足")
	}
	bounds := page.Bounds()
	minX := math.Min(bbox[0], bbox[2])
	minY := math.Min(bbox[1], bbox[3])
	maxX := math.Max(bbox[0], bbox[2])
	maxY := math.Max(bbox[1], bbox[3])
	scaleX := float64(bounds.Dx()) / minerUBBoxScale
	scaleY := float64(bounds.Dy()) / minerUBBoxScale

	const padding = 8
	cropBounds := image.Rect(
		clampInt(bounds.Min.X+int(math.Floor(minX*scaleX))-padding, bounds.Min.X, bounds.Max.X),
		clampInt(bounds.Min.Y+int(math.Floor(minY*scaleY))-padding, bounds.Min.Y, bounds.Max.Y),
		clampInt(bounds.Min.X+int(math.Ceil(maxX*scaleX))+padding, bounds.Min.X, bounds.Max.X),
		clampInt(bounds.Min.Y+int(math.Ceil(maxY*scaleY))+padding, bounds.Min.Y, bounds.Max.Y),
	)
	if cropBounds.Empty() {
		return nil, fmt.Errorf("bbox 超出页面范围")
	}

	cropped := image.NewRGBA(image.Rect(0, 0, cropBounds.Dx(), cropBounds.Dy()))
	draw.Draw(cropped, cropped.Bounds(), page, cropBounds.Min, draw.Src)
	var output bytes.Buffer
	if err := png.Encode(&output, cropped); err != nil {
		return nil, fmt.Errorf("编码公式截图失败: %w", err)
	}
	return output.Bytes(), nil
}

func clampInt(value, minValue, maxValue int) int {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}
