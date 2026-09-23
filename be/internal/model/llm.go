package model

// 对话模型来源：official=服务端配置；custom=前端随请求携带的自备模型（BYOK）。
const (
	ModelSourceOfficial = "official"
	ModelSourceCustom   = "custom"
)

// ChatModelChoice 对话请求可选的模型覆盖；空或 official 用服务端默认模型。
type ChatModelChoice struct {
	Source  string `json:"source"`
	ModelID string `json:"modelId"`
	APIURL  string `json:"apiUrl"`
	APIKey  string `json:"apiKey"`
}

// EmbedModelChoice 嵌入模型覆盖；空或 official 用服务端配置。
// 注意：同一知识库的文档向量与查询向量必须用同一模型同一维度，否则检索失真。
type EmbedModelChoice struct {
	Source    string `json:"source"`
	ModelID   string `json:"modelId"`
	APIURL    string `json:"apiUrl"`
	APIKey    string `json:"apiKey"`
	Dimension int    `json:"dimension"`
}

// TestModelRequest 自定义模型连通性测试请求。
type TestModelRequest struct {
	ModelID string `json:"modelId"`
	APIURL  string `json:"apiUrl"`
	APIKey  string `json:"apiKey"`
}

// TestEmbedRequest 自定义嵌入模型连通性测试请求。
type TestEmbedRequest struct {
	ModelID string `json:"modelId"`
	APIURL  string `json:"apiUrl"`
	APIKey  string `json:"apiKey"`
}

// TestModelResponse 连通性测试结果。
type TestModelResponse struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}
