package worker

import (
	"context"
	"strings"
	"testing"

	"askbase/be/internal/model"
)

func TestMaterializeScanRequiresVisionModel(t *testing.T) {
	w := &Worker{}
	doc := model.Document{ID: 1, DatasetID: 2, Name: "scan.pdf"}
	atoms := []parseAtom{{Image: []byte("image"), Ext: ".jpg", NeedVision: true}}

	_, _, err := w.materializeChunks(context.Background(), doc, atoms, model.ChunkStrategyGeneral, nil)
	if err == nil || !strings.Contains(err.Error(), "扫描件需要视觉模型") {
		t.Fatalf("want vision required, got %v", err)
	}
}
