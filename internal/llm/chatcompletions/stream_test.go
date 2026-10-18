package chatcompletions

import (
	"context"
	"errors"
	"strings"
	"testing"

	"neutron-star/internal/llm"
)

// streamTestCollect 用给定适配器解析整段输入，返回收到的 chunk。
func streamTestCollect(
	t *testing.T,
	client *Client,
	input string,
) ([]*wireChunk, error) {
	t.Helper()

	var chunks []*wireChunk

	err := readStream(
		context.Background(),
		strings.NewReader(input),
		client.decodeChunk,
		func(chunk *wireChunk) bool {
			chunks = append(chunks, chunk)
			return true
		},
	)

	return chunks, err
}

// streamTestPlainClient 返回一个不启用推理字段的适配器。
func streamTestPlainClient(t *testing.T) *Client {
	t.Helper()

	client, err := optionsTestNew(t, Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	return client
}

func TestStreamParsesEventsAndIgnoresOtherFields(t *testing.T) {
	input := ": keepalive comment\n" +
		"event: message\n" +
		"id: 42\n" +
		"retry: 1000\n" +
		"data: {\"id\":\"a\",\"choices\":[{\"index\":0," +
		"\"delta\":{\"content\":\"x\"}}]}\n" +
		"\n" +
		"data: {\"id\":\"b\",\"choices\":[]}\n" +
		"\n"

	chunks, err := streamTestCollect(t, streamTestPlainClient(t), input)
	if err != nil {
		t.Fatalf("readStream: %v", err)
	}

	if len(chunks) != 2 {
		t.Fatalf("chunks: got %d, want 2", len(chunks))
	}
	if chunks[0].ID != "a" || chunks[1].ID != "b" {
		t.Fatalf("ids: %q, %q", chunks[0].ID, chunks[1].ID)
	}
	if chunks[0].Choices[0].Delta.Content == nil ||
		*chunks[0].Choices[0].Delta.Content != "x" {
		t.Fatalf("delta: %+v", chunks[0].Choices[0].Delta)
	}
}

func TestStreamJoinsMultiLineData(t *testing.T) {
	// 同一事件的多行 data 用换行连接后再解析。
	input := "data: {\"id\":\"a\",\n" +
		"data: \"choices\":[]}\n" +
		"\n"

	chunks, err := streamTestCollect(t, streamTestPlainClient(t), input)
	if err != nil {
		t.Fatalf("readStream: %v", err)
	}

	if len(chunks) != 1 {
		t.Fatalf("chunks: got %d, want 1", len(chunks))
	}
	if chunks[0].ID != "a" {
		t.Fatalf("id: %q", chunks[0].ID)
	}
}

func TestStreamStopsAtDone(t *testing.T) {
	input := "data: {\"id\":\"a\",\"choices\":[]}\n\n" +
		"data: [DONE]\n\n" +
		"data: {\"id\":\"b\",\"choices\":[]}\n\n"

	chunks, err := streamTestCollect(t, streamTestPlainClient(t), input)
	if err != nil {
		t.Fatalf("readStream: %v", err)
	}

	if len(chunks) != 1 {
		t.Fatalf("chunks: got %d, want 1", len(chunks))
	}
	if chunks[0].ID != "a" {
		t.Fatalf("id: %q", chunks[0].ID)
	}
}

func TestStreamDispatchesEventAtEOF(t *testing.T) {
	// 对端在事件结束前关闭：已聚合的载荷仍要交付。
	input := "data: {\"id\":\"a\",\"choices\":[]}\n"

	chunks, err := streamTestCollect(t, streamTestPlainClient(t), input)
	if err != nil {
		t.Fatalf("readStream: %v", err)
	}

	if len(chunks) != 1 {
		t.Fatalf("chunks: got %d, want 1", len(chunks))
	}
}

func TestStreamIgnoresKeepaliveOnlyInput(t *testing.T) {
	input := "\n" +
		": ping\n" +
		"\n" +
		"data:   \n" +
		"\n" +
		"event: message\n" +
		"\n"

	chunks, err := streamTestCollect(t, streamTestPlainClient(t), input)
	if err != nil {
		t.Fatalf("readStream: %v", err)
	}

	if len(chunks) != 0 {
		t.Fatalf("chunks: got %d, want 0", len(chunks))
	}
}

func TestStreamHandlesCarriageReturns(t *testing.T) {
	input := "data: {\"id\":\"a\",\"choices\":[]}\r\n\r\n"

	chunks, err := streamTestCollect(t, streamTestPlainClient(t), input)
	if err != nil {
		t.Fatalf("readStream: %v", err)
	}

	if len(chunks) != 1 || chunks[0].ID != "a" {
		t.Fatalf("chunks: %+v", chunks)
	}
}

func TestStreamRejectsInvalidJSON(t *testing.T) {
	input := "data: {oops}\n\n"

	_, err := streamTestCollect(t, streamTestPlainClient(t), input)

	var failure *llm.Error
	if !errors.As(err, &failure) {
		t.Fatalf("error type: %v", err)
	}
	if failure.Kind != llm.ErrorProtocol {
		t.Fatalf("kind: %q", failure.Kind)
	}
}

func TestStreamRejectsOversizedEvent(t *testing.T) {
	input := "data: " +
		strings.Repeat("x", defaultMaxSSEEventBytes+1) +
		"\n\n"

	_, err := streamTestCollect(t, streamTestPlainClient(t), input)

	var failure *llm.Error
	if !errors.As(err, &failure) {
		t.Fatalf("error type: %v", err)
	}
	if failure.Kind != llm.ErrorProtocol {
		t.Fatalf("kind: %q", failure.Kind)
	}
}

func TestStreamStopsWhenConsumerStops(t *testing.T) {
	client := streamTestPlainClient(t)

	var count int

	err := readStream(
		context.Background(),
		strings.NewReader(
			"data: {\"id\":\"a\",\"choices\":[]}\n\n"+
				"data: {\"id\":\"b\",\"choices\":[]}\n\n",
		),
		client.decodeChunk,
		func(*wireChunk) bool {
			count++
			return false
		},
	)

	if err != nil {
		t.Fatalf("readStream: %v", err)
	}
	if count != 1 {
		t.Fatalf("chunks: %d, want 1", count)
	}
}

func TestStreamStopsOnCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := readStream(
		ctx,
		strings.NewReader("data: {\"id\":\"a\",\"choices\":[]}\n\n"),
		streamTestPlainClient(t).decodeChunk,
		func(*wireChunk) bool {
			t.Error("no chunk must be consumed")
			return true
		},
	)

	var failure *llm.Error
	if !errors.As(err, &failure) {
		t.Fatalf("error type: %v", err)
	}
	if failure.Kind != llm.ErrorCanceled {
		t.Fatalf("kind: %q", failure.Kind)
	}
}

