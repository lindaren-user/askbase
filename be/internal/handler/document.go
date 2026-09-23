package handler

import (
	"net/http"

	"askbase/be/internal/middleware"
	"askbase/be/internal/model"
	"askbase/be/pkg/resp"
)

// HandleCreateUploadURL 申请 R2 预签名直传地址。
func (h *Handler) HandleCreateUploadURL(w http.ResponseWriter, req *http.Request) {
	user := middleware.CurrentUser(req)
	var body model.UploadURLRequest
	if !resp.Decode(w, req, &body, "请求格式错误") {
		return
	}
	token, err := h.services.Documents.CreateUploadURL(req.Context(), user.ID, body)
	if h.writeServiceErr(w, err, "知识库不存在", "申请上传地址参数错误", "申请上传地址失败") {
		return
	}
	resp.OK(w, token)
}

// HandleRegisterDocument 直传完成后登记文档；hash 命中返回已有文档并带 existed 标记。
func (h *Handler) HandleRegisterDocument(w http.ResponseWriter, req *http.Request) {
	user := middleware.CurrentUser(req)
	datasetID, ok := pathID(w, req, "datasetId")
	if !ok {
		return
	}
	var body model.RegisterDocumentRequest
	if !resp.Decode(w, req, &body, "请求格式错误") {
		return
	}
	doc, created, err := h.services.Documents.Register(req.Context(), user.ID, datasetID, body)
	if h.writeServiceErr(w, err, "知识库不存在", "登记文档参数错误", "登记文档失败") {
		return
	}
	resp.OK(w, map[string]any{"document": doc, "created": created})
}

// HandleListDocuments 文档列表。
func (h *Handler) HandleListDocuments(w http.ResponseWriter, req *http.Request) {
	user := middleware.CurrentUser(req)
	datasetID, ok := pathID(w, req, "datasetId")
	if !ok {
		return
	}
	items, err := h.services.Documents.List(req.Context(), user.ID, datasetID)
	if h.writeServiceErr(w, err, "知识库不存在", "", "查询文档列表失败") {
		return
	}
	resp.OK(w, items)
}

// HandleGetDocument 查询单个文档。
func (h *Handler) HandleGetDocument(w http.ResponseWriter, req *http.Request) {
	user := middleware.CurrentUser(req)
	id, ok := pathID(w, req, "documentId")
	if !ok {
		return
	}
	doc, err := h.services.Documents.Get(req.Context(), user.ID, id)
	if h.writeServiceErr(w, err, "文档不存在", "", "查询文档失败") {
		return
	}
	resp.OK(w, doc)
}

// HandleUpdateDocument 重命名或启停文档。
func (h *Handler) HandleUpdateDocument(w http.ResponseWriter, req *http.Request) {
	user := middleware.CurrentUser(req)
	id, ok := pathID(w, req, "documentId")
	if !ok {
		return
	}
	var body model.UpdateDocumentRequest
	if !resp.Decode(w, req, &body, "请求格式错误") {
		return
	}
	err := h.services.Documents.Update(req.Context(), user.ID, id, body)
	if h.writeServiceErr(w, err, "文档不存在", "更新文档参数错误", "更新文档失败") {
		return
	}
	resp.OK(w, nil)
}

// HandleDeleteDocument 删除文档及其分块与对象。
func (h *Handler) HandleDeleteDocument(w http.ResponseWriter, req *http.Request) {
	user := middleware.CurrentUser(req)
	id, ok := pathID(w, req, "documentId")
	if !ok {
		return
	}
	err := h.services.Documents.Delete(req.Context(), user.ID, id)
	if h.writeServiceErr(w, err, "文档不存在", "", "删除文档失败") {
		return
	}
	resp.OK(w, nil)
}

// HandleParseDocument 触发或重试解析。
func (h *Handler) HandleParseDocument(w http.ResponseWriter, req *http.Request) {
	user := middleware.CurrentUser(req)
	id, ok := pathID(w, req, "documentId")
	if !ok {
		return
	}
	err := h.services.Documents.TriggerParse(req.Context(), user.ID, id)
	if h.writeServiceErr(w, err, "文档不存在", "文档正在解析中", "触发解析失败") {
		return
	}
	resp.OK(w, nil)
}

// HandleDocumentProgress 文档流水线进度轮询。
func (h *Handler) HandleDocumentProgress(w http.ResponseWriter, req *http.Request) {
	user := middleware.CurrentUser(req)
	id, ok := pathID(w, req, "documentId")
	if !ok {
		return
	}
	progress, err := h.services.Documents.Progress(req.Context(), user.ID, id)
	if h.writeServiceErr(w, err, "文档不存在", "", "查询进度失败") {
		return
	}
	resp.OK(w, progress)
}

// HandleStopDocument 停止解析。
func (h *Handler) HandleStopDocument(w http.ResponseWriter, req *http.Request) {
	user := middleware.CurrentUser(req)
	id, ok := pathID(w, req, "documentId")
	if !ok {
		return
	}
	err := h.services.Documents.Stop(req.Context(), user.ID, id)
	if h.writeServiceErr(w, err, "文档不存在", "当前状态不可停止", "停止解析失败") {
		return
	}
	resp.OK(w, nil)
}

// HandleBatchDocumentStatus 批量启停文档。
func (h *Handler) HandleBatchDocumentStatus(w http.ResponseWriter, req *http.Request) {
	user := middleware.CurrentUser(req)
	var body model.BatchDocumentStatusRequest
	if !resp.Decode(w, req, &body, "请求格式错误") {
		return
	}
	err := h.services.Documents.BatchStatus(req.Context(), user.ID, body)
	if h.writeServiceErr(w, err, "文档不存在", "批量启停参数错误", "批量启停失败") {
		return
	}
	resp.OK(w, nil)
}

// HandlePreviewDocument 在线预览。
func (h *Handler) HandlePreviewDocument(w http.ResponseWriter, req *http.Request) {
	user := middleware.CurrentUser(req)
	id, ok := pathID(w, req, "documentId")
	if !ok {
		return
	}
	preview, err := h.services.Documents.Preview(req.Context(), user.ID, id)
	if h.writeServiceErr(w, err, "文档不存在", "无法预览该文档", "预览失败") {
		return
	}
	resp.OK(w, preview)
}

// HandleDownloadDocument 预签名下载地址。
func (h *Handler) HandleDownloadDocument(w http.ResponseWriter, req *http.Request) {
	user := middleware.CurrentUser(req)
	id, ok := pathID(w, req, "documentId")
	if !ok {
		return
	}
	dl, err := h.services.Documents.Download(req.Context(), user.ID, id)
	if h.writeServiceErr(w, err, "文档不存在", "无法下载该文档", "生成下载地址失败") {
		return
	}
	resp.OK(w, dl)
}
