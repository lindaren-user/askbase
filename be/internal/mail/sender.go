package mail

import (
	"fmt"
	"strings"

	"askbase/be/internal/config"

	"go.uber.org/zap"
)

// Sender 发送邮件的可替换实现。
type Sender interface {
	Send(to string, subject string, textBody string, htmlBody string) error
}

// NewSender 按配置创建 SMTP 或 Resend 发信实现。
func NewSender(cfg config.MailConfig) (Sender, error) {
	switch strings.TrimSpace(cfg.Provider) {
	case "echo", "":
		// 开发兜底：不真正发信，验证码打到日志，便于本地演示
		return echoSender{}, nil
	case "smtp":
		if strings.TrimSpace(cfg.SMTP.Host) == "" {
			return nil, fmt.Errorf("mail.smtp.host 不能为空")
		}
		if cfg.SMTP.Port <= 0 {
			return nil, fmt.Errorf("mail.smtp.port 必须大于 0")
		}
		if strings.TrimSpace(cfg.SMTP.Username) == "" {
			return nil, fmt.Errorf("mail.smtp.username 不能为空")
		}
		if strings.TrimSpace(cfg.SMTP.Password) == "" {
			return nil, fmt.Errorf("mail.smtp.password 不能为空")
		}
		return NewSMTPSender(
			cfg.SMTP.Host,
			cfg.SMTP.Port,
			cfg.SMTP.Username,
			cfg.SMTP.Password,
		), nil
	case "resend":
		if strings.TrimSpace(cfg.Resend.ApiKey) == "" {
			return nil, fmt.Errorf("mail.resend.api_key 不能为空")
		}
		if strings.TrimSpace(cfg.Resend.From) == "" {
			return nil, fmt.Errorf("mail.resend.from 不能为空")
		}
		return NewResendSender(cfg.Resend.ApiKey, cfg.Resend.From), nil
	default:
		return nil, fmt.Errorf("mail.provider 必须是 echo、smtp 或 resend")
	}
}

// echoSender 开发用发信实现：邮件内容只写日志。
type echoSender struct{}

func (echoSender) Send(to string, subject string, textBody string, _ string) error {
	zap.L().Info("echo 邮件", zap.String("to", to), zap.String("subject", subject), zap.String("body", textBody))
	return nil
}
