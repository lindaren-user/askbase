package chunker

import (
	"strings"
	"unicode/utf8"
)

// DefaultTargetRunes 是默认分块目标长度，按 rune 计。
const DefaultTargetRunes = 512

// DefaultOverlapRunes 是相邻分块默认重叠长度，按 rune 计。
const DefaultOverlapRunes = 50

// recursiveSeparators 递归切分分隔符，从粗到细。
var recursiveSeparators = []string{"\n\n", "\n", "。", "！", "？", "；", "，", " ", ""}

// splitRecursive 递归切分文本为不超过 target runes 的块，块间保留 overlap 重叠。
// 优先按粗分隔符聚合，超长再降级细分隔符，最后按 rune 硬切。
func splitRecursive(text string, target, overlap int) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if utf8.RuneCountInString(text) <= target {
		return []string{text}
	}
	return splitWithSeparators(text, recursiveSeparators, target, overlap)
}

// splitWithSeparators 用首层分隔符切分并聚合；超长块递归用下一层分隔符。
func splitWithSeparators(text string, seps []string, target, overlap int) []string {
	sep := seps[0]
	var pieces []string
	if sep == "" {
		pieces = hardSplit(text, target)
	} else {
		pieces = splitKeepSeparator(text, sep)
	}

	var chunks []string
	var current strings.Builder
	flush := func() {
		part := strings.TrimSpace(current.String())
		current.Reset()
		if part == "" {
			return
		}
		if utf8.RuneCountInString(part) > target && len(seps) > 1 {
			// 单块仍超长：递归用更细的分隔符
			chunks = append(chunks, splitWithSeparators(part, seps[1:], target, overlap)...)
			return
		}
		chunks = append(chunks, part)
	}

	for _, piece := range pieces {
		candidate := current.String() + piece
		if utf8.RuneCountInString(candidate) > target && current.Len() > 0 {
			flush()
			if overlap > 0 && len(chunks) > 0 {
				current.WriteString(tailRunes(chunks[len(chunks)-1], overlap))
			}
		}
		current.WriteString(piece)
	}
	flush()
	return chunks
}

// splitKeepSeparator 按分隔符切分，分隔符保留在片段尾部，避免语义截断。
func splitKeepSeparator(text, sep string) []string {
	parts := strings.Split(text, sep)
	pieces := make([]string, 0, len(parts))
	for i, part := range parts {
		if i < len(parts)-1 {
			pieces = append(pieces, part+sep)
		} else if part != "" {
			pieces = append(pieces, part)
		}
	}
	return pieces
}

// hardSplit 无分隔符可用时按 rune 硬切。
func hardSplit(text string, target int) []string {
	runes := []rune(text)
	var chunks []string
	for start := 0; start < len(runes); start += target {
		end := start + target
		if end > len(runes) {
			end = len(runes)
		}
		chunks = append(chunks, string(runes[start:end]))
	}
	return chunks
}

// tailRunes 取文本末尾 n 个 rune，用于块间重叠。
func tailRunes(text string, n int) string {
	runes := []rune(text)
	if len(runes) <= n {
		return text
	}
	return string(runes[len(runes)-n:])
}

// estimateTokens 粗略估算 token 数：中文按字、英文按词近似。
func estimateTokens(text string) int {
	runes := utf8.RuneCountInString(text)
	return runes/2 + 1
}
