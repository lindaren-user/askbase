package config

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/viper"
)

const defaultConfigFile = "config.yaml"

const (
	EnvDev  = "dev"
	EnvProd = "prod"
)

// Config 应用全量配置；文件加载后由环境变量覆盖。
type Config struct {
	Env       string          `mapstructure:"env"`
	HTTP      HTTPConfig      `mapstructure:"http"`
	Postgres  PostgresConfig  `mapstructure:"postgres"`
	RabbitMQ  RabbitMQConfig  `mapstructure:"rabbitmq"`
	Queue     QueueConfig     `mapstructure:"queue"`
	Outbox    OutboxConfig    `mapstructure:"outbox"`
	Log       LogConfig       `mapstructure:"-"`
	Mail      MailConfig      `mapstructure:"mail"`
	Auth      AuthConfig      `mapstructure:"auth"`
	Storage   StorageConfig   `mapstructure:"storage"`
	Embedding EmbeddingConfig `mapstructure:"embedding"`
	Vision    VisionConfig    `mapstructure:"vision"`
	MinerU    MinerUConfig    `mapstructure:"mineru"`
	LLM       LLMConfig       `mapstructure:"llm"`
	Chat      ChatConfig      `mapstructure:"chat"`
}

type HTTPConfig struct {
	Host string `mapstructure:"host"`
	Port int    `mapstructure:"port"`
}

type PostgresConfig struct {
	Host            string `mapstructure:"host"`
	Port            int    `mapstructure:"port"`
	User            string `mapstructure:"user"`
	Password        string `mapstructure:"password"`
	Database        string `mapstructure:"database"`
	SSLMode         string `mapstructure:"sslMode"`
	MaxOpenConns    int    `mapstructure:"maxOpenConns"`
	MaxIdleConns    int    `mapstructure:"maxIdleConns"`
	ConnMaxLifetime string `mapstructure:"connMaxLifetime"`
}

// RabbitMQConfig RabbitMQ 连接配置。
type RabbitMQConfig struct {
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`
	VHost    string `mapstructure:"vhost"`
}

// QueueConfig RabbitMQ 文档解析队列配置。
type QueueConfig struct {
	Exchange         string `mapstructure:"exchange"`
	Name             string `mapstructure:"name"`
	RoutingKey       string `mapstructure:"routingKey"`
	RetryQueue       string `mapstructure:"retryQueue"`
	RetryRoutingKey  string `mapstructure:"retryRoutingKey"`
	DeadExchange     string `mapstructure:"deadExchange"`
	DeadQueue        string `mapstructure:"deadQueue"`
	DeadRoutingKey   string `mapstructure:"deadRoutingKey"`
	RetryDelayMs     int64  `mapstructure:"retryDelayMs"`
	DeadRetentionMs  int64  `mapstructure:"deadRetentionMs"`
	MaxDeliveries    int    `mapstructure:"maxDeliveries"`
	Concurrency      int    `mapstructure:"concurrency"`
	Prefetch         int    `mapstructure:"prefetch"`
	ConfirmTimeoutMs int64  `mapstructure:"confirmTimeoutMs"`
}

// OutboxConfig relay 轮询、退避与清理配置。
type OutboxConfig struct {
	PollIntervalMs int64 `mapstructure:"pollIntervalMs"`
	MaxBackoffMs   int64 `mapstructure:"maxBackoffMs"`
	RetentionHours int   `mapstructure:"retentionHours"`
}

// LogConfig 日志配置，由 env 推导，不落在 yaml。
type LogConfig struct {
	Development bool
	File        string
}

type AuthConfig struct {
	Secret     string          `mapstructure:"secret"`
	CookieName string          `mapstructure:"cookieName"`
	DevCode    string          `mapstructure:"devCode"` // 仅 dev 环境生效的万能验证码，便于本地演示；生产必须留空
	Turnstile  TurnstileConfig `mapstructure:"turnstile"`
}

type TurnstileConfig struct {
	SiteKey   string `mapstructure:"siteKey"`
	SecretKey string `mapstructure:"secretKey"`
}

type MailConfig struct {
	Provider string       `mapstructure:"provider"`
	SMTP     SMTPConfig   `mapstructure:"smtp"`
	Resend   ResendConfig `mapstructure:"resend"`
}

type SMTPConfig struct {
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`
}

type ResendConfig struct {
	ApiKey string `mapstructure:"api_key"`
	From   string `mapstructure:"from"`
}

type StorageConfig struct {
	S3 S3StorageConfig `mapstructure:"s3"`
}

