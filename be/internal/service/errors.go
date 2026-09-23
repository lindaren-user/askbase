package service

import "errors"

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrVerificationCode   = errors.New("verification code invalid")
	ErrTurnstileInvalid   = errors.New("turnstile invalid")
	ErrUnauthorized       = errors.New("unauthorized")
	ErrInvalidInput       = errors.New("invalid input")
	ErrNotFound           = errors.New("not found")
	ErrConflict           = errors.New("conflict")
	ErrAIUnavailable      = errors.New("ai unavailable")
)

// asNotFound 把 repo 层未命中错误映射为服务层 ErrNotFound。
func asNotFound(err error, match error) error {
	if errors.Is(err, match) {
		return ErrNotFound
	}
	return err
}
