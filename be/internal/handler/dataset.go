package handler

import (
	"net/http"
	"strconv"

	"askbase/be/internal/middleware"
	"askbase/be/internal/model"
	"askbase/be/pkg/resp"

	"github.com/go-chi/chi/v5"
)

// pathID 解析路径参数为 int64；非法时写出 400 并返回 false。
func pathID(w http.ResponseWriter, req *http.Request, name string) (int64, bool) {
	raw := chi.URLParam(req, name)
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		resp.Fail(w, http.StatusBadRequest, "参数错误", err)
		return 0, false
	}
	return id, true
}

// HandleListDatasets 知识库列表。
func (h *Handler) HandleListDatasets(w http.ResponseWriter, req *http.Request) {
	user := middleware.CurrentUser(req)
	items, err := h.services.Datasets.List(req.Context(), user.ID)
	if err != nil {
		resp.Fail(w, http.StatusInternalServerError, "查询知识库列表失败", err)
		return
	}
	resp.OK(w, items)
}

// HandleCreateDataset 新建知识库。
func (h *Handler) HandleCreateDataset(w http.ResponseWriter, req *http.Request) {
	user := middleware.CurrentUser(req)
	var body model.CreateDatasetRequest
	if !resp.Decode(w, req, &body, "请求格式错误") {
		return
	}
	ds, err := h.services.Datasets.Create(req.Context(), user.ID, body)
	if h.writeServiceErr(w, err, "知识库不存在", "新建知识库参数错误", "新建知识库失败") {
		return
	}
	resp.OK(w, ds)
}

// HandleUpdateDataset 更新知识库（重命名/改描述）；成功只回 {code:0}。
func (h *Handler) HandleUpdateDataset(w http.ResponseWriter, req *http.Request) {
	user := middleware.CurrentUser(req)
	id, ok := pathID(w, req, "datasetId")
	if !ok {
		return
	}
	var body model.UpdateDatasetRequest
	if !resp.Decode(w, req, &body, "请求格式错误") {
		return
	}
	err := h.services.Datasets.Update(req.Context(), user.ID, id, body)
	if h.writeServiceErr(w, err, "知识库不存在", "更新知识库参数错误", "更新知识库失败") {
		return
	}
	resp.OK(w, nil)
}

// HandleDeleteDataset 删除知识库及其文档、分块；已有会话保留。
func (h *Handler) HandleDeleteDataset(w http.ResponseWriter, req *http.Request) {
	user := middleware.CurrentUser(req)
	id, ok := pathID(w, req, "datasetId")
	if !ok {
		return
	}
	err := h.services.Datasets.Delete(req.Context(), user.ID, id)
	if h.writeServiceErr(w, err, "知识库不存在", "", "删除知识库失败") {
		return
	}
	resp.OK(w, nil)
}
