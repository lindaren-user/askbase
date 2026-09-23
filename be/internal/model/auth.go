package model

// SendCodeRequest 发送邮箱验证码。
type SendCodeRequest struct {
	Email          string `json:"email"`
	TurnstileToken string `json:"turnstileToken"` // 人机验证令牌；未启用时前端可不传
	RemoteIP       string `json:"-"`              // 由 handler 填入，转给人机校验
}

// AuthRequest 邮箱验证码登录；未注册邮箱将自动创建账号。
type AuthRequest struct {
	Email string `json:"email"`
	Code  string `json:"code"`
}

// MeResponse 当前登录用户。
type MeResponse struct {
	User User `json:"user"`
}
