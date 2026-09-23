package evaluation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"askbase/be/internal/service"
)

func TestWriteReports(t *testing.T) {
	dir := t.TempDir()
	report := Report{
		GeneratedAt: time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC),
		Summary: Summary{
			Cases:    1,
			Attempts: 1,
			ByRoute: map[service.QueryType]GroupSummary{
				service.QueryTypeNone: {},
			},
		},
		Results: []CaseResult{{
			Run:           1,
			CaseID:        "case-1",
			ExpectedRoute: service.QueryTypeDirect,
			ActualRoute:   service.QueryTypeNone,
		}},
	}
	if err := WriteReports(dir, report); err != nil {
		t.Fatalf("WriteReports() error = %v", err)
	}
	for _, name := range []string{"report.json", "report.md"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
	}
	markdown, err := os.ReadFile(filepath.Join(dir, "report.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(markdown), "case-1") || !strings.Contains(string(markdown), "路由准确率") {
		t.Fatalf("unexpected markdown report: %s", markdown)
	}
}
