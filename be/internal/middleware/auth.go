package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"askbase/be/internal/model"
)

type authUserContextKey struct{}

type sessionReader interface {
	CookieName() string
	Me(ctx context.Context, token string) (model.User, error)
}

// AuthRequired 校验登录 Cookie，并把当前用户写入上下文。
func AuthRequired(auth sessionReader, writeUnauthorized func(http.ResponseWriter, *http.Request, string, error)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			user, err := userFromRequest(req, auth)
			if err != nil {
				msg := "未登录"
				if req.Header.Get("Cookie") != "" {
					msg = "登录已过期"
				}
				writeUnauthorized(w, req, msg, err)
				return
			}
			ctx := context.WithValue(req.Context(), authUserContextKey{}, user)
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	}
}

// CurrentUser 读取认证中间件写入的用户；未登录时 ID 为 0。
func CurrentUser(req *http.Request) model.User {
	user, _ := req.Context().Value(authUserContextKey{}).(model.User)
	return user
}

func userFromRequest(req *http.Request, auth sessionReader) (model.User, error) {
	token := sessionTokenFromRequest(req, auth.CookieName())
	if token == "" {
		return model.User{}, fmt.Errorf("请求缺少认证 Cookie")
	}
	return auth.Me(req.Context(), token)
}

func sessionTokenFromRequest(req *http.Request, cookieName string) string {
	if cookie, err := req.Cookie(cookieName); err == nil {
		return strings.TrimSpace(cookie.Value)
	}
	return ""
}
