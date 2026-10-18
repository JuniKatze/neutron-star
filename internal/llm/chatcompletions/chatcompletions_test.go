package chatcompletions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"neutron-star/internal/llm"
)

// chatTestUserRequest 返回一条最小的合法请求。
func chatTestUserRequest() *llm.Request {
	return &llm.Request{
		Model: "test-model",
		Messages: []llm.Message{{
			Role:  llm.RoleUser,
			Parts: []llm.Part{{Kind: llm.PartText, Text: "hi"}},
		}},
	}
}

// chatTestClient 构造一个指向 srv 的适配器。
func chatTestClient(
	t *testing.T,
	srv *httptest.Server,
	options Options,
) *Client {
	t.Helper()

	options.Endpoint = srv.URL + "/v1/chat/completions"

	client, err := New(options)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	return client
}

// chatTestDoer 记录请求，并把响应体包装成可计数的关闭。
type chatTestDoer struct {
	inner  HTTPDoer
	calls  atomic.Int32
	closes atomic.Int32
}

func (d *chatTestDoer) Do(request *http.Request) (*http.Response, error) {
	d.calls.Add(1)

	response, err := d.inner.Do(request)
	if err != nil {
		return nil, err
	}

	response.Body = &chatTestBody{inner: response.Body, closes: &d.closes}

	return response, nil
}

// chatTestBody 统计响应体被关闭的次数。
type chatTestBody struct {
	inner  io.ReadCloser
	closes *atomic.Int32
}

func (b *chatTestBody) Read(buffer []byte) (int, error) {
	return b.inner.Read(buffer)
}

func (b *chatTestBody) Close() error {
	b.closes.Add(1)
	return b.inner.Close()
}

// chatTestSSE 把若干载荷拼成 SSE 响应体。
func chatTestSSE(events ...string) string {
	var builder strings.Builder

	for _, event := range events {
		builder.WriteString("data: ")
		builder.WriteString(event)
		builder.WriteString("\n\n")
	}

	return builder.String()
}

// chatTestDelta 返回一项流式增量的可读描述，便于比较事件序列。
func chatTestDelta(delta *llm.PartDelta) string {
	if delta.Kind == llm.PartToolCall {
		return strings.Join([]string{
			string(delta.Kind),
			delta.ToolCall.ID,
			delta.ToolCall.Name,
			delta.ToolCall.ArgumentsDelta,
		}, "|")
	}

	return string(delta.Kind) + "|" + delta.Text
}

