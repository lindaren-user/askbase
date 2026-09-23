package handler

import (
	"net/http"
	"strconv"

	"askbase/be/internal/middleware"
	"askbase/be/internal/model"
	"askbase/be/pkg/resp"
)

// HandleListSessions 会话列表；默认进行中。?status=archived 返回归档会话（含知识库名）。
func (h *Handler) HandleListSessions(w http.ResponseWriter, req *http.Request) {
	user := middleware.CurrentUser(req)
	var (
		items []model.Session
		err   error
		fail  = "查询会话列表失败"
	)
	if req.URL.Query().Get("status") == "archived" {
		items, err = h.services.Sessions.ListArchived(req.Context(), user.ID)
		fail = "查询归档会话失败"
	} else {
		var datasetID *int64
		if raw := req.URL.Query().Get("datasetId"); raw != "" {
			if id, errParse := strconv.ParseInt(raw, 10, 64); errParse == nil && id > 0 {
				datasetID = &id
			}
		}
		items, err = h.services.Sessions.List(req.Context(), user.ID, datasetID)
	}
	if err != nil {
		resp.Fail(w, http.StatusInternalServerError, fail, err)
		return
	}
	resp.OK(w, items)
}

// HandleCreateSession 新建会话。
func (h *Handler) HandleCreateSession(w http.ResponseWriter, req *http.Request) {
	user := middleware.CurrentUser(req)
	var body model.CreateSessionRequest
	if !resp.Decode(w, req, &body, "请求格式错误") {
		return
	}
	session, err := h.services.Sessions.Create(req.Context(), user.ID, body)
	if h.writeServiceErr(w, err, "知识库不存在", "新建会话参数错误", "新建会话失败") {
		return
	}
	resp.OK(w, session)
}

// HandleUpdateSession 更新会话标题或归档状态。
func (h *Handler) HandleUpdateSession(w http.ResponseWriter, req *http.Request) {
	user := middleware.CurrentUser(req)
	id, ok := pathID(w, req, "sessionId")
	if !ok {
		return
	}
	var body model.UpdateSessionRequest
	if !resp.Decode(w, req, &body, "请求格式错误") {
		return
	}
	err := h.services.Sessions.Update(req.Context(), user.ID, id, body)
	if h.writeServiceErr(w, err, "会话不存在", "更新会话参数错误", "更新会话失败") {
		return
	}
	resp.OK(w, nil)
}

// HandleDeleteSession 删除会话及其消息。
func (h *Handler) HandleDeleteSession(w http.ResponseWriter, req *http.Request) {
	user := middleware.CurrentUser(req)
	id, ok := pathID(w, req, "sessionId")
	if !ok {
		return
	}
	err := h.services.Sessions.Delete(req.Context(), user.ID, id)
	if h.writeServiceErr(w, err, "会话不存在", "", "删除会话失败") {
		return
	}
	resp.OK(w, nil)
}

// HandleListMessages 会话消息列表（含引用详情）。
func (h *Handler) HandleListMessages(w http.ResponseWriter, req *http.Request) {
	user := middleware.CurrentUser(req)
	id, ok := pathID(w, req, "sessionId")
	if !ok {
		return
	}
	items, err := h.services.Sessions.ListMessages(req.Context(), user.ID, id)
	if h.writeServiceErr(w, err, "会话不存在", "", "查询消息失败") {
		return
	}
	resp.OK(w, items)
}
