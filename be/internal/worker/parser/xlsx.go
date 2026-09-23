package parser

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/xuri/excelize/v2"
)

const xlsxChunkRows = 256

// parseXlsx 按 sheet 读行列，每 256 行一段 markdown 表格。
func parseXlsx(data []byte) ([]parseAtom, error) {
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%w: XLSX 解析失败: %v", errPermanent, err)
	}
	defer func() { _ = f.Close() }()

	var atoms []parseAtom
	for _, sheet := range f.GetSheetList() {
		rows, err := f.GetRows(sheet)
		if err != nil || len(rows) == 0 {
			continue
		}
		header := rows[0]
		body := rows[1:]
		if len(body) == 0 {
			md := markdownTable(header, nil)
			if md == "" {
				continue
			}
			atoms = append(atoms, tableAtom(sheet+"\n\n"+md))
			continue
		}
		for start := 0; start < len(body); start += xlsxChunkRows {
			end := start + xlsxChunkRows
			if end > len(body) {
				end = len(body)
			}
			md := markdownTable(header, body[start:end])
			if md == "" {
				continue
			}
			var title strings.Builder
			title.WriteString(sheet)
			if len(body) > xlsxChunkRows {
				title.WriteString(fmt.Sprintf("（%d–%d 行）", start+1, end))
			}
			atoms = append(atoms, tableAtom(title.String()+"\n\n"+md))
		}
	}
	if len(atoms) == 0 {
		return nil, fmt.Errorf("%w: XLSX 内容为空", errPermanent)
	}
	return atoms, nil
}
