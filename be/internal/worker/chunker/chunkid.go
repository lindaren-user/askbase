package chunker

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
)

// IDFromContent 由内容与文档 ID 生成稳定主键：同一文档同一正文得到同一 ID。
func IDFromContent(documentID int64, index int, content string) int64 {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d:%d\n%s", documentID, index, content)))
	id := int64(binary.BigEndian.Uint64(sum[:8]) & 0x7FFFFFFFFFFFFFFF)
	if id == 0 {
		return 1
	}
	return id
}
