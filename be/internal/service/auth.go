package service

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/dgraph-io/ristretto/v2"
	"gorm.io/gorm"

	"askbase/be/internal/config"
	"askbase/be/internal/mail"
	"askbase/be/internal/model"
	"askbase/be/internal/repo"
)

const (
	sessionTTL             = 30 * 24 * time.Hour
	verificationCodeTTL    = time.Minute
	verificationCodeMin    = 100000
	verificationCodeSpan   = 900000
	verificationCodeCost   = 1
	turnstileSiteVerifyURL = "https://challenges.cloudflare.com/turnstile/v0/siteverify" // Cloudflare 人机校验接口
)

type accountDatasetStore interface {
	List(ctx context.Context, userID int64) ([]model.Dataset, error)
	Delete(ctx context.Context, userID int64, id int64) error
	DeleteUserConversations(ctx context.Context, userID int64) error
}

type accountModelDeleter interface {
	DeleteAllByUser(ctx context.Context, userID int64) error
}

// AuthService 邮箱验证码登录。
type AuthService struct {
	db           *gorm.DB
	users        userStore
	datasets     accountDatasetStore
	embedModels  accountModelDeleter
	visionModels accountModelDeleter
	turnstile    config.TurnstileConfig
	mailSender   mail.Sender
	secret       []byte
	cookieName   string
	cookieSecure bool
	devCode      string
	env          string
	codes        *ristretto.Cache[string, string]
	turnstileURL string
	httpClient   *http.Client
}

// NewAuthService 创建认证服务。验证码写入 Ristretto，1 分钟过期。
// datasets 用于注销账号时级联清理知识库及对象存储。
func NewAuthService(db *gorm.DB, users userStore, datasets accountDatasetStore, embedModels accountModelDeleter, visionModels accountModelDeleter, authConfig config.AuthConfig, env string, mailSender mail.Sender) (*AuthService, error) {
	codes, err := ristretto.NewCache(&ristretto.Config[string, string]{
		NumCounters: 1e4,
		MaxCost:     1e4,
		BufferItems: 64,
	})
	if err != nil {
		return nil, fmt.Errorf("创建验证码缓存失败: %w", err)
	}
	return &AuthService{
		db:           db,
		users:        users,
		datasets:     datasets,
		embedModels:  embedModels,
		visionModels: visionModels,
		turnstile:    authConfig.Turnstile,
		mailSender:   mailSender,
		secret:       []byte(authConfig.Secret),
		cookieName:   authConfig.CookieName,
		cookieSecure: env == config.EnvProd,
		devCode:      devCode(authConfig, env),
		env:          env,
		codes:        codes,
		turnstileURL: turnstileSiteVerifyURL,
		httpClient:   &http.Client{Timeout: 5 * time.Second},
	}, nil
}

// CookieName 返回登录 Cookie 名称。
func (s *AuthService) CookieName() string {
	return s.cookieName
}

// CookieSecure 生产环境为 Cookie 启用 Secure。
func (s *AuthService) CookieSecure() bool {
	return s.cookieSecure
}

// CookieMaxAge 返回登录 Cookie 有效期秒数。
func (s *AuthService) CookieMaxAge() int {
	return int(sessionTTL.Seconds())
}

// TurnstileConfig 返回前端人机验证站点密钥；非生产或未配置时关闭。
func (s *AuthService) TurnstileConfig() model.TurnstileConfigResponse {
	if !s.turnstileActive() {
		return model.TurnstileConfigResponse{}
	}
	return model.TurnstileConfigResponse{
		SiteKey: strings.TrimSpace(s.turnstile.SiteKey),
		Enabled: true,
	}
}

// SendVerificationCode 校验人机验证后发送邮箱验证码，写入进程内缓存一分钟。
func (s *AuthService) SendVerificationCode(ctx context.Context, req model.SendCodeRequest) error {
	email, err := requireEmail(req.Email)
	if err != nil {
		return err
	}
	if err := s.verifyTurnstile(ctx, req.TurnstileToken, req.RemoteIP); err != nil {
		return err
	}
	code, err := generateVerificationCode()
	if err != nil {
		return fmt.Errorf("生成验证码失败: %w", err)
	}
	textBody, htmlBody := verificationCodeEmail(code)
	if err := s.mailSender.Send(email, "AskBase 验证码", textBody, htmlBody); err != nil {
		return fmt.Errorf("发送验证码邮件失败: %w", err)
	}
	if err := s.putCode(email, code); err != nil {
		return err
	}
	return nil
}

