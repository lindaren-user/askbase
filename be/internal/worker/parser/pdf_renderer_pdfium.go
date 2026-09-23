//go:build linux && cgo

package parser

/*
#cgo LDFLAGS: -lm -lpthread -ldl

#include <stdint.h>
#include <stdlib.h>

typedef struct FPDF_DOCUMENT__ { int unused; } *FPDF_DOCUMENT;
typedef struct FPDF_PAGE__ { int unused; } *FPDF_PAGE;
typedef struct FPDF_BITMAP__ { int unused; } *FPDF_BITMAP;

extern void          FPDF_InitLibrary(void);
extern FPDF_DOCUMENT FPDF_LoadMemDocument(const void* data_buf, int size, const char* password);
extern void          FPDF_CloseDocument(FPDF_DOCUMENT document);
extern int           FPDF_GetPageCount(FPDF_DOCUMENT document);
extern FPDF_PAGE     FPDF_LoadPage(FPDF_DOCUMENT document, int page_index);
extern void          FPDF_ClosePage(FPDF_PAGE page);
extern double        FPDF_GetPageWidth(FPDF_PAGE page);
extern double        FPDF_GetPageHeight(FPDF_PAGE page);
extern FPDF_BITMAP   FPDFBitmap_Create(int width, int height, int alpha);
extern void          FPDFBitmap_Destroy(FPDF_BITMAP bitmap);
extern void          FPDF_RenderPageBitmap(FPDF_BITMAP bitmap, FPDF_PAGE page,
                       int start_x, int start_y, int size_x, int size_y,
                       int rotate, int flags);
extern void*         FPDFBitmap_GetBuffer(FPDF_BITMAP bitmap);
extern int           FPDFBitmap_GetStride(FPDF_BITMAP bitmap);
*/
import "C"

import (
	"context"
	"fmt"
	"image"
	"math"
	"sync"
	"unsafe"
)

var (
	pdfiumInitOnce sync.Once
	pdfiumMu       sync.Mutex
)

func init() {
	renderPDFPageImage = renderPDFPageWithPDFium
}

// renderPDFPageWithPDFium 在进程内把 PDF 页渲染为 RGBA 图片。
func renderPDFPageWithPDFium(ctx context.Context, data []byte, pageIndex int, dpi float64) (image.Image, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("PDF 数据为空")
	}

	pdfiumMu.Lock()
	defer pdfiumMu.Unlock()
	pdfiumInitOnce.Do(func() { C.FPDF_InitLibrary() })

	cData := C.CBytes(data)
	defer C.free(cData)
	document := C.FPDF_LoadMemDocument(cData, C.int(len(data)), nil)
	if document == nil {
		return nil, fmt.Errorf("PDFium 打开 PDF 失败")
	}
	defer C.FPDF_CloseDocument(document)
	if pageIndex < 0 || pageIndex >= int(C.FPDF_GetPageCount(document)) {
		return nil, fmt.Errorf("PDF 页码 %d 超出范围", pageIndex+1)
	}

	page := C.FPDF_LoadPage(document, C.int(pageIndex))
	if page == nil {
		return nil, fmt.Errorf("PDFium 打开第 %d 页失败", pageIndex+1)
	}
	defer C.FPDF_ClosePage(page)
	width := float64(C.FPDF_GetPageWidth(page))
	height := float64(C.FPDF_GetPageHeight(page))
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("PDF 第 %d 页尺寸无效", pageIndex+1)
	}

	scale := dpi / 72.0
	pixelWidth := int(math.Round(width * scale))
	pixelHeight := int(math.Round(height * scale))
	bitmap := C.FPDFBitmap_Create(C.int(pixelWidth), C.int(pixelHeight), 1)
	if bitmap == nil {
		return nil, fmt.Errorf("PDFium 创建页面位图失败")
	}
	defer C.FPDFBitmap_Destroy(bitmap)

	stride := int(C.FPDFBitmap_GetStride(bitmap))
	buffer := C.FPDFBitmap_GetBuffer(bitmap)
	pixels := unsafe.Slice((*byte)(buffer), pixelHeight*stride)
	for i := range pixels {
		pixels[i] = 255
	}
	C.FPDF_RenderPageBitmap(
		bitmap,
		page,
		0,
		0,
		C.int(pixelWidth),
		C.int(pixelHeight),
		0,
		pdfiumRenderFlags,
	)
	return pdfiumPixelsToRGBA(pixels, stride, pixelWidth, pixelHeight), nil
}