type S3StorageConfig struct {
	Endpoint      string `mapstructure:"endpoint"`
	Region        string `mapstructure:"region"`
	Bucket        string `mapstructure:"bucket"`
	AccessKey     string `mapstructure:"accessKey"`
	SecretKey     string `mapstructure:"secretKey"`
	PublicBaseURL string `mapstructure:"publicBaseUrl"`
	Prefix        string `mapstructure:"prefix"`
}

// EmbeddingConfig OpenAI 兼容嵌入服务。
type EmbeddingConfig struct {
	Provider  string `mapstructure:"provider"`
	APIURL    string `mapstructure:"apiUrl"`
	APIKey    string `mapstructure:"apiKey"`
	Model     string `mapstructure:"model"`
	Dimension int    `mapstructure:"dimension"`
	BatchSize int    `mapstructure:"batchSize"`
}

// VisionConfig OpenAI 兼容视觉模型（服务端默认 VLM）。
type VisionConfig struct {
	APIURL string `mapstructure:"apiUrl"`
	APIKey string `mapstructure:"apiKey"`
	Model  string `mapstructure:"model"`
}

// MinerUConfig MinerU 在线精准解析 API 配置。
type MinerUConfig struct {
	Enabled        bool   `mapstructure:"enabled"`
	APIURL         string `mapstructure:"apiUrl"`
	Token          string `mapstructure:"token"`
	ModelVersion   string `mapstructure:"modelVersion"`
	Language       string `mapstructure:"language"`
	OCR            bool   `mapstructure:"ocr"`
	EnableFormula  bool   `mapstructure:"enableFormula"`
	EnableTable    bool   `mapstructure:"enableTable"`
	PollIntervalMs int64  `mapstructure:"pollIntervalMs"`
	TimeoutSeconds int64  `mapstructure:"timeoutSeconds"`
}

// LLMConfig OpenAI 兼容对话服务（SSE 流式）。
type LLMConfig struct {
	APIURL              string `mapstructure:"apiUrl"`
	APIKey              string `mapstructure:"apiKey"`
	Model               string `mapstructure:"model"`
	ContextWindowTokens int    `mapstructure:"contextWindowTokens"`
}

// ChatConfig 汇总查询路由与知识库检索参数。
type ChatConfig struct {
	Router    ChatRouterConfig    `mapstructure:"router"`
	Retrieval ChatRetrievalConfig `mapstructure:"retrieval"`
}

// ChatRouterConfig 查询路由模型的输出上限和最大分解数量。
type ChatRouterConfig struct {
	MaxTokens     int `mapstructure:"maxTokens"`
	MaxSubqueries int `mapstructure:"maxSubqueries"`
}

// ChatRetrievalConfig 检索参数：向量 topK 与余弦相似度阈值。
type ChatRetrievalConfig struct {
	TopK     int     `mapstructure:"topK"`
	MinScore float64 `mapstructure:"minScore"`
}

// Load 读取配置：先加载文件，再用环境变量覆盖。
// 找不到配置文件时（如容器内纯环境变量部署）退化为默认值 + 环境变量。
func Load() (Config, error) {
	path, err := resolveConfigPath()

	v := viper.New()
	v.SetConfigType("yaml")
	v.SetDefault("mineru.ocr", true)
	v.SetDefault("mineru.enableFormula", true)
	v.SetDefault("mineru.enableTable", true)
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
	bindEnvs(v)

	cfg := defaultConfig()
	if err != nil {
		log.Printf("config: 未找到 config.yaml，使用默认值 + 环境变量（%v）", err)
	} else {
		v.SetConfigFile(path)
		if err := v.ReadInConfig(); err != nil {
			return Config{}, fmt.Errorf("读取配置文件失败: %w", err)
		}
	}
	if err := v.Unmarshal(&cfg); err != nil {
		return Config{}, err
	}

	cfg.applyDefaults()
	return cfg, nil
}

func resolveConfigPath() (string, error) {
	if path := strings.TrimSpace(os.Getenv("CONFIG_FILE")); path != "" {
		if _, err := os.Stat(path); err != nil {
			return "", fmt.Errorf("配置文件不存在: %s", path)
		}
		return path, nil
	}

	var dirs []string
	if cwd, err := os.Getwd(); err == nil {
		dirs = append(dirs, walkParents(cwd)...)
	}
	if exe, err := os.Executable(); err == nil {
		dirs = append(dirs, walkParents(filepath.Dir(exe))...)
	}

	seen := make(map[string]struct{})
	for _, dir := range dirs {
		if _, ok := seen[dir]; ok {
			continue
		}
		seen[dir] = struct{}{}
		path := filepath.Join(dir, defaultConfigFile)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, nil
		}
	}

	return "", errors.New("未找到 config.yaml，请在 be 目录运行或设置 CONFIG_FILE")
}

