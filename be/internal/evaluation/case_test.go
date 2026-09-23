package evaluation

import (
	"strings"
	"testing"
)

func TestDecodeCases(t *testing.T) {
	input := `
{"id":"hello","question":"你好","expectedRoute":"none","history":[],"relevantEvidence":[]}
{"id":"fact","datasetId":7,"question":"什么时候上线？","expectedRoute":"direct","history":[],"relevantEvidence":[{"documentName":"说明.pdf","contains":["2024年"]}]}
`
	cases, err := DecodeCases(strings.NewReader(input))
	if err != nil {
		t.Fatalf("DecodeCases() error = %v", err)
	}
	if len(cases) != 2 {
		t.Fatalf("len(cases) = %d, want 2", len(cases))
	}
	if cases[1].RelevantEvidence[0].DocumentName != "说明.pdf" {
		t.Fatalf("unexpected evidence: %#v", cases[1].RelevantEvidence)
	}
}

func TestDecodeCasesRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "duplicate id", input: "{\"id\":\"a\",\"question\":\"q\",\"expectedRoute\":\"none\"}\n{\"id\":\"a\",\"question\":\"q\",\"expectedRoute\":\"none\"}"},
		{name: "unknown route", input: `{"id":"a","question":"q","expectedRoute":"hyde"}`},
		{name: "missing evidence declaration", input: `{"id":"a","datasetId":1,"question":"q","expectedRoute":"direct"}`},
		{name: "conflicting evidence", input: `{"id":"a","datasetId":1,"question":"q","expectedRoute":"direct","expectNoEvidence":true,"relevantEvidence":[{"documentName":"a","contains":["b"]}]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := DecodeCases(strings.NewReader(tt.input)); err == nil {
				t.Fatal("DecodeCases() error = nil, want error")
			}
		})
	}
}
