package handler

import (
	"net/http"

	"askbase/be/pkg/resp"
)

// HandleHealth 健康检查。
func (h *Handler) HandleHealth(w http.ResponseWriter, req *http.Request) {
	if err := h.services.Health.Check(req.Context()); err != nil {
		resp.Fail(w, http.StatusServiceUnavailable, "服务不可用", err)
		return
	}
	resp.OK(w, map[string]string{"status": "ok"})
}
