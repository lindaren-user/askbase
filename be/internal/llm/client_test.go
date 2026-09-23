package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCompleteUsesNonStreamingChatCompletion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		var request chatRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.Stream {
			t.Fatal("complete request must not stream")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{\"basic\":[0]}"}}]}`))
	}))
	defer server.Close()

	client := New(server.URL, "test-key", "test-model", "", "", "")
	content, err := client.Complete(context.Background(), []ChatMessage{{Role: "user", Content: "extract"}}, 100)
	if err != nil {
		t.Fatal(err)
	}
	if content != `{"basic":[0]}` {
		t.Fatalf("unexpected content: %q", content)
	}
}

func TestChatStreamCompletesWithDoneMarker(t *testing.T) {
	server := newChatStreamServer(t, "data: {\"choices\":[{\"delta\":{\"content\":\"回答\"},\"finish_reason\":null}]}\n\ndata: [DONE]\n\n")
	defer server.Close()

	events := collectStreamEvents(New(server.URL, "", "test-model", "", "", "").ChatStream(
		context.Background(),
		[]ChatMessage{{Role: "user", Content: "问题"}},
		100,
	))
	if len(events) != 2 || events[0].Content != "回答" || !events[1].Done || events[1].Err != nil {
		t.Fatalf("ChatStream() events = %#v", events)
	}
}

func TestChatStreamRejectsMalformedDataFrame(t *testing.T) {
	server := newChatStreamServer(t, "data: not-json\n\n")
	defer server.Close()

	events := collectStreamEvents(New(server.URL, "", "test-model", "", "", "").ChatStream(
		context.Background(),
		[]ChatMessage{{Role: "user", Content: "问题"}},
		100,
	))
	if len(events) != 1 || events[0].Err == nil || !strings.Contains(events[0].Err.Error(), "解析流式响应数据失败") {
		t.Fatalf("ChatStream() events = %#v", events)
	}
}

func TestChatStreamRejectsPrematureEOF(t *testing.T) {
	server := newChatStreamServer(t, "data: {\"choices\":[{\"delta\":{\"content\":\"部分回答\"},\"finish_reason\":null}]}\n\n")
	defer server.Close()

	events := collectStreamEvents(New(server.URL, "", "test-model", "", "", "").ChatStream(
		context.Background(),
		[]ChatMessage{{Role: "user", Content: "问题"}},
		100,
	))
	if len(events) != 2 || events[0].Content != "部分回答" || events[1].Err == nil || !strings.Contains(events[1].Err.Error(), "未收到结束标记") {
		t.Fatalf("ChatStream() events = %#v", events)
	}
}

func newChatStreamServer(t *testing.T, response string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(response))
	}))
}

func collectStreamEvents(stream <-chan StreamEvent) []StreamEvent {
	var events []StreamEvent
	for event := range stream {
		events = append(events, event)
	}
	return events
}
