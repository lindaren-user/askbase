package worker

import (
	"context"

	"askbase/be/internal/worker/chunker"
	documentparser "askbase/be/internal/worker/parser"
)

type parseAtom = documentparser.Atom

const (
	chunkTargetRunes  = chunker.DefaultTargetRunes
	chunkOverlapRunes = chunker.DefaultOverlapRunes
)

var errPDFEmpty = documentparser.ErrPDFEmpty

func textAtoms(text string) []parseAtom {
	return documentparser.TextAtoms(text)
}

func tableAtom(text string) parseAtom {
	return documentparser.TableAtom(text)
}

func joinTextAtoms(atoms []parseAtom, separator string) string {
	return documentparser.JoinTextAtoms(atoms, separator)
}

func parseDocx(data []byte) ([]parseAtom, error) {
	return documentparser.ParseDocx(data)
}

func extractMarkdownAtoms(text string) []parseAtom {
	return documentparser.ParseMarkdown(text)
}

func parsePDF(data []byte) ([]parseAtom, error) {
	return documentparser.ParsePDF(data)
}

func hasPDFImages(data []byte) bool {
	return documentparser.HasPDFImages(data)
}

func attachPDFFormulaScreenshots(ctx context.Context, data []byte, atoms []parseAtom) []parseAtom {
	return documentparser.AttachPDFFormulaScreenshots(ctx, data, atoms)
}

func parseXlsx(data []byte) ([]parseAtom, error) {
	return documentparser.ParseXlsx(data)
}

func splitTableKeepHeader(text string, target int) []string {
	return documentparser.SplitTableKeepHeader(text, target)
}

func markdownTableCells(line string) []string {
	return documentparser.MarkdownTableCells(line)
}

func isMarkdownTableSeparator(line string) bool {
	return documentparser.IsMarkdownTableSeparator(line)
}

func splitRecursive(text string, target, overlap int) []string {
	return chunker.SplitRecursive(text, target, overlap)
}

func estimateTokens(text string) int {
	return chunker.EstimateTokens(text)
}

func chunkIDFromContent(documentID int64, index int, content string) int64 {
	return chunker.IDFromContent(documentID, index, content)
}
