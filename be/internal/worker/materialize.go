package worker

import (
	"context"
	"fmt"
	"path"
	"strings"

	"askbase/be/internal/model"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// splitText 按当前统一的语义分隔符策略切分正文。
func splitText(text string) []string {
	return splitRecursive(text, chunkTargetRunes, chunkOverlapRunes)
}

// materializeChunks 按模板生成分块：简历先做语义聚合，其余正文按策略切分。
// 非简历模板整表保留、图片调用视觉模型；NeedVision 缺少视觉模型时永久失败。
func (w *Worker) materializeChunks(ctx context.Context, doc model.Document, atoms []parseAtom, strategy string, vision imageDescriber) ([]model.Chunk, []string, error) {
	if strategy == model.ChunkStrategyResume {
		chunks, embedTexts := materializeResumeChunks(ctx, doc, atoms, w.resumeLLM)
		return chunks, embedTexts, nil
	}
	if strategy == model.ChunkStrategyQA {
		return materializeQAChunks(doc, atoms)
	}
	atoms = prepareAtomsForStrategy(atoms, strategy)
	standaloneImage := len(atoms) == 1 && len(atoms[0].Image) > 0 && !atoms[0].NeedVision && atoms[0].Parser == ""
	if standaloneImage && vision == nil {
		return nil, nil, fmt.Errorf("%w: 请先为知识库绑定视觉模型后再解析图片", errPermanent)
	}
	var (
		chunks     []model.Chunk
		embedTexts []string
		buf        strings.Builder
	)
	appendElement := func(content string, atom parseAtom) {
		chunks = append(chunks, newElementChunk(doc, len(chunks), content, "", atom))
		embedTexts = append(embedTexts, model.EmbedText(doc.Name, content))
	}
	flushText := func() {
		text := strings.TrimSpace(buf.String())
		buf.Reset()
		if text == "" {
			return
		}
		for _, piece := range splitText(text) {
			appendElement(piece, parseAtom{ElementType: "text"})
		}
	}
	for _, atom := range atoms {
		if len(atom.Image) > 0 {
			flushText()
			if vision == nil {
				if atom.NeedVision {
					return nil, nil, fmt.Errorf("%w: 扫描件需要视觉模型", errPermanent)
				}
				if atom.Parser == "" && strings.TrimSpace(atom.Text) == "" {
					continue
				}
			}
			chunk, embedText, err := w.imageChunk(ctx, doc, len(chunks), atom, vision)
			if err != nil {
				return nil, nil, err
			}
			chunks = append(chunks, chunk)
			embedTexts = append(embedTexts, embedText)
			continue
		}
		if atom.Table {
			flushText()
			for _, piece := range splitTableKeepHeader(atom.Text, chunkTargetRunes) {
				appendElement(withSectionPath(atom.SectionPath, piece), atom)
			}
			continue
		}
		if atom.ElementType == "image" {
			flushText()
			content := strings.TrimSpace(atom.Text)
			if content != "" {
				appendElement(withSectionPath(atom.SectionPath, content), atom)
			}
			continue
		}
		if atom.Boundary {
			flushText()
			pieces := []string{atom.Text}
			if !atom.NoSplit {
				pieces = splitText(atom.Text)
			}
			for _, piece := range pieces {
				appendElement(withSectionPath(atom.SectionPath, piece), atom)
			}
			continue
		}
		if atom.Parser != "" {
			flushText()
			for _, piece := range splitText(atom.Text) {
				appendElement(piece, atom)
			}
			continue
		}
		if buf.Len() > 0 {
			buf.WriteString("\n\n")
		}
		buf.WriteString(atom.Text)
	}
	flushText()
	return chunks, embedTexts, nil
}

// newElementChunk 创建带页面坐标和解析器来源的分块。
func newElementChunk(doc model.Document, index int, content, imageKey string, atom parseAtom) model.Chunk {
	elementType := strings.TrimSpace(atom.ElementType)
	if elementType == "" {
		elementType = "text"
	}
	return model.Chunk{
		ID:          chunkIDFromContent(doc.ID, index, content+imageKey),
		DocumentID:  doc.ID,
		DatasetID:   doc.DatasetID,
		ChunkIndex:  index,
		Content:     content,
		TokenNum:    estimateTokens(content),
		Enabled:     true,
		ImageKey:    imageKey,
		ElementType: elementType,
		PageNumber:  atom.Page,
		BBox:        model.Float64Array(atom.BBox),
		ParserName:  atom.Parser,
	}
}

// imageChunk 调用视觉模型补充图片描述，保存原图并构造可检索分块。
func (w *Worker) imageChunk(ctx context.Context, doc model.Document, index int, atom parseAtom, vision imageDescriber) (model.Chunk, string, error) {
	caption := strings.TrimSpace(atom.Text)
	desc := ""
	if vision != nil && atom.ElementType != "formula" {
		prompt := ""
		if caption != "" {
			prompt = "结合图片已有标题或说明，转录图中全部文字并准确描述图表、流程或关键数据。已有说明：\n" + caption
		}
		var err error
		desc, err = vision.DescribeImage(ctx, mimeFromExt(atom.Ext), atom.Image, prompt)
		if err != nil {
			// MinerU 已完成主体解析时，视觉补充失败不应阻断整份文档。
			if atom.Parser != "" && !atom.NeedVision {
				zap.L().Warn("MinerU 图片视觉补充失败，保留已有解析结果",
					zap.Int64("documentId", doc.ID),
					zap.Int("pageNumber", atom.Page),
					zap.Error(err),
				)
				desc = ""
			} else {
				return model.Chunk{}, "", ioError(err, "视觉模型识别图片失败")
			}
		}
	}
	desc = strings.TrimSpace(desc)
	parts := make([]string, 0, 2)
	if caption != "" {
		parts = append(parts, caption)
	}
	if desc != "" && desc != caption {
		parts = append(parts, desc)
	}
	if len(parts) == 0 {
		parts = append(parts, "（无文字描述）")
	}
	content := strings.Join(parts, "\n")
	if atom.ElementType != "formula" {
		content = "[图片] " + content
	}
	content = withSectionPath(atom.SectionPath, content)
	ext := atom.Ext
	if ext == "" {
		ext = ".png"
	}
	key := fmt.Sprintf("%s/images/%s%s", strings.TrimSuffix(doc.R2Key, path.Ext(doc.R2Key)), uuid.NewString(), ext)
	if err := w.objects.Put(ctx, key, atom.Image, mimeFromExt(ext)); err != nil {
		return model.Chunk{}, "", ioError(err, "保存图片失败")
	}
	chunk := newElementChunk(doc, index, content, key, atom)
	return chunk, model.EmbedText(doc.Name, content), nil
}

// mimeFromExt 返回支持图片扩展名对应的 MIME 类型，未知格式按 PNG 处理。
func mimeFromExt(ext string) string {
	switch strings.ToLower(ext) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".bmp":
		return "image/bmp"
	default:
		return "image/png"
	}
}