func TestStreamDefaultReasoningField(t *testing.T) {
	client, err := optionsTestNew(t, Options{
		SupportsAssistantReasoning: true,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	chunks, err := streamTestCollect(t, client,
		"data: {\"choices\":[{\"index\":0,\"delta\":"+
			"{\"reasoning_content\":\"hmm\",\"content\":\"hi\"}}]}\n\n",
	)
	if err != nil {
		t.Fatalf("readStream: %v", err)
	}

	delta := chunks[0].Choices[0].Delta
	if delta.ReasoningContent == nil || *delta.ReasoningContent != "hmm" {
		t.Fatalf("reasoning: %+v", delta.ReasoningContent)
	}
	if delta.Content == nil || *delta.Content != "hi" {
		t.Fatalf("content: %+v", delta.Content)
	}
}

func TestStreamCustomReasoningField(t *testing.T) {
	client, err := optionsTestNew(t, Options{
		SupportsAssistantReasoning: true,
		AssistantReasoningField:    "thinking",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	payload := "data: {\"choices\":[{\"index\":0,\"delta\":" +
		"{\"thinking\":\"hmm\",\"content\":\"hi\"}}]}\n\n"

	chunks, err := streamTestCollect(t, client, payload)
	if err != nil {
		t.Fatalf("readStream: %v", err)
	}

	delta := chunks[0].Choices[0].Delta
	if delta.ReasoningContent == nil || *delta.ReasoningContent != "hmm" {
		t.Fatalf("reasoning: %+v", delta.ReasoningContent)
	}

	// 默认字段名在自定义配置下不再被识别。
	plain := streamTestPlainClient(t)

	chunks, err = streamTestCollect(t, plain, payload)
	if err != nil {
		t.Fatalf("readStream: %v", err)
	}
	if chunks[0].Choices[0].Delta.ReasoningContent != nil {
		t.Fatalf(
			"reasoning: %+v",
			chunks[0].Choices[0].Delta.ReasoningContent,
		)
	}
}

func TestStreamRejectsNonStringReasoningField(t *testing.T) {
	client, err := optionsTestNew(t, Options{
		SupportsAssistantReasoning: true,
		AssistantReasoningField:    "thinking",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = streamTestCollect(t, client,
		"data: {\"choices\":[{\"index\":0,"+
			"\"delta\":{\"thinking\":123}}]}\n\n",
	)

	var failure *llm.Error
	if !errors.As(err, &failure) {
		t.Fatalf("error type: %v", err)
	}
	if failure.Kind != llm.ErrorProtocol {
		t.Fatalf("kind: %q", failure.Kind)
	}
}
