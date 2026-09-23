package storage

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"askbase/be/internal/config"
)

const unsignedPayload = "UNSIGNED-PAYLOAD"

var s3HTTPClient = &http.Client{Timeout: 60 * time.Second}

// S3Client 使用兼容协议生成直传凭证，支持 Cloudflare R2。
type S3Client struct {
	endpoint      string
	region        string
	bucket        string
	accessKey     string
	secretKey     string
	publicBaseURL string
}

// NewS3Client 创建兼容对象存储客户端。
func NewS3Client(cfg config.S3StorageConfig) *S3Client {
	return &S3Client{
		endpoint:      strings.TrimRight(cfg.Endpoint, "/"),
		region:        cfg.Region,
		bucket:        cfg.Bucket,
		accessKey:     cfg.AccessKey,
		secretKey:     cfg.SecretKey,
		publicBaseURL: strings.TrimRight(cfg.PublicBaseURL, "/"),
	}
}

// requireConfig 检查 endpoint、桶和密钥是否齐全。
func (c *S3Client) requireConfig() error {
	if c.endpoint == "" || c.bucket == "" || c.accessKey == "" || c.secretKey == "" {
		return fmt.Errorf("对象存储配置不完整")
	}
	return nil
}

// objectURL 拼出桶内对象的请求地址。
func (c *S3Client) objectURL(key string) (*url.URL, error) {
	objectKey := strings.TrimLeft(path.Clean(key), "/")
	endpointURL, err := url.Parse(c.endpoint)
	if err != nil {
		return nil, fmt.Errorf("解析对象存储地址失败: %w", err)
	}
	reqURL := *endpointURL
	reqURL.Path = path.Join("/", c.bucket, objectKey)
	reqURL.RawQuery = ""
	return &reqURL, nil
}

// Get 下载对象全文。
func (c *S3Client) Get(ctx context.Context, key string) ([]byte, error) {
	resp, err := c.signedDo(ctx, http.MethodGet, key, nil, "")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取对象失败: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("下载对象失败: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return body, nil
}

// Put 服务端上传对象（解析流水线写入图片原子）。
func (c *S3Client) Put(ctx context.Context, key string, data []byte, contentType string) error {
	resp, err := c.signedDo(ctx, http.MethodPut, key, data, contentType)
	if err != nil {
		return err
	}
	return closeAndCheck(resp, "上传对象", false)
}

// Delete 删除对象；对象不存在视为成功。
func (c *S3Client) Delete(ctx context.Context, key string) error {
	resp, err := c.signedDo(ctx, http.MethodDelete, key, nil, "")
	if err != nil {
		return err
	}
	return closeAndCheck(resp, "删除对象", true)
}

// closeAndCheck 读取响应并按状态码判断对象操作是否成功。
func closeAndCheck(resp *http.Response, op string, notFoundOK bool) error {
	defer resp.Body.Close()
	if notFoundOK && resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	return fmt.Errorf("%s失败: status=%d body=%s", op, resp.StatusCode, strings.TrimSpace(string(body)))
}

// signedDo 对对象存储发出带 SigV4 的请求。
func (c *S3Client) signedDo(ctx context.Context, method, key string, body []byte, contentType string) (*http.Response, error) {
	if err := c.requireConfig(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	reqURL, err := c.objectURL(key)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	payloadHash := unsignedPayload
	if body != nil {
		payloadHash = sha256Hex(body)
	}
	headerMap := map[string]string{
		"host":                 reqURL.Host,
		"x-amz-content-sha256": payloadHash,
		"x-amz-date":           amzDate,
	}
	signedNames := []string{"host", "x-amz-content-sha256", "x-amz-date"}
	if contentType != "" {
		headerMap["content-type"] = contentType
		signedNames = append(signedNames, "content-type")
		sort.Strings(signedNames)
	}
	var canonicalHeaders strings.Builder
	for _, name := range signedNames {
		canonicalHeaders.WriteString(name)
		canonicalHeaders.WriteByte(':')
		canonicalHeaders.WriteString(strings.TrimSpace(headerMap[name]))
		canonicalHeaders.WriteByte('\n')
	}
	signedHeaders := strings.Join(signedNames, ";")
	canonicalRequest := strings.Join([]string{
		method,
		reqURL.EscapedPath(),
		"",
		canonicalHeaders.String(),
		signedHeaders,
		payloadHash,
	}, "\n")
	scope := fmt.Sprintf("%s/%s/s3/aws4_request", now.Format("20060102"), c.region)
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		scope,
		sha256Hex([]byte(canonicalRequest)),
	}, "\n")
	signature := hex.EncodeToString(hmacSHA256(signingKey(c.secretKey, now.Format("20060102"), c.region), []byte(stringToSign)))
	auth := fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		c.accessKey, scope, signedHeaders, signature,
	)

	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, reqURL.String(), reader)
	if err != nil {
		return nil, fmt.Errorf("构造对象存储请求失败: %w", err)
	}
	req.Host = reqURL.Host
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)
	req.Header.Set("X-Amz-Date", amzDate)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := s3HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求对象存储失败: %w", err)
	}
	return resp, nil
}

