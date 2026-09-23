package worker

import (
	"encoding/base64"
	"strings"
)

const previewImageMaxBytes = 400 << 10

// ExtractDocxText 提取 DOCX 的可检索纯文本，供文档预览和服务层复用。
func ExtractDocxText(data []byte) (string, error) {
	atoms, err := parseDocx(data)
	if err != nil {
		return "", err
	}
	return joinTextAtoms(atoms, "\n\n"), nil
}

// ExtractDocxPreview 提取 DOCX 预览内容，并以内嵌 Markdown 保留尺寸受控的图片。
func ExtractDocxPreview(data []byte) (string, error) {
	atoms, err := parseDocx(data)
	if err != nil {
		return "", err
	}
	return joinPreviewAtoms(atoms), nil
}

// ExtractXlsxText 将 XLSX 工作表转换为 Markdown 表格文本。
func ExtractXlsxText(data []byte) (string, error) {
	atoms, err := parseXlsx(data)
	if err != nil {
		return "", err
	}
	return joinTextAtoms(atoms, "\n\n"), nil
}

// joinPreviewAtoms 按文档顺序拼接正文、表格和可预览图片。
func joinPreviewAtoms(atoms []parseAtom) string {
	parts := make([]string, 0, len(atoms))
	for _, a := range atoms {
		if len(a.Image) > 0 {
			parts = append(parts, previewImageMarkdown(a))
			continue
		}
		text := strings.TrimSpace(a.Text)
		if text == "" {
			continue
		}
		parts = append(parts, text)
	}
	return strings.Join(parts, "\n\n")
}

// previewImageMarkdown 将小型图片编码为预览用 data URI，超限图片只保留说明。
func previewImageMarkdown(a parseAtom) string {
	if len(a.Image) == 0 || len(a.Image) > previewImageMaxBytes {
		return "*[图片]*"
	}
	return "![](data:" + mimeFromExt(a.Ext) + ";base64," + base64.StdEncoding.EncodeToString(a.Image) + ")"
}
