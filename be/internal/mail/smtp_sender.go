package mail

import (
	"crypto/tls"
	"fmt"
	"mime"
	"net/smtp"
)

type smtpSender struct {
	host     string
	port     int
	username string
	password string
}

// NewSMTPSender 创建通过 SMTPS 发送邮件的实现。
func NewSMTPSender(host string, port int, username string, password string) Sender {
	return &smtpSender{
		host:     host,
		port:     port,
		username: username,
		password: password,
	}
}

func (s *smtpSender) Send(to string, subject string, textBody string, htmlBody string) error {
	addr := fmt.Sprintf("%s:%d", s.host, s.port)
	boundary := "askbase-mail-boundary"

	msg := fmt.Sprintf(
		"From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: multipart/alternative; boundary=%q\r\n\r\n"+
			"--%s\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n%s\r\n\r\n"+
			"--%s\r\nContent-Type: text/html; charset=UTF-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n%s\r\n\r\n"+
			"--%s--\r\n",
		s.username,
		to,
		mime.QEncoding.Encode("UTF-8", subject),
		boundary,
		boundary,
		textBody,
		boundary,
		htmlBody,
		boundary,
	)

	tlsConfig := &tls.Config{
		ServerName: s.host,
		MinVersion: tls.VersionTLS12,
	}

	conn, err := tls.Dial("tcp", addr, tlsConfig)
	if err != nil {
		return fmt.Errorf("SMTP TLS 连接失败: %w", err)
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, s.host)
	if err != nil {
		return fmt.Errorf("SMTP 客户端创建失败: %w", err)
	}
	defer client.Quit()

	auth := smtp.PlainAuth("", s.username, s.password, s.host)
	if err := client.Auth(auth); err != nil {
		return fmt.Errorf("SMTP 认证失败: %w", err)
	}

	if err := client.Mail(s.username); err != nil {
		return fmt.Errorf("SMTP 设置发件人失败: %w", err)
	}

	if err := client.Rcpt(to); err != nil {
		return fmt.Errorf("SMTP 设置收件人失败: %w", err)
	}

	wc, err := client.Data()
	if err != nil {
		return fmt.Errorf("SMTP 准备邮件数据失败: %w", err)
	}
	defer wc.Close()

	if _, err := wc.Write([]byte(msg)); err != nil {
		return fmt.Errorf("SMTP 写入邮件内容失败: %w", err)
	}

	return nil
}
