package filetext

import (
	"bytes"
	"encoding/binary"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
)

// IsPDF 判断是否为 PDF 文件头。
func IsPDF(data []byte) bool {
	return bytes.HasPrefix(bytes.TrimSpace(data), []byte("%PDF"))
}

// Decode 把文件字节解成文本：UTF-8 / UTF-16 / GB18030（含 GBK）。
func Decode(data []byte) string {
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	if bytes.HasPrefix(data, []byte{0xFF, 0xFE}) {
		return decodeUTF16(data[2:], binary.LittleEndian)
	}
	if bytes.HasPrefix(data, []byte{0xFE, 0xFF}) {
		return decodeUTF16(data[2:], binary.BigEndian)
	}
	if utf8.Valid(data) {
		return string(data)
	}
	if out, err := simplifiedchinese.GB18030.NewDecoder().Bytes(data); err == nil && utf8.Valid(out) {
		return string(out)
	}
	return string(data)
}

func decodeUTF16(data []byte, order binary.ByteOrder) string {
	if len(data)%2 == 1 {
		data = data[:len(data)-1]
	}
	u16 := make([]uint16, len(data)/2)
	for i := range u16 {
		u16[i] = order.Uint16(data[i*2:])
	}
	return string(utf16.Decode(u16))
}
