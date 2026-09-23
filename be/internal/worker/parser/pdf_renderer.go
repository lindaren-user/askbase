package parser

import (
	"context"
	"fmt"
	"image"
	"image/color"
)

const (
	formulaRenderDPI  = 144
	pdfiumRenderFlags = 0x01 | 0x02
)

var renderPDFPageImage = unsupportedPDFRenderer

// unsupportedPDFRenderer 是未启用平台 PDFium 实现时的显式失败占位。
func unsupportedPDFRenderer(_ context.Context, _ []byte, _ int, _ float64) (image.Image, error) {
	return nil, fmt.Errorf("当前构建未启用 PDFium")
}

// pdfiumPixelsToRGBA 将 PDFium 的 BGRA 行缓冲转换为 Go RGBA 图像。
func pdfiumPixelsToRGBA(pixels []byte, stride, width, height int) *image.RGBA {
	result := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			offset := y*stride + x*4
			result.SetRGBA(x, y, color.RGBA{
				R: pixels[offset+2],
				G: pixels[offset+1],
				B: pixels[offset],
				A: 255,
			})
		}
	}
	return result
}