func walkParents(start string) []string {
	var dirs []string
	dir, err := filepath.Abs(start)
	if err != nil {
		return nil
	}
	for {
		dirs = append(dirs, dir)
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return dirs
}

func bindEnvs(v *viper.Viper) {
	_ = v.BindEnv("env", "APP_ENV")
	_ = v.BindEnv("http.host", "HTTP_HOST")
	_ = v.BindEnv("http.port", "HTTP_PORT")
	_ = v.BindEnv("postgres.host", "POSTGRES_HOST")
	_ = v.BindEnv("postgres.port", "POSTGRES_PORT")
	_ = v.BindEnv("postgres.user", "POSTGRES_USER")
	_ = v.BindEnv("postgres.password", "POSTGRES_PASSWORD")
	_ = v.BindEnv("postgres.database", "POSTGRES_DB")
	_ = v.BindEnv("postgres.sslMode", "POSTGRES_SSLMODE")
	_ = v.BindEnv("rabbitmq.host", "RABBITMQ_HOST")
	_ = v.BindEnv("rabbitmq.port", "RABBITMQ_PORT")
	_ = v.BindEnv("rabbitmq.username", "RABBITMQ_USERNAME")
	_ = v.BindEnv("rabbitmq.password", "RABBITMQ_PASSWORD")
	_ = v.BindEnv("rabbitmq.vhost", "RABBITMQ_VHOST")
	_ = v.BindEnv("queue.concurrency", "QUEUE_CONCURRENCY")
	_ = v.BindEnv("queue.prefetch", "QUEUE_PREFETCH")
	_ = v.BindEnv("queue.retryDelayMs", "QUEUE_RETRY_DELAY_MS")
	_ = v.BindEnv("queue.maxDeliveries", "QUEUE_MAX_DELIVERIES")
	_ = v.BindEnv("outbox.pollIntervalMs", "OUTBOX_POLL_INTERVAL_MS")
	_ = v.BindEnv("auth.secret", "AUTH_SECRET")
	_ = v.BindEnv("auth.cookieName", "AUTH_COOKIE_NAME")
	_ = v.BindEnv("auth.devCode", "AUTH_DEV_CODE")
	_ = v.BindEnv("auth.turnstile.siteKey", "TURNSTILE_SITE_KEY")
	_ = v.BindEnv("auth.turnstile.secretKey", "TURNSTILE_SECRET_KEY")
	_ = v.BindEnv("mail.provider", "MAIL_PROVIDER")
	_ = v.BindEnv("mail.smtp.host", "SMTP_HOST")
	_ = v.BindEnv("mail.smtp.port", "SMTP_PORT")
	_ = v.BindEnv("mail.smtp.username", "SMTP_USERNAME")
	_ = v.BindEnv("mail.smtp.password", "SMTP_PASSWORD")
	_ = v.BindEnv("mail.resend.api_key", "RESEND_API_KEY")
	_ = v.BindEnv("mail.resend.from", "RESEND_FROM")
	_ = v.BindEnv("storage.s3.endpoint", "STORAGE_S3_ENDPOINT")
	_ = v.BindEnv("storage.s3.region", "STORAGE_S3_REGION")
	_ = v.BindEnv("storage.s3.bucket", "STORAGE_S3_BUCKET")
	_ = v.BindEnv("storage.s3.accessKey", "STORAGE_S3_ACCESS_KEY")
	_ = v.BindEnv("storage.s3.secretKey", "STORAGE_S3_SECRET_KEY")
	_ = v.BindEnv("storage.s3.publicBaseUrl", "STORAGE_S3_PUBLIC_BASE_URL")
	_ = v.BindEnv("storage.s3.prefix", "STORAGE_S3_PREFIX")
	_ = v.BindEnv("embedding.apiUrl", "EMBEDDING_API_URL")
	_ = v.BindEnv("embedding.apiKey", "EMBEDDING_API_KEY")
	_ = v.BindEnv("embedding.model", "EMBEDDING_MODEL")
	_ = v.BindEnv("embedding.dimension", "EMBEDDING_DIMENSION")
	_ = v.BindEnv("embedding.batchSize", "EMBEDDING_BATCH_SIZE")
	_ = v.BindEnv("vision.apiUrl", "VISION_API_URL")
	_ = v.BindEnv("vision.apiKey", "VISION_API_KEY")
	_ = v.BindEnv("vision.model", "VISION_MODEL")
	_ = v.BindEnv("mineru.enabled", "MINERU_ENABLED")
	_ = v.BindEnv("mineru.apiUrl", "MINERU_API_URL")
	_ = v.BindEnv("mineru.token", "MINERU_TOKEN")
	_ = v.BindEnv("mineru.modelVersion", "MINERU_MODEL_VERSION")
	_ = v.BindEnv("mineru.language", "MINERU_LANGUAGE")
	_ = v.BindEnv("mineru.ocr", "MINERU_OCR")
	_ = v.BindEnv("mineru.enableFormula", "MINERU_ENABLE_FORMULA")
	_ = v.BindEnv("mineru.enableTable", "MINERU_ENABLE_TABLE")
	_ = v.BindEnv("mineru.pollIntervalMs", "MINERU_POLL_INTERVAL_MS")
	_ = v.BindEnv("mineru.timeoutSeconds", "MINERU_TIMEOUT_SECONDS")
	_ = v.BindEnv("llm.apiUrl", "LLM_API_URL")
	_ = v.BindEnv("llm.apiKey", "LLM_API_KEY")
	_ = v.BindEnv("llm.model", "LLM_MODEL")
	_ = v.BindEnv("chat.router.maxTokens", "CHAT_ROUTER_MAX_TOKENS")
	_ = v.BindEnv("chat.router.maxSubqueries", "CHAT_ROUTER_MAX_SUBQUERIES")
	_ = v.BindEnv("chat.retrieval.topK", "CHAT_RETRIEVAL_TOP_K")
	_ = v.BindEnv("chat.retrieval.minScore", "CHAT_RETRIEVAL_MIN_SCORE")
}

func defaultConfig() Config {
	cfg := Config{}
	cfg.applyDefaults()
	return cfg
}

func (c *Config) applyDefaults() {
	c.Env = strings.TrimSpace(strings.ToLower(c.Env))
	if c.Env != EnvProd {
		c.Env = EnvDev
	}
	if c.HTTP.Host == "" {
		c.HTTP.Host = "127.0.0.1"
	}
	if c.HTTP.Port == 0 {
		c.HTTP.Port = 8082
	}
	if c.Postgres.Host == "" {
		c.Postgres.Host = "localhost"
	}
	if c.Postgres.Port == 0 {
		c.Postgres.Port = 5434
	}
	if c.Postgres.User == "" {
		c.Postgres.User = "askbase"
	}
	if c.Postgres.Password == "" {
		c.Postgres.Password = "askbase_password"
	}
	if c.Postgres.Database == "" {
		c.Postgres.Database = "askbase"
	}
	if c.Postgres.SSLMode == "" {
		c.Postgres.SSLMode = "disable"
	}
	if c.Postgres.MaxOpenConns == 0 {
		c.Postgres.MaxOpenConns = 20
	}
	if c.Postgres.MaxIdleConns == 0 {
		c.Postgres.MaxIdleConns = 5
	}
	if c.Postgres.ConnMaxLifetime == "" {
		c.Postgres.ConnMaxLifetime = "30m"
	}
	if c.RabbitMQ.Host == "" {
		c.RabbitMQ.Host = "localhost"
	}
	if c.RabbitMQ.Port == 0 {
		c.RabbitMQ.Port = 5672
	}
	if c.RabbitMQ.Username == "" {
		c.RabbitMQ.Username = "askbase"
	}
	if c.RabbitMQ.Password == "" {
		c.RabbitMQ.Password = "askbase_password"
	}
	if c.RabbitMQ.VHost == "" {
		c.RabbitMQ.VHost = "/"
	}
	if c.Queue.Exchange == "" {
		c.Queue.Exchange = "askbase.documents"
	}
	if c.Queue.Name == "" {
		c.Queue.Name = "askbase.document.parse"
	}
	if c.Queue.RoutingKey == "" {
		c.Queue.RoutingKey = "document.parse"
	}
	if c.Queue.RetryQueue == "" {
		c.Queue.RetryQueue = "askbase.document.parse.retry"
	}
	if c.Queue.RetryRoutingKey == "" {
		c.Queue.RetryRoutingKey = "document.parse.retry"
	}
	if c.Queue.DeadExchange == "" {
		c.Queue.DeadExchange = "askbase.documents.dead"
	}
	if c.Queue.DeadQueue == "" {
		c.Queue.DeadQueue = "askbase.document.parse.dead"
	}
	if c.Queue.DeadRoutingKey == "" {
		c.Queue.DeadRoutingKey = "document.parse.dead"
	}
	if c.Queue.RetryDelayMs <= 0 {
		c.Queue.RetryDelayMs = 60000
	}
	if c.Queue.DeadRetentionMs <= 0 {
		c.Queue.DeadRetentionMs = int64((7 * 24 * time.Hour) / time.Millisecond)
	}
	if c.Queue.MaxDeliveries <= 0 {
		c.Queue.MaxDeliveries = 5
	}
	if c.Queue.Concurrency <= 0 {
		c.Queue.Concurrency = 1
	}
	if c.Queue.Prefetch <= 0 {
		c.Queue.Prefetch = c.Queue.Concurrency
	}
	if c.Queue.ConfirmTimeoutMs <= 0 {
		c.Queue.ConfirmTimeoutMs = 5000
	}
	if c.Outbox.PollIntervalMs <= 0 {
		c.Outbox.PollIntervalMs = 1000
	}
	if c.Outbox.MaxBackoffMs <= 0 {
		c.Outbox.MaxBackoffMs = 60000
	}
	if c.Outbox.RetentionHours <= 0 {
		c.Outbox.RetentionHours = 24 * 7
	}
	if c.Auth.Secret == "" {
		c.Auth.Secret = "dev-auth-secret"
	}
	if c.Auth.CookieName == "" {
		c.Auth.CookieName = "askbase_session"
	}
	if c.Storage.S3.Region == "" {
		c.Storage.S3.Region = "auto"
	}
	if c.Storage.S3.Prefix == "" {
		c.Storage.S3.Prefix = "askbase"
	}
	if c.Embedding.APIURL == "" {
		c.Embedding.APIURL = "https://api.siliconflow.cn/v1"
	}
	if c.Embedding.Model == "" {
		c.Embedding.Model = "BAAI/bge-m3"
	}
	if c.Embedding.Dimension == 0 {
		c.Embedding.Dimension = 1024
	}
	if c.Embedding.BatchSize <= 0 {
		c.Embedding.BatchSize = 32
	}
	if c.Vision.APIURL == "" {
		c.Vision.APIURL = "https://openrouter.ai/api/v1"
	}
	if c.Vision.Model == "" {
		c.Vision.Model = "google/gemma-4-31b-it:free"
	}
	if c.MinerU.APIURL == "" {
		c.MinerU.APIURL = "https://mineru.net"
	}
	if c.MinerU.ModelVersion == "" {
		c.MinerU.ModelVersion = "vlm"
	}
	if c.MinerU.Language == "" {
		c.MinerU.Language = "ch"
	}
	if c.MinerU.PollIntervalMs <= 0 {
		c.MinerU.PollIntervalMs = 3000
	}
	if c.MinerU.TimeoutSeconds <= 0 {
		c.MinerU.TimeoutSeconds = 900
	}
	if c.LLM.ContextWindowTokens <= 0 {
		c.LLM.ContextWindowTokens = 256000
	}
	if c.Chat.Retrieval.TopK <= 0 {
		c.Chat.Retrieval.TopK = 10
	}
	if c.Chat.Retrieval.MinScore <= 0 {
		c.Chat.Retrieval.MinScore = 0.20
	}
	if c.Chat.Router.MaxTokens <= 0 {
		c.Chat.Router.MaxTokens = 512
	}
	if c.Chat.Router.MaxSubqueries < 2 {
		c.Chat.Router.MaxSubqueries = 4
	} else if c.Chat.Router.MaxSubqueries > 4 {
		c.Chat.Router.MaxSubqueries = 4
	}
	c.Log = logConfigFromEnv(c.Env)
}

func logConfigFromEnv(env string) LogConfig {
	if env == EnvProd {
		return LogConfig{
			Development: false,
			File:        "logs/app.log",
		}
	}
	return LogConfig{Development: true}
}

// ListenAddr 返回 HTTP 监听地址。
func (c HTTPConfig) ListenAddr() string {
	return c.Host + ":" + strconv.Itoa(c.Port)
}

// ConfirmTimeout 返回等待 publisher confirm 的最长时间。
func (c QueueConfig) ConfirmTimeout() time.Duration {
	return time.Duration(c.ConfirmTimeoutMs) * time.Millisecond
}

// PollInterval 返回 relay 空闲时的轮询间隔。
func (c OutboxConfig) PollInterval() time.Duration {
	return time.Duration(c.PollIntervalMs) * time.Millisecond
}

// MaxBackoff 返回 Outbox 发布失败的最大退避时间。
func (c OutboxConfig) MaxBackoff() time.Duration {
	return time.Duration(c.MaxBackoffMs) * time.Millisecond
}

// Retention 返回已发布 Outbox 的保留时间。
func (c OutboxConfig) Retention() time.Duration {
	return time.Duration(c.RetentionHours) * time.Hour
}
