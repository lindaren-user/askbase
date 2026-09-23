package handler

import (
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"askbase/be/internal/middleware"
	"askbase/be/internal/model"
	"askbase/be/internal/service"
	"askbase/be/pkg/resp"
)

// HandleTurnstileConfig 返回前端渲染人机验证所需的站点密钥。
func (h *Handler) HandleTurnstileConfig(w http.ResponseWriter, _ *http.Request) {
	resp.OK(w, h.services.Auth.TurnstileConfig())
}

// HandleSendVerificationCode 发送邮箱验证码。
func (h *Handler) HandleSendVerificationCode(w http.ResponseWriter, req *http.Request) {
	var body model.SendCodeRequest
	if !resp.Decode(w, req, &body, "请求格式错误") {
		return
	}
	body.RemoteIP = requestIP(req)
	err := h.services.Auth.SendVerificationCode(req.Context(), body)
	if h.writeAuthError(w, err, "发送验证码失败") {
		return
	}
	resp.OK(w, nil)
}

// HandleLogin 邮箱验证码登录；邮箱未注册时自动创建账号。
func (h *Handler) HandleLogin(w http.ResponseWriter, req *http.Request) {
	var body model.AuthRequest
	if !resp.Decode(w, req, &body, "请求格式错误") {
		return
	}
	user, err := h.services.Auth.Login(req.Context(), body)
	if h.writeAuthError(w, err, "登录失败") {
		return
	}
	if !h.writeSessionCookie(w, user.ID) {
		return
	}
	resp.OK(w, model.MeResponse{User: user})
}

func (h *Handler) writeAuthError(w http.ResponseWriter, err error, fallback string) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, service.ErrTurnstileInvalid) {
		resp.Fail(w, http.StatusBadRequest, "人机验证失败，请重试", err)
		return true
	}
	if errors.Is(err, service.ErrVerificationCode) {
		resp.Fail(w, http.StatusBadRequest, "邮箱验证码不正确或已过期", err)
		return true
	}
	if errors.Is(err, service.ErrInvalidCredentials) {
		resp.Fail(w, http.StatusBadRequest, "请输入正确的邮箱", err)
		return true
	}
	resp.Fail(w, http.StatusInternalServerError, fallback, err)
	return true
}

// HandleMe 返回当前登录用户。
func (h *Handler) HandleMe(w http.ResponseWriter, req *http.Request) {
	resp.OK(w, model.MeResponse{User: middleware.CurrentUser(req)})
}

// HandleLogout 清除登录 Cookie。
func (h *Handler) HandleLogout(w http.ResponseWriter, _ *http.Request) {
	h.clearSessionCookie(w)
	resp.OK(w, nil)
}

// HandleDeleteAccount 注销当前账号：级联删除全部业务数据后清除登录 Cookie。
func (h *Handler) HandleDeleteAccount(w http.ResponseWriter, req *http.Request) {
	user := middleware.CurrentUser(req)
	if err := h.services.Auth.DeleteAccount(req.Context(), user.ID); err != nil {
		h.writeServiceErr(w, err, "账号不存在", "", "注销账号失败")
		return
	}
	h.clearSessionCookie(w)
	resp.OK(w, nil)
}

func (h *Handler) sessionCookie(value string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name:     h.services.Auth.CookieName(),
		Value:    value,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   h.services.Auth.CookieSecure(),
		SameSite: http.SameSiteLaxMode,
	}
}

func (h *Handler) writeSessionCookie(w http.ResponseWriter, userID int64) bool {
	token, err := h.services.Auth.SignSession(userID)
	if err != nil {
		resp.Fail(w, http.StatusInternalServerError, "写入登录状态失败", err)
		return false
	}
	cookie := h.sessionCookie(token, h.services.Auth.CookieMaxAge())
	cookie.Expires = time.Now().Add(time.Duration(h.services.Auth.CookieMaxAge()) * time.Second)
	http.SetCookie(w, cookie)
	return true
}

func (h *Handler) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, h.sessionCookie("", -1))
}

func requestIP(req *http.Request) string {
	if forwarded := strings.TrimSpace(req.Header.Get("X-Forwarded-For")); forwarded != "" {
		return strings.TrimSpace(strings.Split(forwarded, ",")[0])
	}
	host, _, err := net.SplitHostPort(req.RemoteAddr)
	if err != nil {
		return req.RemoteAddr
	}
	return host
}