func TestChatGenerateStreamsAndAssembles(t *testing.T) {
	var (
		doer     chatTestDoer
		received []byte
	)

	srv := httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, request *http.Request) {
			received, _ = io.ReadAll(request.Body)

			writer.Header().Set("Content-Type", "text/event-stream")
			writer.Header().Set("x-request-id", "req-1")
			writer.WriteHeader(http.StatusOK)

			_, _ = io.WriteString(writer, chatTestSSE(
				`{"id":"chatcmpl-1","model":"test-model",`+
					`"choices":[{"index":0,"delta":{"role":"assistant",`+
					`"reasoning_content":"think"},"finish_reason":null}]}`,
				`{"id":"chatcmpl-1","model":"test-model",`+
					`"choices":[{"index":0,"delta":{"content":"Hello"},`+
					`"finish_reason":null}]}`,
				`{"id":"chatcmpl-1","model":"test-model",`+
					`"choices":[{"index":0,"delta":{"content":" world"},`+
					`"finish_reason":null}]}`,
				`{"id":"chatcmpl-1","model":"test-model",`+
					`"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,`+
					`"id":"call_a","type":"function","function":`+
					`{"name":"weather","arguments":"{\"ci"}}]},`+
					`"finish_reason":null}]}`,
				`{"id":"chatcmpl-1","model":"test-model",`+
					`"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,`+
					`"function":{"arguments":"ty\":\"Paris\"}"}}]},`+
					`"finish_reason":null}]}`,
				`{"id":"chatcmpl-1","model":"test-model",`+
					`"choices":[{"index":0,"delta":{"tool_calls":[{"index":1,`+
					`"id":"call_b","type":"function","function":`+
					`{"name":"clock","arguments":"{}"}}]},`+
					`"finish_reason":null}]}`,
				`{"id":"chatcmpl-1","model":"test-model",`+
					`"choices":[{"index":0,"delta":{},`+
					`"finish_reason":"tool_calls"}]}`,
				`{"id":"chatcmpl-1","model":"test-model","choices":[],`+
					`"usage":{"prompt_tokens":10,"completion_tokens":5,`+
					`"total_tokens":15}}`,
				"[DONE]",
			))
		},
	))
	defer srv.Close()

	doer.inner = srv.Client()

	client := chatTestClient(t, srv, Options{
		HTTPClient:                 &doer,
		IncludeUsage:               true,
		SupportsAssistantReasoning: true,
	})

	stream := client.Generate(context.Background(), chatTestUserRequest())

	var deltas []string

	final, err := llm.Consume(stream, func(delta *llm.PartDelta) error {
		deltas = append(deltas, chatTestDelta(delta))
		return nil
	})

	if err != nil {
		t.Fatalf("Consume: %v", err)
	}

	want := []string{
		"reasoning|think",
		"text|Hello",
		"text| world",
		"tool_call|call_a|weather|{\"ci",
		"tool_call|||ty\":\"Paris\"}",
		"tool_call|call_b|clock|{}",
	}
	if strings.Join(deltas, "\n") != strings.Join(want, "\n") {
		t.Fatalf("deltas:\ngot  %v\nwant %v", deltas, want)
	}

	if final.StopReason != llm.StopToolUse {
		t.Fatalf("stop reason: got %q", final.StopReason)
	}
	if final.ProviderStopReason != "tool_calls" {
		t.Fatalf(
			"provider stop reason: got %q",
			final.ProviderStopReason,
		)
	}
	if final.Meta.ResponseID != "chatcmpl-1" ||
		final.Meta.RequestID != "req-1" ||
		final.Meta.Model != "test-model" {
		t.Fatalf("meta: %+v", final.Meta)
	}
	if final.Usage == nil ||
		final.Usage.InputTokens == nil ||
		*final.Usage.InputTokens != 10 ||
		final.Usage.OutputTokens == nil ||
		*final.Usage.OutputTokens != 5 ||
		final.Usage.TotalTokens == nil ||
		*final.Usage.TotalTokens != 15 {
		t.Fatalf("usage: %+v", final.Usage)
	}

	parts := final.Message.Parts
	if len(parts) != 4 {
		t.Fatalf("parts: got %d, want 4", len(parts))
	}
	if parts[0].Kind != llm.PartReasoning || parts[0].Text != "think" {
		t.Fatalf("parts[0]: %+v", parts[0])
	}
	if parts[1].Kind != llm.PartText || parts[1].Text != "Hello world" {
		t.Fatalf("parts[1]: %+v", parts[1])
	}
	if parts[2].ToolCall.ID != "call_a" ||
		parts[2].ToolCall.Name != "weather" ||
		parts[2].ToolCall.Arguments != `{"city":"Paris"}` {
		t.Fatalf("parts[2]: %+v", parts[2].ToolCall)
	}
	if parts[3].ToolCall.ID != "call_b" ||
		parts[3].ToolCall.Name != "clock" ||
		parts[3].ToolCall.Arguments != "{}" {
		t.Fatalf("parts[3]: %+v", parts[3].ToolCall)
	}

	if err := final.ValidateFor(chatTestUserRequest()); err != nil {
		t.Fatalf("ValidateFor: %v", err)
	}

	var body map[string]json.RawMessage
	if err := json.Unmarshal(received, &body); err != nil {
		t.Fatalf("request body: %v", err)
	}
	if string(body["stream"]) != "true" {
		t.Fatalf("stream: %s", body["stream"])
	}
	if string(body["stream_options"]) !=
		`{"include_usage":true}` {
		t.Fatalf("stream_options: %s", body["stream_options"])
	}
	if _, exists := body["max_tokens"]; exists {
		t.Fatalf("unexpected max_tokens: %s", body["max_tokens"])
	}

	if closes := doer.closes.Load(); closes != 1 {
		t.Fatalf("response body closed %d times, want 1", closes)
	}
}

func TestChatGenerateUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, request *http.Request) {
			writer.Header().Set("x-request-id", "req-401")
			writer.WriteHeader(http.StatusUnauthorized)

			_, _ = io.WriteString(writer, `{"error":{`+
				`"message":"bad key","type":"invalid_request_error",`+
				`"code":"invalid_api_key"}}`)
		},
	))
	defer srv.Close()

	client := chatTestClient(t, srv, Options{})

	_, err := llm.Consume(
		client.Generate(context.Background(), chatTestUserRequest()),
		nil,
	)

	var failure *llm.Error
	if !errors.As(err, &failure) {
		t.Fatalf("error type: %v", err)
	}
	if failure.Kind != llm.ErrorAuthentication {
		t.Fatalf("kind: %q", failure.Kind)
	}
	if failure.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status code: %d", failure.StatusCode)
	}
	if failure.ProviderCode != "invalid_api_key" {
		t.Fatalf("provider code: %q", failure.ProviderCode)
	}
	if failure.RequestID != "req-401" {
		t.Fatalf("request id: %q", failure.RequestID)
	}
	if failure.Message != "bad key" {
		t.Fatalf("message: %q", failure.Message)
	}
}

func TestChatGenerateRateLimitedWithRetryAfter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, request *http.Request) {
			writer.Header().Set("Retry-After", "3")
			writer.WriteHeader(http.StatusTooManyRequests)
		},
	))
	defer srv.Close()

	client := chatTestClient(t, srv, Options{})

	_, err := llm.Consume(
		client.Generate(context.Background(), chatTestUserRequest()),
		nil,
	)

	var failure *llm.Error
	if !errors.As(err, &failure) {
		t.Fatalf("error type: %v", err)
	}
	if failure.Kind != llm.ErrorRateLimited {
		t.Fatalf("kind: %q", failure.Kind)
	}
	if failure.RetryAfterSeconds != 3 {
		t.Fatalf("retry after: %d", failure.RetryAfterSeconds)
	}
}

func TestChatGenerateTruncatedStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, request *http.Request) {
			writer.WriteHeader(http.StatusOK)

			// 有内容增量，但没有 finish_reason 就断开。
			_, _ = io.WriteString(writer, chatTestSSE(
				`{"id":"chatcmpl-1","choices":[{"index":0,`+
					`"delta":{"content":"Hello"}}]}`,
			))
		},
	))
	defer srv.Close()

	client := chatTestClient(t, srv, Options{})

	var deltas int

	final, err := llm.Consume(
		client.Generate(context.Background(), chatTestUserRequest()),
		func(*llm.PartDelta) error {
			deltas++
			return nil
		},
	)

	if err == nil {
		t.Fatal("expected an error")
	}

	var failure *llm.Error
	if !errors.As(err, &failure) {
		t.Fatalf("error type: %v", err)
	}
	if failure.Kind != llm.ErrorProtocol {
		t.Fatalf("kind: %q", failure.Kind)
	}
	if deltas != 1 {
		t.Fatalf("deltas: %d, want 1", deltas)
	}
	if final != nil {
		t.Fatal("final must not be delivered")
	}
}

func TestChatGenerateInvalidRequestSendsNothing(t *testing.T) {
	var doer chatTestDoer

	srv := httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, request *http.Request) {
			t.Error("server must not be reached")
		},
	))
	defer srv.Close()

	doer.inner = srv.Client()

	client := chatTestClient(t, srv, Options{HTTPClient: &doer})

	request := chatTestUserRequest()
	request.Model = ""

	_, err := llm.Consume(
		client.Generate(context.Background(), request),
		nil,
	)

	var failure *llm.Error
	if !errors.As(err, &failure) {
		t.Fatalf("error type: %v", err)
	}
	if failure.Kind != llm.ErrorInvalidRequest {
		t.Fatalf("kind: %q", failure.Kind)
	}
	if calls := doer.calls.Load(); calls != 0 {
		t.Fatalf("http calls: %d, want 0", calls)
	}
}

