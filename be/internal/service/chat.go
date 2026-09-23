package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"askbase/be/internal/llm"
	"askbase/be/internal/model"
	"askbase/be/internal/repo"

	"go.uber.org/zap"
)

// TODO: 改为基于 token 水位线的滑动窗口，并压缩窗口外历史，避免长会话超过上下文限制。
// 多轮对话当前最多取最近十条消息供路由和最终回答参考。
const chatHistoryLimit = 10

var citationPattern = regexp.MustCompile(`\[(\d{1,3})\]`)

// ChatEvent 是对外发送的流式事件。
type ChatEvent struct {
	Type               string
	SessionID          int64
	UserMessageID      int64
	AssistantMessageID int64
	Content            string
	FinishReason       string
	Citations          []model.Citation
	Err                error
}

type chatStreamer interface {
	ChatStream(ctx context.Context, messages []llm.ChatMessage, maxTokens int) <-chan llm.StreamEvent
}

type chatRetriever interface {
	SearchQueries(ctx context.Context, userID int64, datasetID int64, queries []string, topK int, minScore float64) ([]model.RetrievalHit, error)
}

type chatSessionStore interface {
	GetByUser(ctx context.Context, userID int64, id int64) (model.Session, error)
	Update(ctx context.Context, userID int64, id int64, title *string, status *int16) error
	Touch(ctx context.Context, id int64) error
}

type chatMessageStore interface {
	Create(ctx context.Context, msg model.Message) (model.Message, error)
	ListBySession(ctx context.Context, sessionID int64) ([]model.Message, error)
	UpdateContent(ctx context.Context, id int64, content string, citations []model.Citation) error
}

// ChatService 编排查询路由、混合检索、流式回答和消息落库。
type ChatService struct {
	sessions  chatSessionStore
	messages  chatMessageStore
	datasets  datasetByUserStore
	retrieval chatRetriever
	planner   QueryPlanner
	llm       chatStreamer
}

// NewChatService 创建对话服务。
func NewChatService(
	sessions chatSessionStore,
	messages chatMessageStore,
	datasets datasetByUserStore,
	retrieval chatRetriever,
	planner QueryPlanner,
	llmClient chatStreamer,
) *ChatService {
	return &ChatService{
		sessions:  sessions,
		messages:  messages,
		datasets:  datasets,
		retrieval: retrieval,
		planner:   planner,
		llm:       llmClient,
	}
}

// ChatStream 执行一轮 RAG 对话并返回事件通道。
// 用户消息与助手占位消息会先落库，首个元事件携带两个消息 ID。
func (s *ChatService) ChatStream(
	ctx context.Context,
	userID int64,
	req model.ChatCompletionRequest,
) (<-chan ChatEvent, error) {
	content := strings.TrimSpace(req.Content)
	if content == "" {
		return nil, fmt.Errorf("%w: 问题不能为空", ErrInvalidInput)
	}
	if err := ValidateChoice(req.Model); err != nil {
		return nil, err
	}
	streamer := s.resolveStreamer(req.Model)
	session, err := s.sessions.GetByUser(ctx, userID, req.SessionID)
	if err != nil {
		return nil, asNotFound(err, repo.ErrSessionNotFound)
	}
	if _, err := s.datasets.GetByUser(ctx, userID, session.DatasetID); err != nil {
		return nil, asNotFound(err, repo.ErrDatasetNotFound)
	}

	userMsg, err := s.messages.Create(ctx, model.Message{
		SessionID: session.ID,
		Role:      model.MessageRoleUser,
		Content:   content,
	})
	if err != nil {
		return nil, err
	}
	assistantMsg, err := s.messages.Create(ctx, model.Message{
		SessionID: session.ID,
		Role:      model.MessageRoleAssistant,
		Content:   "",
	})
	if err != nil {
		return nil, err
	}
	if session.Title == "" || session.Title == "新会话" {
		title := []rune(content)
		if len(title) > 30 {
			title = title[:30]
		}
		if err := s.sessions.Update(ctx, userID, session.ID, ptrString(string(title)), nil); err != nil {
			zap.L().Warn("更新会话标题失败", zap.Int64("sessionId", session.ID), zap.Error(err))
		}
	}
	_ = s.sessions.Touch(ctx, session.ID)

	events := make(chan ChatEvent, 32)
	go s.run(ctx, session, userMsg, assistantMsg, content, streamer, events)
	return events, nil
}

