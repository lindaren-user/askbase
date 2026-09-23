package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"askbase/be/internal/config"
	"askbase/be/internal/llm"
	"askbase/be/internal/model"
)

const maxRouteQueryRunes = 200

// QueryType 表示查询进入回答链路前采用的检索策略。
type QueryType string

const (
	// QueryTypeNone 表示无需访问知识库。
	QueryTypeNone QueryType = "none"
	// QueryTypeDirect 表示直接使用原问题检索。
	QueryTypeDirect QueryType = "direct"
	// QueryTypeContextualRewrite 表示用原问题和上下文改写问题共同检索。
	QueryTypeContextualRewrite QueryType = "contextual_rewrite"
	// QueryTypeDecomposition 表示用原问题和多个独立子问题共同检索。
	QueryTypeDecomposition QueryType = "decomposition"
)

// QueryRouteInput 是查询路由器判断策略所需的对话输入。
type QueryRouteInput struct {
	Question string
	History  []model.Message
}

// QueryRouteDecision 是路由器返回的查询类型和派生查询。
type QueryRouteDecision struct {
	QueryType QueryType `json:"queryType"`
	Queries   []string  `json:"queries"`
}

// QueryRouter 为 LLM、Jev 等查询路由实现提供统一替换边界。
type QueryRouter interface {
	Route(ctx context.Context, input QueryRouteInput) (QueryRouteDecision, error)
}

type queryCompleter interface {
	Complete(ctx context.Context, messages []llm.ChatMessage, maxTokens int) (string, error)
}

// LLMQueryRouter 使用服务端模型完成查询分类、上下文改写和问题分解。
type LLMQueryRouter struct {
	llm           queryCompleter
	maxTokens     int
	maxSubqueries int
}

// NewLLMQueryRouter 创建基于服务端 LLM 的查询路由器。
func NewLLMQueryRouter(client queryCompleter, cfg config.ChatRouterConfig) *LLMQueryRouter {
	maxSubqueries := normalizeMaxSubqueries(cfg.MaxSubqueries)
	return &LLMQueryRouter{
		llm:           client,
		maxTokens:     cfg.MaxTokens,
		maxSubqueries: maxSubqueries,
	}
}

// Route 让模型只返回结构化路由结果，并在本地执行严格校验和归一化。
func (r *LLMQueryRouter) Route(ctx context.Context, input QueryRouteInput) (QueryRouteDecision, error) {
	if r.llm == nil {
		return QueryRouteDecision{}, fmt.Errorf("查询路由模型未配置")
	}
	messages := routeMessages(input, r.maxSubqueries)
	raw, err := r.llm.Complete(ctx, messages, r.maxTokens)
	if err != nil {
		return QueryRouteDecision{}, fmt.Errorf("查询路由失败: %w", err)
	}
	decision, err := parseRouteDecision(raw, input.Question, r.maxSubqueries)
	if err != nil {
		return QueryRouteDecision{}, fmt.Errorf("解析查询路由结果失败: %w", err)
	}
	return decision, nil
}

func routeSystemPrompt(maxSubqueries int) string {
	return fmt.Sprintf(`你是 AskBase 的检索查询规划器。分析最后一条用户消息；此前消息仅用于理解上下文。不要回答用户问题。

按以下互斥顺序选择 queryType：
1. none：消息不需要检索知识库，只属于寒暄、AskBase 操作或对话本身的元问题。
2. decomposition：消息要求检索两个或更多可以独立回答的方面。queries 返回 2 到 %d 个自包含查询。
3. contextual_rewrite：消息要求检索知识，但脱离历史后无法准确理解。queries 只返回 1 个自包含查询。
4. direct：消息要求检索知识，并且本身已经完整明确。queries 返回空数组。

改写要求：
- 解析代词、省略的主语、简称以及相对时间，使查询无需对话历史也能被理解；
- 保留用户原意、语言、专有名词、时间和范围约束，不回答问题，不加入历史中不存在的事实；
- 即使历史回答看起来已经包含答案，依赖历史的知识追问仍选择 contextual_rewrite；
- decomposition 的每个子查询也必须能够脱离对话历史独立理解；
- 无法从历史确定指代对象时不要猜测，保留用户原问题并选择 direct。

只输出下列结构的 JSON，不要输出解释、Markdown 或额外字段：
{"queryType":"none|direct|contextual_rewrite|decomposition","queries":["..."]}

none 和 direct 的 queries 必须为空数组。`, maxSubqueries)
}