func TestChatGenerateNilRequest(t *testing.T) {
	client, err := New(Options{
		Endpoint: "https://example.invalid/v1/chat/completions",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = llm.Consume(
		client.Generate(context.Background(), nil),
		nil,
	)

	var failure *llm.Error
	if !errors.As(err, &failure) {
		t.Fatalf("error type: %v", err)
	}
	if failure.Kind != llm.ErrorInvalidRequest {
		t.Fatalf("kind: %q", failure.Kind)
	}
}

func TestChatGenerateContextCanceled(t *testing.T) {
	release := make(chan struct{})
	defer close(release)

	srv := httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, request *http.Request) {
			writer.WriteHeader(http.StatusOK)
			writer.(http.Flusher).Flush()

			<-release
		},
	))
	defer srv.Close()

	client := chatTestClient(t, srv, Options{})

	ctx, cancel := context.WithCancel(context.Background())

	// 第一个增量到达之前就取消，模拟调用方中途放弃。
	cancel()

	_, err := llm.Consume(client.Generate(ctx, chatTestUserRequest()), nil)

	var failure *llm.Error
	if !errors.As(err, &failure) {
		t.Fatalf("error type: %v", err)
	}
	if failure.Kind != llm.ErrorCanceled {
		t.Fatalf("kind: %q", failure.Kind)
	}
}

func TestChatGenerateMultipleChoicesUnsupported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, request *http.Request) {
			writer.WriteHeader(http.StatusOK)

			_, _ = io.WriteString(writer, chatTestSSE(
				`{"id":"chatcmpl-1","choices":[`+
					`{"index":0,"delta":{"content":"a"},`+
					`"finish_reason":"stop"},`+
					`{"index":1,"delta":{"content":"b"},`+
					`"finish_reason":"stop"}]}`,
			))
		},
	))
	defer srv.Close()

	client := chatTestClient(t, srv, Options{})

	_, err := llm.Consume(
		client.Generate(context.Background(), chatTestUserRequest()),
		nil,
	)

	var failure *llm.Error
	if !errors.As(err, &failure) {
		t.Fatalf("error type: %v", err)
	}
	if failure.Kind != llm.ErrorUnsupported {
		t.Fatalf("kind: %q", failure.Kind)
	}
}

func TestChatGenerateConcurrentCalls(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, request *http.Request) {
			var body struct {
				Model string `json:"model"`
			}
			_ = json.NewDecoder(request.Body).Decode(&body)

			writer.WriteHeader(http.StatusOK)

			// 回送请求里的模型名，用于确认每次调用拿到的是自己的响应。
			_, _ = io.WriteString(writer, chatTestSSE(
				`{"id":"chatcmpl-1","model":"`+body.Model+`",`+
					`"choices":[{"index":0,"delta":{"content":"ok"},`+
					`"finish_reason":"stop"}]}`,
				"[DONE]",
			))
		},
	))
	defer srv.Close()

	client := chatTestClient(t, srv, Options{})

	var group sync.WaitGroup

	for index := range 8 {
		group.Add(1)

		go func() {
			defer group.Done()

			request := chatTestUserRequest()
			request.Model = fmt.Sprintf("model-%d", index)

			final, err := llm.Consume(
				client.Generate(context.Background(), request),
				nil,
			)
			if err != nil {
				t.Errorf("Consume: %v", err)
				return
			}
			if final.Meta.Model != request.Model {
				t.Errorf(
					"model: got %q, want %q",
					final.Meta.Model,
					request.Model,
				)
			}
		}()
	}

	group.Wait()
}
