package service

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"askbase/be/internal/config"
	"askbase/be/internal/llm"
	"askbase/be/internal/model"
)

type fakeQueryCompleter struct {
	response  string
	err       error
	messages  []llm.ChatMessage
	maxTokens int
}

func (f *fakeQueryCompleter) Complete(_ context.Context, messages []llm.ChatMessage, maxTokens int) (string, error) {
	f.messages = messages
	f.maxTokens = maxTokens
	return f.response, f.err
}

func TestLLMQueryRouterRoute(t *testing.T) {
	tests := []struct {
		name     string
		response string
		want     QueryRouteDecision
		wantErr  bool
	}{
		{
			name:     "none",
			response: `{"queryType":"none","queries":[]}`,
			want:     QueryRouteDecision{QueryType: QueryTypeNone, Queries: []string{}},
		},
		{
			name:     "direct ignores queries",
			response: `{"queryType":"direct","queries":["不应保留"]}`,
			want:     QueryRouteDecision{QueryType: QueryTypeDirect, Queries: []string{}},
		},
		{
			name:     "fenced contextual rewrite",
			response: "```json\n{\"queryType\":\"contextual_rewrite\",\"queries\":[\"AskBase 的部署要求\"]}\n```",
			want: QueryRouteDecision{
				QueryType: QueryTypeContextualRewrite,
				Queries:   []string{"AskBase 的部署要求"},
			},
		},
		{
			name:     "decomposition deduplicates and limits",
			response: `{"queryType":"decomposition","queries":["架构","架构","配置","测试","部署"]}`,
			want: QueryRouteDecision{
				QueryType: QueryTypeDecomposition,
				Queries:   []string{"架构", "配置", "测试"},
			},
		},
		{name: "invalid json", response: `not json`, wantErr: true},
		{name: "unknown type", response: `{"queryType":"hyde","queries":[]}`, wantErr: true},
		{name: "empty rewrite", response: `{"queryType":"contextual_rewrite","queries":[""]}`, wantErr: true},
		{name: "duplicate decomposition", response: `{"queryType":"decomposition","queries":["相同","相同"]}`, wantErr: true},
		{
			name:     "overlong rewrite",
			response: `{"queryType":"contextual_rewrite","queries":["` + strings.Repeat("长", maxRouteQueryRunes+1) + `"]}`,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeQueryCompleter{response: tt.response}
			router := NewLLMQueryRouter(client, config.ChatRouterConfig{MaxTokens: 321, MaxSubqueries: 3})
			got, err := router.Route(context.Background(), QueryRouteInput{Question: "问题"})
			if tt.wantErr {
				if err == nil {
					t.Fatal("Route() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("Route() error = %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Route() = %#v, want %#v", got, tt.want)
			}
			if client.maxTokens != 321 {
				t.Fatalf("maxTokens = %d, want 321", client.maxTokens)
			}
		})
	}
}

func TestLLMQueryRouterReturnsProviderError(t *testing.T) {
	router := NewLLMQueryRouter(
		&fakeQueryCompleter{err: errors.New("provider unavailable")},
		config.ChatRouterConfig{MaxTokens: 10, MaxSubqueries: 4},
	)
	if _, err := router.Route(context.Background(), QueryRouteInput{Question: "问题"}); err == nil {
		t.Fatal("Route() error = nil, want provider error")
	}
}

func TestRouteSystemPromptDefinesStandaloneQueryRules(t *testing.T) {
	prompt := routeSystemPrompt(4)
	for _, rule := range []string{
		"按以下互斥顺序选择 queryType",
		"依赖历史的知识追问仍选择 contextual_rewrite",
		"使查询无需对话历史也能被理解",
		"无法从历史确定指代对象时不要猜测",
	} {
		if !strings.Contains(prompt, rule) {
			t.Fatalf("routeSystemPrompt() 缺少规则 %q", rule)
		}
	}
}

func TestRouteMessagesPreservesConversationRoles(t *testing.T) {
	longAnswer := strings.Repeat("答", 301)
	messages := routeMessages(QueryRouteInput{
		Question: "  最新问题  ",
		History: []model.Message{
			{Role: model.MessageRoleUser, Content: "上一轮问题"},
			{Role: model.MessageRoleAssistant, Content: longAnswer},
			{Role: "system", Content: "不应传入"},
		},
	}, 4)

	if len(messages) != 4 {
		t.Fatalf("routeMessages() 消息数 = %d, want 4", len(messages))
	}
	if messages[1].Role != model.MessageRoleUser || messages[1].Content != "上一轮问题" {
		t.Fatalf("routeMessages() 用户历史 = %#v", messages[1])
	}
	if messages[2].Role != model.MessageRoleAssistant || messages[2].Content != strings.Repeat("答", 300)+"…" {
		t.Fatalf("routeMessages() 助手历史 = %#v", messages[2])
	}
	if messages[3].Role != model.MessageRoleUser || messages[3].Content != "最新问题" {
		t.Fatalf("routeMessages() 最新问题 = %#v", messages[3])
	}
}

func TestBuildQueryPlan(t *testing.T) {
	tests := []struct {
		name     string
		decision QueryRouteDecision
		want     []string
	}{
		{name: "none", decision: QueryRouteDecision{QueryType: QueryTypeNone}, want: []string{}},
		{name: "direct", decision: QueryRouteDecision{QueryType: QueryTypeDirect}, want: []string{"原问题"}},
		{
			name: "rewrite protects original",
			decision: QueryRouteDecision{
				QueryType: QueryTypeContextualRewrite,
				Queries:   []string{"改写问题"},
			},
			want: []string{"原问题", "改写问题"},
		},
		{
			name: "decomposition deduplicates original",
			decision: QueryRouteDecision{
				QueryType: QueryTypeDecomposition,
				Queries:   []string{"原问题", "子问题一", "子问题二", "子问题三"},
			},
			want: []string{"原问题", "子问题一", "子问题二"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildQueryPlan("原问题", tt.decision, 2)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("buildQueryPlan() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestQueryPlannerFallsBackToDirect(t *testing.T) {
	planner := NewQueryPlanner(
		&fakeQueryRouter{err: errors.New("router unavailable")},
		4,
	)
	plan, err := planner.Plan(context.Background(), QueryRouteInput{Question: " 原问题 "})
	if err == nil {
		t.Fatal("Plan() error = nil, want router error")
	}
	want := QueryPlan{
		QueryType: QueryTypeDirect,
		Queries:   []string{"原问题"},
		Fallback:  true,
	}
	if !reflect.DeepEqual(plan, want) {
		t.Fatalf("Plan() = %#v, want %#v", plan, want)
	}
}