// routeMessages 使用原生消息角色传递历史，避免把整段对话压进一条用户消息。
func routeMessages(input QueryRouteInput, maxSubqueries int) []llm.ChatMessage {
	messages := make([]llm.ChatMessage, 0, len(input.History)+2)
	messages = append(messages, llm.ChatMessage{Role: "system", Content: routeSystemPrompt(maxSubqueries)})
	for _, msg := range input.History {
		if msg.Role != model.MessageRoleUser && msg.Role != model.MessageRoleAssistant {
			continue
		}
		content := strings.TrimSpace(truncateRunes(msg.Content, 300))
		if content == "" {
			continue
		}
		messages = append(messages, llm.ChatMessage{Role: msg.Role, Content: content})
	}
	messages = append(messages, llm.ChatMessage{Role: model.MessageRoleUser, Content: strings.TrimSpace(input.Question)})
	return messages
}

func parseRouteDecision(raw string, original string, maxSubqueries int) (QueryRouteDecision, error) {
	jsonText := stripJSONCodeFence(raw)
	var decision QueryRouteDecision
	if err := json.Unmarshal([]byte(jsonText), &decision); err != nil {
		return QueryRouteDecision{}, err
	}
	return normalizeRouteDecision(decision, original, maxSubqueries)
}

func normalizeRouteDecision(
	decision QueryRouteDecision,
	original string,
	maxSubqueries int,
) (QueryRouteDecision, error) {
	decision.QueryType = QueryType(strings.TrimSpace(string(decision.QueryType)))
	decision.Queries = normalizeRouteQueries(decision.Queries, normalizeMaxSubqueries(maxSubqueries))

	switch decision.QueryType {
	case QueryTypeNone, QueryTypeDirect:
		decision.Queries = []string{}
	case QueryTypeContextualRewrite:
		if len(decision.Queries) != 1 {
			return QueryRouteDecision{}, fmt.Errorf("上下文改写必须返回一个有效查询")
		}
	case QueryTypeDecomposition:
		if len(decision.Queries) < 2 {
			return QueryRouteDecision{}, fmt.Errorf("问题分解必须返回至少两个有效子查询")
		}
	default:
		return QueryRouteDecision{}, fmt.Errorf("未知查询类型 %q", decision.QueryType)
	}

	if strings.TrimSpace(original) == "" {
		return QueryRouteDecision{}, fmt.Errorf("原问题为空")
	}
	return decision, nil
}

func normalizeMaxSubqueries(value int) int {
	if value < 2 {
		return 4
	}
	if value > 4 {
		return 4
	}
	return value
}

func stripJSONCodeFence(raw string) string {
	value := strings.TrimSpace(raw)
	if !strings.HasPrefix(value, "```") {
		return value
	}
	value = strings.TrimPrefix(value, "```")
	value = strings.TrimSpace(value)
	if strings.HasPrefix(strings.ToLower(value), "json") {
		value = strings.TrimSpace(value[len("json"):])
	}
	value = strings.TrimSuffix(value, "```")
	return strings.TrimSpace(value)
}

func normalizeRouteQueries(queries []string, limit int) []string {
	if limit <= 0 {
		limit = 1
	}
	result := make([]string, 0, min(len(queries), limit))
	seen := make(map[string]struct{}, len(queries))
	for _, query := range queries {
		query = strings.TrimSpace(query)
		if query == "" || len([]rune(query)) > maxRouteQueryRunes {
			continue
		}
		key := strings.ToLower(query)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, query)
		if len(result) == limit {
			break
		}
	}
	return result
}

func buildQueryPlan(original string, decision QueryRouteDecision, maxSubqueries int) []string {
	original = strings.TrimSpace(original)
	if decision.QueryType == QueryTypeNone || original == "" {
		return []string{}
	}
	if decision.QueryType == QueryTypeDirect {
		return []string{original}
	}
	maxSubqueries = normalizeMaxSubqueries(maxSubqueries)

	queries := []string{original}
	seen := map[string]struct{}{strings.ToLower(original): {}}
	for _, query := range decision.Queries {
		query = strings.TrimSpace(query)
		if query == "" || len([]rune(query)) > maxRouteQueryRunes {
			continue
		}
		key := strings.ToLower(query)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		queries = append(queries, query)
		if len(queries)-1 == maxSubqueries {
			break
		}
	}
	return queries
}
