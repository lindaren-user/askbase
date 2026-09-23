//go:build windows

package parser

import (
	"context"
	"fmt"
	"image"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

// windowsPDFiumPageSize 保存 PDFium 返回的页面点尺寸。
type windowsPDFiumPageSize struct {
	Width  float32
	Height float32
}

// windowsPDFiumAPI 保存运行时动态绑定的 PDFium 函数入口。
type windowsPDFiumAPI struct {
	initLibrary         *syscall.Proc
	loadMemDocument     *syscall.Proc
	closeDocument       *syscall.Proc
	getPageCount        *syscall.Proc
	getPageSizeByIndexF *syscall.Proc
	loadPage            *syscall.Proc
	closePage           *syscall.Proc
	bitmapCreate        *syscall.Proc
	bitmapDestroy       *syscall.Proc
	renderPageBitmap    *syscall.Proc
	bitmapGetBuffer     *syscall.Proc
	bitmapGetStride     *syscall.Proc
}

var (
	windowsPDFiumOnce sync.Once
	windowsPDFiumMu   sync.Mutex
	windowsPDFium     *windowsPDFiumAPI
	windowsPDFiumErr  error
)

func init() {
	renderPDFPageImage = renderPDFPageWithWindowsPDFium
}

// renderPDFPageWithWindowsPDFium 使用本机 pdfium.dll 在进程内渲染 PDF 页面。
func renderPDFPageWithWindowsPDFium(ctx context.Context, data []byte, pageIndex int, dpi float64) (image.Image, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("PDF 数据为空")
	}

	api, err := loadWindowsPDFium()
	if err != nil {
		return nil, err
	}

	windowsPDFiumMu.Lock()
	defer windowsPDFiumMu.Unlock()
	defer runtime.KeepAlive(data)

	document, _, _ := api.loadMemDocument.Call(
		uintptr(unsafe.Pointer(&data[0])),
		uintptr(len(data)),
		0,
	)
	if document == 0 {
		return nil, fmt.Errorf("PDFium 打开 PDF 失败")
	}
	defer api.closeDocument.Call(document)

	pageCount, _, _ := api.getPageCount.Call(document)
	if pageIndex < 0 || pageIndex >= int(pageCount) {
		return nil, fmt.Errorf("PDF 页码 %d 超出范围", pageIndex+1)
	}

	var pageSize windowsPDFiumPageSize
	ok, _, _ := api.getPageSizeByIndexF.Call(
		document,
		uintptr(pageIndex),
		uintptr(unsafe.Pointer(&pageSize)),
	)
	if ok == 0 || pageSize.Width <= 0 || pageSize.Height <= 0 {
		return nil, fmt.Errorf("PDF 第 %d 页尺寸无效", pageIndex+1)
	}

	page, _, _ := api.loadPage.Call(document, uintptr(pageIndex))
	if page == 0 {
		return nil, fmt.Errorf("PDFium 打开第 %d 页失败", pageIndex+1)
	}
	defer api.closePage.Call(page)

	scale := dpi / 72.0
	pixelWidth := int(math.Round(float64(pageSize.Width) * scale))
	pixelHeight := int(math.Round(float64(pageSize.Height) * scale))
	return renderWindowsPDFiumBitmap(api, page, pixelWidth, pixelHeight)
}

// renderWindowsPDFiumBitmap 渲染页面并复制 PDFium 管理的位图缓冲。
func renderWindowsPDFiumBitmap(api *windowsPDFiumAPI, page uintptr, width, height int) (image.Image, error) {
	bitmap, _, _ := api.bitmapCreate.Call(uintptr(width), uintptr(height), 1)
	if bitmap == 0 {
		return nil, fmt.Errorf("PDFium 创建页面位图失败")
	}
	defer api.bitmapDestroy.Call(bitmap)

	strideValue, _, _ := api.bitmapGetStride.Call(bitmap)
	buffer, _, _ := api.bitmapGetBuffer.Call(bitmap)
	stride := int(strideValue)
	if buffer == 0 || stride < width*4 {
		return nil, fmt.Errorf("PDFium 页面位图缓冲区无效")
	}

	pixels := unsafe.Slice((*byte)(unsafe.Pointer(buffer)), height*stride)
	for i := range pixels {
		pixels[i] = 255
	}
	api.renderPageBitmap.Call(
		bitmap,
		page,
		0,
		0,
		uintptr(width),
		uintptr(height),
		0,
		pdfiumRenderFlags,
	)
	return pdfiumPixelsToRGBA(pixels, stride, width, height), nil
}

