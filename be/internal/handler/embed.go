package handler

import (
	"net/http"

	"askbase/be/internal/middleware"
	"askbase/be/internal/model"
	"askbase/be/pkg/resp"
)

// HandleListEmbedModels 当前用户的嵌入模型列表。
func (h *Handler) HandleListEmbedModels(w http.ResponseWriter, req *http.Request) {
	user := middleware.CurrentUser(req)
	items, err := h.services.EmbedModels.List(req.Context(), user.ID)
	if h.writeServiceErr(w, err, "", "", "查询嵌入模型失败") {
		return
	}
	resp.OK(w, items)
}

// HandleCreateEmbedModel 登记嵌入模型。
func (h *Handler) HandleCreateEmbedModel(w http.ResponseWriter, req *http.Request) {
	user := middleware.CurrentUser(req)
	var body model.CreateEmbedModelRequest
	if !resp.Decode(w, req, &body, "请求格式错误") {
		return
	}
	item, err := h.services.EmbedModels.Create(req.Context(), user.ID, body)
	if h.writeServiceErr(w, err, "", "参数错误", "创建嵌入模型失败") {
		return
	}
	resp.OK(w, item)
}

// HandleUpdateEmbedModel 更新嵌入模型。
func (h *Handler) HandleUpdateEmbedModel(w http.ResponseWriter, req *http.Request) {
	user := middleware.CurrentUser(req)
	id, ok := pathID(w, req, "embedModelId")
	if !ok {
		return
	}
	var body model.UpdateEmbedModelRequest
	if !resp.Decode(w, req, &body, "请求格式错误") {
		return
	}
	err := h.services.EmbedModels.Update(req.Context(), user.ID, id, body)
	if h.writeServiceErr(w, err, "嵌入模型不存在", "参数错误", "更新嵌入模型失败") {
		return
	}
	resp.OK(w, nil)
}

// HandleDeleteEmbedModel 删除未被引用的嵌入模型。
func (h *Handler) HandleDeleteEmbedModel(w http.ResponseWriter, req *http.Request) {
	user := middleware.CurrentUser(req)
	id, ok := pathID(w, req, "embedModelId")
	if !ok {
		return
	}
	err := h.services.EmbedModels.Delete(req.Context(), user.ID, id)
	if h.writeServiceErr(w, err, "嵌入模型不存在", "无法删除", "删除嵌入模型失败") {
		return
	}
	resp.OK(w, nil)
}