// resolveStreamer 为最终回答选择模型；查询路由始终使用独立的服务端模型。
func (s *ChatService) resolveStreamer(choice *model.ChatModelChoice) chatStreamer {
	if choice == nil || choice.Source != model.ModelSourceCustom {
		return s.llm
	}
	return llm.New(choice.APIURL, choice.APIKey, choice.ModelID, "", "", "")
}

// run 编排一轮完整的 RAG 问答，并把执行过程转换为流式事件。
func (s *ChatService) run(
	ctx context.Context,
	session model.Session,
	userMsg model.Message,
	assistantMsg model.Message,
	question string,
	streamer chatStreamer,
	events chan<- ChatEvent,
) {
	defer close(events)
	log := zap.L().With(zap.Int64("sessionId", session.ID))

	// 1. 先发送本轮元信息，让调用方建立用户消息与助手占位消息的关联。
	events <- ChatEvent{
		Type:               "meta",
		SessionID:          session.ID,
		UserMessageID:      userMsg.ID,
		AssistantMessageID: assistantMsg.ID,
	}

	// 2. 加载最近对话历史，并排除本轮刚写入的消息和空助手占位消息。
	history, err := s.recentHistory(ctx, session.ID, userMsg.ID)
	if err != nil {
		events <- ChatEvent{Type: "error", Err: fmt.Errorf("加载对话历史失败: %w", err)}
		return
	}

	// 3. 根据问题和历史生成查询计划；路由不可用时使用原问题直接检索。
	plan := QueryPlan{QueryType: QueryTypeDirect, Queries: []string{question}, Fallback: true}
	if s.planner != nil {
		plan, err = s.planner.Plan(ctx, QueryRouteInput{Question: question, History: history})
		if err != nil {
			log.Warn("查询路由失败，已回退为直接检索", zap.Error(err))
		}
	}
	log.Info(
		"查询路由完成",
		zap.String("queryType", string(plan.QueryType)),
		zap.Bool("fallback", plan.Fallback),
		zap.Int("queryCount", len(plan.Queries)),
		zap.Strings("queries", plan.Queries),
	)

	// 4. 对需要知识库支撑的问题执行多查询混合检索，并转换为稳定的引用编号。
	var hits []model.RetrievalHit
	if plan.QueryType != QueryTypeNone {
		hits, err = s.retrieval.SearchQueries(ctx, session.UserID, session.DatasetID, plan.Queries, 0, 0)
		if err != nil {
			events <- ChatEvent{Type: "error", Err: fmt.Errorf("检索失败: %w", err)}
			return
		}
	}
	citations := hitsToCitations(hits)

	// 5. 组装系统提示词、历史和检索上下文，逐段转发模型生成内容。
	messages := buildChatMessages(history, question, citations, plan.QueryType)
	var answer strings.Builder
	finishReason := "stop"
	stream := streamer.ChatStream(ctx, messages, 0)
	for event := range stream {
		if event.Err != nil {
			fields := []zap.Field{
				zap.Int64("userMessageId", userMsg.ID),
				zap.Int64("assistantMessageId", assistantMsg.ID),
				zap.Int("partialBytes", answer.Len()),
				zap.Error(event.Err),
			}
			if errors.Is(event.Err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
				log.Info("生成模型回复已取消", fields...)
			} else {
				log.Error("生成模型回复失败", fields...)
			}
			events <- ChatEvent{Type: "error", Err: event.Err}
			return
		}
		if event.Content != "" {
			answer.WriteString(event.Content)
			events <- ChatEvent{Type: "delta", Content: event.Content}
		}
		if event.Done {
			if event.FinishReason != "" {
				finishReason = event.FinishReason
			}
			break
		}
	}

	// 6. 汇总完整回答与引用分块，回填助手消息后发送最终完成事件。
	finalContent := answer.String()
	// TODO: 增加引用后处理：校验并修复模型生成的 [n]；模型未生成引用时，
	// 按句计算回答与检索分块的词项及向量相似度，自动补充引用。
	if strings.TrimSpace(finalContent) == "" {
		finalContent = "抱歉，本次没有生成有效回答，请重试。"
	}
	citations = filterCitationsByAnswer(finalContent, citations)
	if err := s.messages.UpdateContent(ctx, assistantMsg.ID, finalContent, citations); err != nil {
		log.Error("回填助手消息失败", zap.Error(err))
	}
	events <- ChatEvent{
		Type:         "done",
		FinishReason: finishReason,
		Citations:    citations,
		Content:      finalContent,
	}
}

