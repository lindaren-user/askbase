package handler

import (
	"net/http"

	"askbase/be/internal/middleware"
	"askbase/be/internal/model"
	"askbase/be/pkg/resp"
)

// HandleListVisionModels 当前用户的视觉模型列表。
func (h *Handler) HandleListVisionModels(w http.ResponseWriter, req *http.Request) {
	user := middleware.CurrentUser(req)
	items, err := h.services.VisionModels.List(req.Context(), user.ID)
	if h.writeServiceErr(w, err, "", "", "查询视觉模型失败") {
		return
	}
	resp.OK(w, items)
}

// HandleCreateVisionModel 登记视觉模型。
func (h *Handler) HandleCreateVisionModel(w http.ResponseWriter, req *http.Request) {
	user := middleware.CurrentUser(req)
	var body model.CreateVisionModelRequest
	if !resp.Decode(w, req, &body, "请求格式错误") {
		return
	}
	item, err := h.services.VisionModels.Create(req.Context(), user.ID, body)
	if h.writeServiceErr(w, err, "", "参数错误", "创建视觉模型失败") {
		return
	}
	resp.OK(w, item)
}

// HandleUpdateVisionModel 更新视觉模型。
func (h *Handler) HandleUpdateVisionModel(w http.ResponseWriter, req *http.Request) {
	user := middleware.CurrentUser(req)
	id, ok := pathID(w, req, "visionModelId")
	if !ok {
		return
	}
	var body model.UpdateVisionModelRequest
	if !resp.Decode(w, req, &body, "请求格式错误") {
		return
	}
	err := h.services.VisionModels.Update(req.Context(), user.ID, id, body)
	if h.writeServiceErr(w, err, "视觉模型不存在", "参数错误", "更新视觉模型失败") {
		return
	}
	resp.OK(w, nil)
}

// HandleDeleteVisionModel 删除未被引用的视觉模型。
func (h *Handler) HandleDeleteVisionModel(w http.ResponseWriter, req *http.Request) {
	user := middleware.CurrentUser(req)
	id, ok := pathID(w, req, "visionModelId")
	if !ok {
		return
	}
	err := h.services.VisionModels.Delete(req.Context(), user.ID, id)
	if h.writeServiceErr(w, err, "视觉模型不存在", "无法删除", "删除视觉模型失败") {
		return
	}
	resp.OK(w, nil)
}