// PresignPut 生成可由前端直传对象存储的预签名地址。
func (c *S3Client) PresignPut(ctx context.Context, key string, contentType string, expires time.Duration) (UploadToken, error) {
	objectKey := strings.TrimLeft(path.Clean(key), "/")
	uploadURL, err := c.presignURL(ctx, http.MethodPut, objectKey, nil, expires)
	if err != nil {
		return UploadToken{}, err
	}
	expiresSeconds := int64(expires.Seconds())
	if expiresSeconds <= 0 || expiresSeconds > 604800 {
		expiresSeconds = 300
	}
	return UploadToken{
		Key:       objectKey,
		URL:       c.publicURL(objectKey),
		UploadURL: uploadURL,
		Method:    http.MethodPut,
		Headers:   map[string]string{},
		ExpiresAt: time.Now().UTC().Add(time.Duration(expiresSeconds) * time.Second),
	}, nil
}

// PresignGet 生成预签名下载地址；filename 非空时带 attachment；contentType 非空时覆盖响应类型。
func (c *S3Client) PresignGet(ctx context.Context, key string, filename string, contentType string, expires time.Duration) (string, error) {
	objectKey := strings.TrimLeft(path.Clean(key), "/")
	extra := url.Values{}
	if name := strings.TrimSpace(filename); name != "" {
		extra.Set("response-content-disposition", `attachment; filename="`+strings.ReplaceAll(name, `"`, "")+`"`)
	}
	if ct := strings.TrimSpace(contentType); ct != "" {
		extra.Set("response-content-type", ct)
	}
	return c.presignURL(ctx, http.MethodGet, objectKey, extra, expires)
}

func (c *S3Client) presignURL(ctx context.Context, method, objectKey string, extra url.Values, expires time.Duration) (string, error) {
	if err := c.requireConfig(); err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	endpointURL, err := url.Parse(c.endpoint)
	if err != nil {
		return "", fmt.Errorf("解析对象存储地址失败: %w", err)
	}
	now := time.Now().UTC()
	expiresSeconds := int64(expires.Seconds())
	if expiresSeconds <= 0 || expiresSeconds > 604800 {
		expiresSeconds = 300
	}
	reqURL := *endpointURL
	reqURL.Path = path.Join(endpointURL.Path, "/", c.bucket, objectKey)
	query := reqURL.Query()
	for key, vals := range extra {
		for _, v := range vals {
			query.Set(key, v)
		}
	}
	scope := fmt.Sprintf("%s/%s/s3/aws4_request", now.Format("20060102"), c.region)
	query.Set("X-Amz-Algorithm", "AWS4-HMAC-SHA256")
	query.Set("X-Amz-Credential", c.accessKey+"/"+scope)
	query.Set("X-Amz-Date", now.Format("20060102T150405Z"))
	query.Set("X-Amz-Expires", strconv.FormatInt(expiresSeconds, 10))
	query.Set("X-Amz-SignedHeaders", "host")
	reqURL.RawQuery = canonicalQuery(query)
	signature := c.presignSignature(method, reqURL, scope, now)
	query.Set("X-Amz-Signature", signature)
	reqURL.RawQuery = canonicalQuery(query)
	return reqURL.String(), nil
}

// presignSignature 计算预签名 PUT 的 SigV4 签名。
func (c *S3Client) presignSignature(method string, uploadURL url.URL, scope string, now time.Time) string {
	canonicalRequest := strings.Join([]string{
		method,
		uploadURL.EscapedPath(),
		uploadURL.RawQuery,
		"host:" + uploadURL.Host + "\n",
		"host",
		unsignedPayload,
	}, "\n")
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		now.Format("20060102T150405Z"),
		scope,
		sha256Hex([]byte(canonicalRequest)),
	}, "\n")
	signature := hmacSHA256(signingKey(c.secretKey, now.Format("20060102"), c.region), []byte(stringToSign))
	return hex.EncodeToString(signature)
}

// publicURL 返回对象对外访问地址；未配置公共域名时退回 endpoint 路径。
func (c *S3Client) publicURL(key string) string {
	if c.publicBaseURL == "" {
		return c.endpoint + "/" + c.bucket + "/" + key
	}
	return c.publicBaseURL + "/" + strings.TrimLeft(key, "/")
}

// canonicalQuery 按 AWS 规则对查询参数排序并编码。
func canonicalQuery(values url.Values) string {
	if len(values) == 0 {
		return ""
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0)
	for _, key := range keys {
		vals := append([]string(nil), values[key]...)
		sort.Strings(vals)
		for _, value := range vals {
			parts = append(parts, awsEscape(key)+"="+awsEscape(value))
		}
	}
	return strings.Join(parts, "&")
}

// awsEscape 做 URI 编码，空格写成 %20。
func awsEscape(value string) string {
	return strings.ReplaceAll(url.QueryEscape(value), "+", "%20")
}

// signingKey 派生 AWS4 签名密钥。
func signingKey(secret string, date string, region string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+secret), []byte(date))
	kRegion := hmacSHA256(kDate, []byte(region))
	kService := hmacSHA256(kRegion, []byte("s3"))
	return hmacSHA256(kService, []byte("aws4_request"))
}

// hmacSHA256 计算 HMAC-SHA256。
func hmacSHA256(key []byte, data []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return mac.Sum(nil)
}

// sha256Hex 返回 SHA256 十六进制摘要。
func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
