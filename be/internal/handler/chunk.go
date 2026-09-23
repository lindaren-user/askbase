package handler

import (
	"net/http"

	"askbase/be/internal/middleware"
	"askbase/be/internal/model"
	"askbase/be/pkg/resp"
)

// HandleListChunks 文档分块列表。
func (h *Handler) HandleListChunks(w http.ResponseWriter, req *http.Request) {
	user := middleware.CurrentUser(req)
	documentID, ok := pathID(w, req, "documentId")
	if !ok {
		return
	}
	items, err := h.services.Chunks.ListByDocument(req.Context(), user.ID, documentID)
	if h.writeServiceErr(w, err, "文档不存在", "", "查询分块列表失败") {
		return
	}
	resp.OK(w, items)
}

// HandleUpdateChunk 更新分块启停或内容。
func (h *Handler) HandleUpdateChunk(w http.ResponseWriter, req *http.Request) {
	user := middleware.CurrentUser(req)
	id, ok := pathID(w, req, "chunkId")
	if !ok {
		return
	}
	var body model.UpdateChunkRequest
	if !resp.Decode(w, req, &body, "请求格式错误") {
		return
	}
	err := h.services.Chunks.Update(req.Context(), user.ID, id, body)
	if h.writeServiceErr(w, err, "分块不存在", "更新分块参数错误", "更新分块失败") {
		return
	}
	resp.OK(w, nil)
}

// HandleRetrievalTest 检索测试：只召回，不调 LLM。
func (h *Handler) HandleRetrievalTest(w http.ResponseWriter, req *http.Request) {
	user := middleware.CurrentUser(req)
	var body model.RetrievalTestRequest
	if !resp.Decode(w, req, &body, "请求格式错误") {
		return
	}
	hits, err := h.services.Retrieval.Search(req.Context(), user.ID, body.DatasetID, body.Query, body.TopK, body.MinScore)
	if h.writeServiceErr(w, err, "知识库不存在", "检索参数错误", "检索失败") {
		return
	}
	resp.OK(w, hits)
}