// filterCitationsByAnswer 只保留回答正文实际使用且存在于检索结果中的引用。
func filterCitationsByAnswer(answer string, citations []model.Citation) []model.Citation {
	used := make(map[int]struct{})
	for _, match := range citationPattern.FindAllStringSubmatch(answer, -1) {
		n, err := strconv.Atoi(match[1])
		if err == nil {
			used[n] = struct{}{}
		}
	}
	filtered := make([]model.Citation, 0, len(used))
	for _, citation := range citations {
		if _, ok := used[citation.N]; ok {
			filtered = append(filtered, citation)
		}
	}
	return filtered
}

// recentHistory 获取会话最近若干条非空消息，并排除本轮新建消息。
func (s *ChatService) recentHistory(
	ctx context.Context,
	sessionID int64,
	excludeID int64,
) ([]model.Message, error) {
	all, err := s.messages.ListBySession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	filtered := make([]model.Message, 0, len(all))
	for _, msg := range all {
		if msg.ID >= excludeID || strings.TrimSpace(msg.Content) == "" {
			continue
		}
		filtered = append(filtered, msg)
	}
	if len(filtered) > chatHistoryLimit {
		filtered = filtered[len(filtered)-chatHistoryLimit:]
	}
	return filtered, nil
}

func hitsToCitations(hits []model.RetrievalHit) []model.Citation {
	citations := make([]model.Citation, 0, len(hits))
	for i, hit := range hits {
		citations = append(citations, model.Citation{
			N:            i + 1,
			ChunkID:      hit.ChunkID,
			DocumentID:   hit.DocumentID,
			DocumentName: hit.DocumentName,
			Content:      hit.Content,
			Score:        hit.Score,
			ImageURL:     hit.ImageURL,
		})
	}
	return citations
}

// buildChatMessages 组装系统提示词、历史消息和最新问题。
func buildChatMessages(
	history []model.Message,
	question string,
	citations []model.Citation,
	queryType QueryType,
) []llm.ChatMessage {
	var systemPrompt string
	if queryType == QueryTypeNone {
		systemPrompt = `你是私有知识库产品 AskBase 的助手。本轮没有检索文档。
你只能处理寒暄、说明 AskBase 的基本操作，或依据当前对话中已经明确出现的信息回答。
禁止使用模型自身知识回答外部事实、专业知识或用户私有文档中的事实；遇到此类问题，请说明需要查询知识库后才能回答。`
	} else if len(citations) > 0 {
		var contextBuilder strings.Builder
		for _, citation := range citations {
			fmt.Fprintf(
				&contextBuilder,
				"[%d] 文档《%s》：\n%s\n\n",
				citation.N,
				citation.DocumentName,
				citation.Content,
			)
		}
		systemPrompt = fmt.Sprintf(`你是个人知识库问答助手。基于下面的检索片段回答用户问题。
要求：
1. 回答中引用片段时，在句末标注 [n]，n 为片段编号；
2. 片段不足以回答时，明确说明“根据已有资料无法回答”，不要编造；
3. 使用中文简洁、有条理地回答，可以使用 Markdown。

检索片段：
%s`, contextBuilder.String())
	} else {
		systemPrompt = `你是个人知识库问答助手。当前知识库没有检索到相关内容，请明确告知用户“根据已有资料无法回答该问题”，并建议用户补充文档。不要编造答案。`
	}

	messages := []llm.ChatMessage{{Role: "system", Content: systemPrompt}}
	for _, msg := range history {
		messages = append(messages, llm.ChatMessage{Role: msg.Role, Content: msg.Content})
	}
	messages = append(messages, llm.ChatMessage{Role: "user", Content: question})
	return messages
}

func ptrString(value string) *string {
	return &value
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…"
}