// Login 校验验证码后登录；邮箱尚未注册时自动创建账号。
func (s *AuthService) Login(ctx context.Context, req model.AuthRequest) (model.User, error) {
	email, err := requireEmail(req.Email)
	if err != nil {
		return model.User{}, err
	}
	if !s.verifyCode(email, req.Code) {
		return model.User{}, ErrVerificationCode
	}
	user, err := s.lookupOrCreateUser(ctx, email)
	if err != nil {
		return model.User{}, fmt.Errorf("登录失败: %w", err)
	}
	s.deleteCode(email)
	return user, nil
}

// lookupOrCreateUser 已有账号则返回，否则创建；并发冲突时再按邮箱读取。
func (s *AuthService) lookupOrCreateUser(ctx context.Context, email string) (model.User, error) {
	user, err := s.users.FindByEmail(ctx, email)
	if err == nil {
		return user, nil
	}
	if !errors.Is(err, repo.ErrUserNotFound) {
		return model.User{}, err
	}
	user, err = s.users.Create(ctx, email, nicknameFromEmail(email))
	if errors.Is(err, repo.ErrEmailExists) {
		return s.users.FindByEmail(ctx, email)
	}
	return user, err
}

// DeleteAccount 注销账号：级联删除全部知识库（含 R2 对象、分块）及会话、消息后删除用户。
func (s *AuthService) DeleteAccount(ctx context.Context, userID int64) error {
	if _, err := s.users.FindByID(ctx, userID); err != nil {
		return asNotFound(err, repo.ErrUserNotFound)
	}
	list, err := s.datasets.List(ctx, userID)
	if err != nil {
		return fmt.Errorf("查询用户知识库失败: %w", err)
	}
	for _, ds := range list {
		if err := s.datasets.Delete(ctx, userID, ds.ID); err != nil {
			return fmt.Errorf("删除知识库 %d 失败: %w", ds.ID, err)
		}
	}
	if err := s.datasets.DeleteUserConversations(ctx, userID); err != nil {
		return err
	}
	if s.embedModels != nil {
		if err := s.embedModels.DeleteAllByUser(ctx, userID); err != nil {
			return fmt.Errorf("删除用户嵌入模型失败: %w", err)
		}
	}
	if s.visionModels != nil {
		if err := s.visionModels.DeleteAllByUser(ctx, userID); err != nil {
			return fmt.Errorf("删除用户视觉模型失败: %w", err)
		}
	}
	return asNotFound(s.users.Delete(ctx, userID), repo.ErrUserNotFound)
}

// Me 解析登录令牌并返回当前用户；令牌无效或用户不存在时视为未登录。
func (s *AuthService) Me(ctx context.Context, token string) (model.User, error) {
	userID, err := s.parseSession(token)
	if err != nil {
		return model.User{}, ErrUnauthorized
	}
	user, err := s.users.FindByID(ctx, userID)
	if errors.Is(err, repo.ErrUserNotFound) {
		return model.User{}, ErrUnauthorized
	}
	if err != nil {
		return model.User{}, fmt.Errorf("查询当前用户失败: %w", err)
	}
	return user, nil
}

// SignSession 签发登录令牌，有效期与 Cookie 一致。
func (s *AuthService) SignSession(userID int64) (string, error) {
	exp := time.Now().Add(sessionTTL).Unix()
	payload := strconv.FormatInt(userID, 10) + "." + strconv.FormatInt(exp, 10)
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(payload))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return payload + "." + sig, nil
}

// parseSession 校验令牌签名与过期时间，返回用户编号。
func (s *AuthService) parseSession(token string) (int64, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return 0, ErrUnauthorized
	}
	payload := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(payload))
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if subtle.ConstantTimeCompare([]byte(parts[2]), []byte(expected)) != 1 {
		return 0, ErrUnauthorized
	}
	exp, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return 0, ErrUnauthorized
	}
	userID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || userID <= 0 {
		return 0, ErrUnauthorized
	}
	return userID, nil
}

