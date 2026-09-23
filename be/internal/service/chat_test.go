package service

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"askbase/be/internal/llm"
	"askbase/be/internal/model"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

type fakeMessageStore struct {
	history   []model.Message
	content   string
	citations []model.Citation
}

func (s *fakeMessageStore) Create(_ context.Context, msg model.Message) (model.Message, error) {
	return msg, nil
}

func (s *fakeMessageStore) ListBySession(context.Context, int64) ([]model.Message, error) {
	return s.history, nil
}

func (s *fakeMessageStore) UpdateContent(_ context.Context, _ int64, content string, citations []model.Citation) error {
	s.content = content
	s.citations = append([]model.Citation(nil), citations...)
	return nil
}

func (s *fakeMessageStore) DeleteBySession(context.Context, int64) error {
	return nil
}

func (s *fakeMessageStore) DeleteAllByUser(context.Context, int64) error {
	return nil
}

type fakeQueryRouter struct {
	decision QueryRouteDecision
	err      error
}

func (r *fakeQueryRouter) Route(context.Context, QueryRouteInput) (QueryRouteDecision, error) {
	return r.decision, r.err
}

type fakeChatRetriever struct {
	queries [][]string
	hits    []model.RetrievalHit
}

func (r *fakeChatRetriever) SearchQueries(
	_ context.Context,
	_ int64,
	_ int64,
	queries []string,
	_ int,
	_ float64,
) ([]model.RetrievalHit, error) {
	r.queries = append(r.queries, append([]string(nil), queries...))
	return r.hits, nil
}

type fakeChatStreamer struct {
	messages []llm.ChatMessage
}

func (s *fakeChatStreamer) ChatStream(_ context.Context, messages []llm.ChatMessage, _ int) <-chan llm.StreamEvent {
	s.messages = append([]llm.ChatMessage(nil), messages...)
	events := make(chan llm.StreamEvent, 2)
	events <- llm.StreamEvent{Content: "回答"}
	events <- llm.StreamEvent{Done: true, FinishReason: "stop"}
	close(events)
	return events
}

type failingChatStreamer struct {
	err error
}

func (s *failingChatStreamer) ChatStream(context.Context, []llm.ChatMessage, int) <-chan llm.StreamEvent {
	events := make(chan llm.StreamEvent, 1)
	events <- llm.StreamEvent{Err: s.err}
	close(events)
	return events
}

func TestChatRunUsesRouteQueryPlan(t *testing.T) {
	tests := []struct {
		name     string
		decision QueryRouteDecision
		want     [][]string
	}{
		{
			name:     "none skips retrieval",
			decision: QueryRouteDecision{QueryType: QueryTypeNone},
			want:     nil,
		},
		{
			name:     "direct",
			decision: QueryRouteDecision{QueryType: QueryTypeDirect},
			want:     [][]string{{"原问题"}},
		},
		{
			name: "contextual rewrite",
			decision: QueryRouteDecision{
				QueryType: QueryTypeContextualRewrite,
				Queries:   []string{"改写问题"},
			},
			want: [][]string{{"原问题", "改写问题"}},
		},
		{
			name: "decomposition",
			decision: QueryRouteDecision{
				QueryType: QueryTypeDecomposition,
				Queries:   []string{"子问题一", "子问题二"},
			},
			want: [][]string{{"原问题", "子问题一", "子问题二"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			messages := &fakeMessageStore{}
			retrieval := &fakeChatRetriever{}
			streamer := &fakeChatStreamer{}
			service := &ChatService{
				messages:  messages,
				retrieval: retrieval,
				planner:   NewQueryPlanner(&fakeQueryRouter{decision: tt.decision}, 4),
				llm:       streamer,
			}
			events := make(chan ChatEvent, 8)
			service.run(
				context.Background(),
				model.Session{ID: 1, UserID: 2, DatasetID: 3},
				model.Message{ID: 10},
				model.Message{ID: 11},
				"原问题",
				streamer,
				events,
			)
			for range events {
			}

			if !reflect.DeepEqual(retrieval.queries, tt.want) {
				t.Fatalf("retrieval queries = %#v, want %#v", retrieval.queries, tt.want)
			}
			if tt.decision.QueryType == QueryTypeNone && !strings.Contains(streamer.messages[0].Content, "禁止使用模型自身知识") {
				t.Fatal("none route did not use restricted prompt")
			}
		})
	}
}

func TestChatRunFallsBackToDirectWhenRouterFails(t *testing.T) {
	messages := &fakeMessageStore{}
	retrieval := &fakeChatRetriever{}
	streamer := &fakeChatStreamer{}
	service := &ChatService{
		messages:  messages,
		retrieval: retrieval,
		planner:   NewQueryPlanner(&fakeQueryRouter{err: errors.New("router unavailable")}, 4),
		llm:       streamer,
	}
	events := make(chan ChatEvent, 8)
	service.run(
		context.Background(),
		model.Session{ID: 1, UserID: 2, DatasetID: 3},
		model.Message{ID: 10},
		model.Message{ID: 11},
		"原问题",
		streamer,
		events,
	)
	for range events {
	}

	want := [][]string{{"原问题"}}
	if !reflect.DeepEqual(retrieval.queries, want) {
		t.Fatalf("retrieval queries = %#v, want %#v", retrieval.queries, want)
	}
}

func TestChatRunLogsGenerationFailure(t *testing.T) {
	core, observed := observer.New(zap.ErrorLevel)
	previousLogger := zap.L()
	zap.ReplaceGlobals(zap.New(core))
	defer zap.ReplaceGlobals(previousLogger)

	streamer := &failingChatStreamer{err: errors.New("provider unavailable")}
	service := &ChatService{
		messages: &fakeMessageStore{},
		planner: NewQueryPlanner(&fakeQueryRouter{
			decision: QueryRouteDecision{QueryType: QueryTypeNone},
		}, 4),
		llm: streamer,
	}
	events := make(chan ChatEvent, 8)
	service.run(
		context.Background(),
		model.Session{ID: 1, UserID: 2, DatasetID: 3},
		model.Message{ID: 10},
		model.Message{ID: 11},
		"原问题",
		streamer,
		events,
	)

	var gotErrorEvent bool
	for event := range events {
		gotErrorEvent = gotErrorEvent || event.Type == "error"
	}
	if !gotErrorEvent {
		t.Fatal("ChatService.run() 未发送 error 事件")
	}
	entries := observed.FilterMessage("生成模型回复失败").All()
	if len(entries) != 1 {
		t.Fatalf("生成失败日志数 = %d, want 1", len(entries))
	}
	fields := entries[0].ContextMap()
	if fields["sessionId"] != int64(1) || fields["userMessageId"] != int64(10) || fields["assistantMessageId"] != int64(11) {
		t.Fatalf("生成失败日志字段 = %#v", fields)
	}
}

func TestFilterCitationsByAnswer(t *testing.T) {
	citations := []model.Citation{
		{N: 1, ChunkID: 101},
		{N: 2, ChunkID: 102},
		{N: 4, ChunkID: 104},
	}

	result := filterCitationsByAnswer("第一处依据 [1]，另一处依据 [4]，无效编号 [9]。", citations)
	if len(result) != 2 || result[0].N != 1 || result[1].N != 4 {
		t.Fatalf("filterCitationsByAnswer() = %#v", result)
	}
}
