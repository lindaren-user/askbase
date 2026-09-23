package worker

import (
	"context"
	"fmt"
	"path"
	"strings"
	"unicode"

	"askbase/be/internal/filetext"
	"askbase/be/internal/model"

	"go.uber.org/zap"
)

const minerUReasonBookImages = "书籍包含内嵌图片"

// fetchAndParse 下载对象，并按文件格式和知识库模板拆成解析原子。
func (w *Worker) fetchAndParse(ctx context.Context, doc model.Document, strategy string) ([]parseAtom, error) {
	data, err := w.objects.Get(ctx, doc.R2Key)
	if err != nil {
		return nil, ioError(err, "拉取文档失败")
	}
	ext := strings.ToLower(path.Ext(doc.Name))
	switch ext {
	case ".txt":
		text := strings.ReplaceAll(filetext.Decode(data), "\r\n", "\n")
		if strategy == model.ChunkStrategyQA {
			return parseQADelimited(text, false)
		}
		return textAtoms(text), nil
	case ".csv":
		text := strings.ReplaceAll(filetext.Decode(data), "\r\n", "\n")
		if strategy != model.ChunkStrategyQA {
			return nil, fmt.Errorf("%w: CSV 仅支持问答对模板", errPermanent)
		}
		return parseQADelimited(text, true)
	case ".md", ".markdown":
		text := strings.ReplaceAll(filetext.Decode(data), "\r\n", "\n")
		return extractMarkdownAtoms(text), nil
	case ".pdf":
		return w.parsePDFByStrategy(ctx, doc, data, strategy)
	case ".docx":
		return parseDocx(data)
	case ".xlsx":
		if strategy == model.ChunkStrategyQA {
			return parseQAXlsx(data)
		}
		return parseXlsx(data)
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp":
		return []parseAtom{{Image: data, Ext: ext}}, nil
	default:
		return nil, fmt.Errorf("%w: 不支持的文件类型 %s，请上传 txt/csv/md/pdf/docx/xlsx/图片", errPermanent, ext)
	}
}

// parsePDFByStrategy 根据领域模板和本地探测结果选择轻量解析或 MinerU。
// 论文固定走结构化解析；书籍遇到内嵌图片时升级；其余模板仅在文本层不可用时升级。
func (w *Worker) parsePDFByStrategy(ctx context.Context, doc model.Document, data []byte, strategy string) ([]parseAtom, error) {
	if strategy == model.ChunkStrategyPaper {
		if w.mineru == nil {
			return nil, fmt.Errorf("%w: 论文模板需要启用 MinerU 以保留图表、公式与版面结构", errPermanent)
		}
		return w.parsePDFWithMinerU(ctx, doc, data, strategy, "论文模板")
	}

	localAtoms, localErr := parsePDF(data)
	routeReason := minerUReason(strategy, data, localAtoms, localErr)
	if routeReason != "" {
		if w.mineru != nil {
			return w.parsePDFWithMinerU(ctx, doc, data, strategy, routeReason)
		}
		if localErr == nil && (atomsTextIsPoor(localAtoms) || routeReason == minerUReasonBookImages) {
			return nil, fmt.Errorf("%w: %s，需要启用 MinerU", errPermanent, routeReason)
		}
	}
	if localErr != nil {
		return nil, localErr
	}
	zap.L().Info("PDF 使用本地解析器",
		zap.Int64("documentId", doc.ID),
		zap.String("fileName", doc.Name),
		zap.String("strategy", strategy),
	)
	return localAtoms, nil
}

// shouldUseMinerU 判断本地 PDF 结果是否不足以支撑当前模板。
func shouldUseMinerU(strategy string, data []byte, atoms []parseAtom, localErr error) bool {
	return minerUReason(strategy, data, atoms, localErr) != ""
}

// minerUReason 返回本地 PDF 结果需要升级 MinerU 的原因，空串表示可继续使用本地结果。
func minerUReason(strategy string, data []byte, atoms []parseAtom, localErr error) string {
	switch {
	case localErr != nil:
		return "本地文本层不可用"
	case atomsNeedVision(atoms):
		return "检测到扫描页"
	case atomsTextIsPoor(atoms):
		return "本地文本层存在乱码"
	case strategy == model.ChunkStrategyBook && hasPDFImages(data):
		return minerUReasonBookImages
	default:
		return ""
	}
}

// atomsNeedVision 判断解析结果中是否存在必须通过视觉模型识别的页面。
func atomsNeedVision(atoms []parseAtom) bool {
	for _, atom := range atoms {
		if atom.NeedVision {
			return true
		}
	}
	return false
}

// atomsTextIsPoor 检测字体映射失败产生的替换字符和控制字符，避免把乱码送入分块。
func atomsTextIsPoor(atoms []parseAtom) bool {
	var total, invalid int
	for _, atom := range atoms {
		for _, r := range atom.Text {
			if unicode.IsSpace(r) {
				continue
			}
			total++
			if r == unicode.ReplacementChar || unicode.IsControl(r) {
				invalid++
			}
		}
	}
	return invalid >= 3 && invalid*100 >= total
}

// parsePDFWithMinerU 执行在线结构化解析并补充公式截图。
func (w *Worker) parsePDFWithMinerU(ctx context.Context, doc model.Document, data []byte, strategy, reason string) ([]parseAtom, error) {
	zap.L().Info("PDF 使用 MinerU 在线解析",
		zap.Int64("documentId", doc.ID),
		zap.String("fileName", doc.Name),
		zap.String("strategy", strategy),
		zap.String("reason", reason),
		zap.String("modelVersion", w.mineru.model),
	)
	atoms, err := w.mineru.Parse(ctx, doc.Name, data)
	if err != nil {
		return nil, err
	}
	return attachPDFFormulaScreenshots(ctx, data, atoms), nil
}
