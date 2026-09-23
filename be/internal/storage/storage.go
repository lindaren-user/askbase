package storage

import (
	"fmt"
	"strings"
	"time"

	"askbase/be/internal/config"
)

// UploadToken 文件直传凭证，返回预签名上传地址和最终访问链接。
type UploadToken struct {
	Key       string            `json:"key"`
	URL       string            `json:"url"`
	UploadURL string            `json:"uploadUrl"`
	Method    string            `json:"method"`
	Headers   map[string]string `json:"headers"`
	ExpiresAt time.Time         `json:"expiresAt"`
}

// NewClient 按配置创建对象存储客户端。
func NewClient(cfg config.StorageConfig) (*S3Client, error) {
	if strings.TrimSpace(cfg.S3.Endpoint) == "" {
		return nil, fmt.Errorf("对象存储地址不能为空")
	}
	if strings.TrimSpace(cfg.S3.Bucket) == "" {
		return nil, fmt.Errorf("对象存储桶不能为空")
	}
	if strings.TrimSpace(cfg.S3.AccessKey) == "" {
		return nil, fmt.Errorf("对象存储访问密钥不能为空")
	}
	if strings.TrimSpace(cfg.S3.SecretKey) == "" {
		return nil, fmt.Errorf("对象存储私有密钥不能为空")
	}
	return NewS3Client(cfg.S3), nil
}
