package handler

import (
	"net/http"

	"askbase/be/internal/model"
	"askbase/be/pkg/resp"
)

// HandleTestModel 测试自定义对话模型连通性（BYOK，密钥不落地）。
func (h *Handler) HandleTestModel(w http.ResponseWriter, req *http.Request) {
	var body model.TestModelRequest
	if !resp.Decode(w, req, &body, "请求格式错误") {
		return
	}
	result, err := h.services.Models.Test(req.Context(), body)
	writeModelTestResult(w, result, err)
}

// HandleTestEmbed 测试自定义嵌入模型连通性，回报向量维度。
func (h *Handler) HandleTestEmbed(w http.ResponseWriter, req *http.Request) {
	var body model.TestEmbedRequest
	if !resp.Decode(w, req, &body, "请求格式错误") {
		return
	}
	result, err := h.services.Models.TestEmbed(req.Context(), body)
	writeModelTestResult(w, result, err)
}

// HandleTestVision 测试自定义视觉模型连通性。
func (h *Handler) HandleTestVision(w http.ResponseWriter, req *http.Request) {
	var body model.TestModelRequest
	if !resp.Decode(w, req, &body, "请求格式错误") {
		return
	}
	result, err := h.services.Models.TestVision(req.Context(), body)
	writeModelTestResult(w, result, err)
}

// writeModelTestResult 连通性测试统一出参：失败也以 200 + ok=false 返回，便于前端展示原因。
func writeModelTestResult(w http.ResponseWriter, result model.TestModelResponse, err error) {
	if err != nil {
		resp.OK(w, model.TestModelResponse{OK: false, Message: err.Error()})
		return
	}
	resp.OK(w, result)
}
