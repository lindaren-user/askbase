// Package resp 统一 API 响应：成功 {code:0[,data]}，失败 {code:1,msg}。
package resp

import (
	"encoding/json"
	"net/http"

	"go.uber.org/zap"
)

const (
	CodeOK      = 0 // 请求成功
	CodeFailure = 1 // 业务或请求失败
)

// Response 统一 JSON 响应。成功仅含 code，需要时再带 data；失败含 msg。
type Response struct {
	Code int    `json:"code"`
	Msg  string `json:"msg,omitempty"`
	Data any    `json:"data,omitempty"`
}

// OK 写出成功响应；data 为 nil 时只回 {code:0}（更新接口不回查）。
func OK(w http.ResponseWriter, data any) {
	payload := Response{Code: CodeOK}
	if data != nil {
		payload.Data = data
	}
	writeJSON(w, http.StatusOK, payload)
}

// Fail 写出失败响应；5xx 时记录内部错误，对外只暴露安全 msg。
func Fail(w http.ResponseWriter, status int, msg string, err error) {
	if err != nil && status >= http.StatusInternalServerError {
		zap.L().Error("请求处理失败", zap.String("msg", msg), zap.Error(err))
	}
	writeJSON(w, status, Response{Code: CodeFailure, Msg: msg})
}

// Decode 解析请求 JSON；失败时写出 400 并返回 false。
func Decode(w http.ResponseWriter, req *http.Request, target any, msg string) bool {
	if err := json.NewDecoder(req.Body).Decode(target); err != nil {
		Fail(w, http.StatusBadRequest, msg, err)
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		zap.L().Error("写入响应失败", zap.Error(err))
	}
}