// putCode 把验证码写入缓存；须 Wait 确保对后续读取可见。
func (s *AuthService) putCode(email string, code string) error {
	if ok := s.codes.SetWithTTL(email, code, verificationCodeCost, verificationCodeTTL); !ok {
		return fmt.Errorf("写入验证码缓存失败")
	}
	s.codes.Wait()
	return nil
}

// verifyCode 恒定时间比较验证码；dev 环境配置 devCode 时该码直通（本地演示用）。
func (s *AuthService) verifyCode(email string, code string) bool {
	if s.devCode != "" && subtle.ConstantTimeCompare([]byte(digitsOnly(code)), []byte(s.devCode)) == 1 {
		return true
	}
	stored, found := s.codes.Get(email)
	if !found {
		return false
	}
	got := digitsOnly(code)
	if len(got) != len(stored) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(stored)) == 1
}

func (s *AuthService) deleteCode(email string) {
	s.codes.Del(email)
}

// devCode 仅开发环境返回配置的万能验证码；生产环境强制为空。
func devCode(authConfig config.AuthConfig, env string) string {
	if env == config.EnvProd {
		return ""
	}
	return digitsOnly(authConfig.DevCode)
}

// turnstileActive 仅生产且配置了站点密钥时启用人机验证。
func (s *AuthService) turnstileActive() bool {
	if s.env != config.EnvProd {
		return false
	}
	return strings.TrimSpace(s.turnstile.SiteKey) != "" && strings.TrimSpace(s.turnstile.SecretKey) != ""
}

// verifyTurnstile 向 Cloudflare 校验人机令牌；未启用时直接通过。
func (s *AuthService) verifyTurnstile(ctx context.Context, token string, remoteIP string) error {
	if !s.turnstileActive() {
		return nil
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return ErrTurnstileInvalid
	}
	form := url.Values{}
	form.Set("secret", strings.TrimSpace(s.turnstile.SecretKey))
	form.Set("response", token)
	if ip := publicClientIP(remoteIP); ip != "" {
		form.Set("remoteip", ip)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.turnstileURL, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrTurnstileInvalid, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrTurnstileInvalid, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: siteverify status %d", ErrTurnstileInvalid, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrTurnstileInvalid, err)
	}
	var result struct {
		Success    bool     `json:"success"`
		ErrorCodes []string `json:"error-codes"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("%w: %v", ErrTurnstileInvalid, err)
	}
	if result.Success {
		return nil
	}
	if len(result.ErrorCodes) == 0 {
		return ErrTurnstileInvalid
	}
	return fmt.Errorf("%w: %s", ErrTurnstileInvalid, strings.Join(result.ErrorCodes, ","))
}

// publicClientIP 过滤回环与内网地址，避免把它们传给人机校验。
func publicClientIP(value string) string {
	ip := net.ParseIP(strings.TrimSpace(value))
	if ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() {
		return ""
	}
	return ip.String()
}

// generateVerificationCode 生成六位数字验证码。
func generateVerificationCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(verificationCodeSpan))
	if err != nil {
		return "", err
	}
	return strconv.FormatInt(n.Int64()+verificationCodeMin, 10), nil
}

func digitsOnly(value string) string {
	var b strings.Builder
	for _, r := range value {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// requireEmail 规范化邮箱；缺少 @ 视为不合法。
func requireEmail(email string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(email))
	if normalized == "" || !strings.Contains(normalized, "@") {
		return "", ErrInvalidCredentials
	}
	return normalized, nil
}

func nicknameFromEmail(email string) string {
	at := strings.Index(email, "@")
	if at <= 0 {
		return "用户"
	}
	name := email[:at]
	if len(name) > 64 {
		return name[:64]
	}
	return name
}

func verificationCodeEmail(code string) (string, string) {
	textBody := fmt.Sprintf("您的 AskBase 验证码是：%s。验证码有效期 1 分钟，请勿泄露给他人。", code)
	htmlBody := fmt.Sprintf(`<!doctype html>
<html lang="zh-CN">
  <head><meta charset="UTF-8"><title>AskBase 验证码</title></head>
  <body style="font-family:sans-serif;color:#111;">
    <p>你正在验证 AskBase 账号邮箱，请输入验证码：</p>
    <p style="font-size:28px;letter-spacing:0.2em;font-weight:700;">%s</p>
    <p>验证码有效期 1 分钟。如果这不是你的操作，可以忽略这封邮件。</p>
  </body>
</html>`, code)
	return textBody, htmlBody
}
