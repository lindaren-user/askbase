package handler

import (
	"errors"
	"net/http"

	"askbase/be/internal/service"
	"askbase/be/pkg/resp"
)

// Handler 承载 HTTP 处理函数。
type Handler struct {
	services service.Services
}

// New 创建 handler 集合。
func New(services service.Services) *Handler {
	return &Handler{services: services}
}

// WriteUnauthorized 返回未登录或登录过期。
func (h *Handler) WriteUnauthorized(w http.ResponseWriter, req *http.Request, msg string, err error) {
	resp.Fail(w, http.StatusUnauthorized, msg, err)
}

// writeServiceErr 将 service 层错误映射为 HTTP 响应；无错误返回 false。
func (h *Handler) writeServiceErr(w http.ResponseWriter, err error, notFoundMsg, badRequestMsg, failMsg string) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, service.ErrNotFound) {
		resp.Fail(w, http.StatusNotFound, notFoundMsg, err)
		return true
	}
	if badRequestMsg != "" && errors.Is(err, service.ErrInvalidInput) {
		resp.Fail(w, http.StatusBadRequest, badRequestMsg, err)
		return true
	}
	if errors.Is(err, service.ErrConflict) {
		msg := failMsg
		if badRequestMsg != "" {
			msg = badRequestMsg
		}
		resp.Fail(w, http.StatusConflict, msg, err)
		return true
	}
	resp.Fail(w, http.StatusInternalServerError, failMsg, err)
	return true
}
