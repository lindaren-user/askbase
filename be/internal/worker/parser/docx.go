package parser

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"regexp"
	"strconv"
	"strings"
)

const (
	docxDocumentXML = "word/document.xml"
	docxRelsXML     = "word/_rels/document.xml.rels"
)

var docxHeadingStylePattern = regexp.MustCompile(`(?i)(?:heading|标题)[ _-]?([1-6])$`)

// parseDocx 确定性读取 zip+XML：w:p 段落、w:tbl 表格、内嵌图按出现顺序插入。
func parseDocx(data []byte) ([]parseAtom, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("%w: DOCX 不是有效的 zip: %v", errPermanent, err)
	}
	files := zipFileIndex(zr)
	docFile, ok := files[docxDocumentXML]
	if !ok {
		return nil, fmt.Errorf("%w: DOCX 缺少 word/document.xml", errPermanent)
	}
	rels := readDocxRels(files)
	media := readDocxMedia(files, rels)

	rc, err := docFile.Open()
	if err != nil {
		return nil, fmt.Errorf("%w: 打开 DOCX 正文失败: %v", errPermanent, err)
	}
	defer rc.Close()

	dec := xml.NewDecoder(rc)
	inBody := false
	var atoms []parseAtom
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%w: 解析 DOCX XML 失败: %v", errPermanent, err)
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch se.Name.Local {
		case "body":
			inBody = true
		case "p":
			if inBody {
				atoms = append(atoms, consumeDocxParagraph(dec, se, media)...)
			}
		case "tbl":
			if inBody {
				if t := consumeDocxTable(dec, se); t != "" {
					atoms = append(atoms, tableAtom(t))
				}
			}
		}
	}
	if len(atoms) == 0 {
		return nil, fmt.Errorf("%w: DOCX 内容为空", errPermanent)
	}
	return atoms, nil
}

// consumeDocxParagraph 保留段落内文字、换行和图片的原始顺序，并记录标题层级。
func consumeDocxParagraph(dec *xml.Decoder, start xml.StartElement, media map[string]parseAtom) []parseAtom {
	var atoms []parseAtom
	var text strings.Builder
	headingLevel := 0
	flushText := func() {
		s := strings.TrimSpace(text.String())
		text.Reset()
		if s != "" {
			atom := parseAtom{Text: s, HeadingLevel: headingLevel}
			if headingLevel > 0 {
				atom.ElementType = "title"
			}
			atoms = append(atoms, atom)
		}
	}
	depth := 1
	for depth > 0 {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "pStyle":
				headingLevel = docxHeadingLevel(t)
				_ = dec.DecodeElement(new(struct{}), &t)
			case "t":
				var s string
				if err := dec.DecodeElement(&s, &t); err == nil {
					text.WriteString(s)
				}
			case "tab":
				text.WriteByte('\t')
				_ = dec.DecodeElement(new(struct{}), &t)
			case "br":
				text.WriteByte('\n')
				_ = dec.DecodeElement(new(struct{}), &t)
			case "blip":
				id := blipEmbed(t)
				_ = dec.DecodeElement(new(struct{}), &t)
				if img, ok := media[id]; ok {
					flushText()
					atoms = append(atoms, img)
				}
			default:
				depth++
			}
		case xml.EndElement:
			if t.Name.Local == start.Name.Local && depth == 1 {
				depth = 0
				continue
			}
			depth--
		}
	}
	flushText()
	return atoms
}

// docxHeadingLevel 从段落样式 Heading1/标题1 中读取标题层级。
func docxHeadingLevel(element xml.StartElement) int {
	for _, attribute := range element.Attr {
		if attribute.Name.Local != "val" {
			continue
		}
		match := docxHeadingStylePattern.FindStringSubmatch(strings.TrimSpace(attribute.Value))
		if len(match) != 2 {
			return 0
		}
		level, _ := strconv.Atoi(match[1])
		return level
	}
	return 0
}

// consumeDocxTable 读取表格行列并转换为 Markdown 表格。
func consumeDocxTable(dec *xml.Decoder, start xml.StartElement) string {
	var rows [][]string
	var currentRow []string
	var cell strings.Builder
	inCell := false
	depth := 1
	for depth > 0 {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "tr":
				currentRow = nil
			case "tc":
				inCell = true
				cell.Reset()
			case "t":
				var s string
				if err := dec.DecodeElement(&s, &t); err == nil && inCell {
					if cell.Len() > 0 {
						cell.WriteByte(' ')
					}
					cell.WriteString(s)
				}
			case "br":
				if inCell {
					cell.WriteByte('\n')
				}
			default:
				depth++
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "tc":
				currentRow = append(currentRow, strings.TrimSpace(cell.String()))
				inCell = false
			case "tr":
				if len(currentRow) > 0 {
					rows = append(rows, currentRow)
				}
			case start.Name.Local:
				if depth == 1 {
					depth = 0
					continue
				}
				depth--
			default:
				depth--
			}
		}
	}
	if len(rows) == 0 {
		return ""
	}
	header := rows[0]
	body := rows[1:]
	return markdownTable(header, body)
}

// blipEmbed 读取 DrawingML 图片节点引用的关系 ID。
func blipEmbed(se xml.StartElement) string {
	for _, a := range se.Attr {
		if a.Name.Local == "embed" {
			return a.Value
		}
	}
	return ""
}

// docxRelationship 描述 document.xml.rels 中的一条资源映射。
type docxRelationship struct {
	ID     string `xml:"Id,attr"`
	Type   string `xml:"Type,attr"`
	Target string `xml:"Target,attr"`
}

// docxRelationships 对应 DOCX 关系文件的 XML 根节点。
type docxRelationships struct {
	Items []docxRelationship `xml:"Relationship"`
}

// readDocxRels 读取关系 ID 到包内资源路径的映射。
func readDocxRels(files map[string]*zip.File) map[string]string {
	f, ok := files[docxRelsXML]
	if !ok {
		return nil
	}
	rc, err := f.Open()
	if err != nil {
		return nil
	}
	defer rc.Close()
	var rels docxRelationships
	if err := xml.NewDecoder(rc).Decode(&rels); err != nil {
		return nil
	}
	out := make(map[string]string, len(rels.Items))
	for _, r := range rels.Items {
		if !strings.Contains(r.Type, "/image") {
			continue
		}
		target := zipPath(r.Target)
		target = strings.TrimLeft(target, "/")
		if !strings.HasPrefix(target, "word/") {
			target = path.Join("word", target)
		}
		out[r.ID] = target
	}
	return out
}

// readDocxMedia 加载关系表引用的受支持栅格图片。
func readDocxMedia(files map[string]*zip.File, rels map[string]string) map[string]parseAtom {
	out := make(map[string]parseAtom, len(rels))
	for id, target := range rels {
		f, ok := files[target]
		if !ok {
			continue
		}
		ext := strings.ToLower(path.Ext(target))
		if !isRasterImageExt(ext) {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			continue
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil || len(data) == 0 {
			continue
		}
		out[id] = parseAtom{Image: data, Ext: ext}
	}
	return out
}

func zipPath(name string) string {
	return strings.ToLower(strings.ReplaceAll(name, "\\", "/"))
}

func zipFileIndex(zr *zip.Reader) map[string]*zip.File {
	files := make(map[string]*zip.File, len(zr.File))
	for _, f := range zr.File {
		files[zipPath(f.Name)] = f
	}
	return files
}

func isRasterImageExt(ext string) bool {
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".tif", ".tiff":
		return true
	default:
		return false
	}
}