// loadWindowsPDFium 只执行一次 DLL 探测和符号绑定，并缓存结果。
func loadWindowsPDFium() (*windowsPDFiumAPI, error) {
	windowsPDFiumOnce.Do(func() {
		windowsPDFium, windowsPDFiumErr = openWindowsPDFium()
	})
	return windowsPDFium, windowsPDFiumErr
}

// openWindowsPDFium 按候选路径加载首个可用的 PDFium DLL。
func openWindowsPDFium() (*windowsPDFiumAPI, error) {
	var loadErrors []string
	for _, candidate := range windowsPDFiumCandidates() {
		dll, err := syscall.LoadDLL(candidate)
		if err != nil {
			loadErrors = append(loadErrors, candidate+": "+err.Error())
			continue
		}

		api, err := bindWindowsPDFium(dll)
		if err != nil {
			_ = dll.Release()
			return nil, fmt.Errorf("加载 PDFium 导出函数失败: %w", err)
		}
		api.initLibrary.Call()
		return api, nil
	}
	return nil, fmt.Errorf(
		"未找到可用的 pdfium.dll；请运行 scripts\\install-pdfium.ps1 或设置 PDFIUM_DLL。尝试结果: %s",
		strings.Join(loadErrors, "; "),
	)
}

// bindWindowsPDFium 校验并绑定渲染所需的最小 PDFium API 集合。
func bindWindowsPDFium(dll *syscall.DLL) (*windowsPDFiumAPI, error) {
	find := func(name string) (*syscall.Proc, error) {
		proc, err := dll.FindProc(name)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		return proc, nil
	}

	api := &windowsPDFiumAPI{}
	bindings := []struct {
		name   string
		target **syscall.Proc
	}{
		{"FPDF_InitLibrary", &api.initLibrary},
		{"FPDF_LoadMemDocument", &api.loadMemDocument},
		{"FPDF_CloseDocument", &api.closeDocument},
		{"FPDF_GetPageCount", &api.getPageCount},
		{"FPDF_GetPageSizeByIndexF", &api.getPageSizeByIndexF},
		{"FPDF_LoadPage", &api.loadPage},
		{"FPDF_ClosePage", &api.closePage},
		{"FPDFBitmap_Create", &api.bitmapCreate},
		{"FPDFBitmap_Destroy", &api.bitmapDestroy},
		{"FPDF_RenderPageBitmap", &api.renderPageBitmap},
		{"FPDFBitmap_GetBuffer", &api.bitmapGetBuffer},
		{"FPDFBitmap_GetStride", &api.bitmapGetStride},
	}
	for _, binding := range bindings {
		proc, err := find(binding.name)
		if err != nil {
			return nil, err
		}
		*binding.target = proc
	}
	return api, nil
}

// windowsPDFiumCandidates 返回环境变量、可执行文件和工作目录附近的 DLL 候选路径。
func windowsPDFiumCandidates() []string {
	var candidates []string
	if configured := strings.TrimSpace(os.Getenv("PDFIUM_DLL")); configured != "" {
		candidates = append(candidates, configured)
	}
	if cwd, err := os.Getwd(); err == nil {
		for _, directory := range windowsPDFiumParentDirectories(cwd) {
			candidates = append(candidates,
				filepath.Join(directory, "lib", "pdfium.dll"),
				filepath.Join(directory, "be", "lib", "pdfium.dll"),
				filepath.Join(directory, "pdfium.dll"),
			)
		}
	}
	if executable, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(executable), "pdfium.dll"))
	}
	candidates = append(candidates, "pdfium.dll")
	return uniqueWindowsPDFiumCandidates(candidates)
}

func windowsPDFiumParentDirectories(start string) []string {
	var directories []string
	directory := filepath.Clean(start)
	for {
		directories = append(directories, directory)
		parent := filepath.Dir(directory)
		if parent == directory {
			return directories
		}
		directory = parent
	}
}

func uniqueWindowsPDFiumCandidates(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		key := strings.ToLower(filepath.Clean(value))
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	return result
}
