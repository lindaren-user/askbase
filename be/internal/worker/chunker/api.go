package chunker

// SplitRecursive 按语义分隔符递归切分文本，并保留指定重叠。
func SplitRecursive(text string, target, overlap int) []string {
	return splitRecursive(text, target, overlap)
}

// EstimateTokens 粗略估算文本 token 数量。
func EstimateTokens(text string) int {
	return estimateTokens(text)
}
