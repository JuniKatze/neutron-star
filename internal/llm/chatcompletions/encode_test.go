package chatcompletions

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"neutron-star/internal/llm"
)

// encodeTestContentPart 是内容数组的一项，字段按 wire 命名。
type encodeTestContentPart struct {
	Type     string `json:"type"`
	Text     string `json:"text"`
	ImageURL *struct {
		URL string `json:"url"`
	} `json:"image_url"`
	InputAudio *struct {
		Data   string `json:"data"`
		Format string `json:"format"`
	} `json:"input_audio"`
}

// encodeTestClient 用给定选项构造适配器。
func encodeTestClient(t *testing.T, options Options) *Client {
	t.Helper()

	client, err := optionsTestNew(t, options)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	return client
}

// encodeTestBody 投影请求体并解码为字段表。
func encodeTestBody(
	t *testing.T,
	options Options,
	request *llm.Request,
) map[string]json.RawMessage {
	t.Helper()

	body, err := encodeTestClient(t, options).buildBody(request)
	if err != nil {
		t.Fatalf("buildBody: %v", err)
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		t.Fatalf("decode body: %v (%s)", err, body)
	}

	return fields
}

// encodeTestMessages 取出请求体的 messages 数组。
func encodeTestMessages(
	t *testing.T,
	fields map[string]json.RawMessage,
) []map[string]json.RawMessage {
	t.Helper()

	var messages []map[string]json.RawMessage
	if err := json.Unmarshal(fields["messages"], &messages); err != nil {
		t.Fatalf("decode messages: %v", err)
	}

	return messages
}

// encodeTestContent 返回一条消息的 content：字符串或内容项数组。
func encodeTestContent(
	t *testing.T,
	message map[string]json.RawMessage,
) (string, []encodeTestContentPart) {
	t.Helper()

	raw, exists := message["content"]
	if !exists {
		return "", nil
	}

	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text, nil
	}

	var items []encodeTestContentPart
	if err := json.Unmarshal(raw, &items); err != nil {
		t.Fatalf("decode content: %v (%s)", err, raw)
	}

	return "", items
}

// encodeTestTextRequest 返回一个只有一条用户文本消息的请求。
func encodeTestTextRequest(text string) *llm.Request {
	return &llm.Request{
		Model: "test-model",
		Messages: []llm.Message{{
			Role:  llm.RoleUser,
			Parts: []llm.Part{{Kind: llm.PartText, Text: text}},
		}},
	}
}

// encodeTestImagePart 返回一个图片来源指定的图片内容块。
func encodeTestImagePart(source llm.Source, mimeType string) llm.Part {
	return llm.Part{
		Kind:  llm.PartImage,
		Image: &llm.Image{Source: source, MIMEType: mimeType},
	}
}

// encodeTestAudioPart 返回一个音频来源指定的音频内容块。
func encodeTestAudioPart(source llm.Source, mimeType string) llm.Part {
	return llm.Part{
		Kind:  llm.PartAudio,
		Audio: &llm.Audio{Source: source, MIMEType: mimeType},
	}
}

func encodeTestFloat(value float64) *float64 { return &value }
func encodeTestInt(value int) *int           { return &value }
func encodeTestBool(value bool) *bool        { return &value }

func TestEncodeAlwaysStreams(t *testing.T) {
	fields := encodeTestBody(
		t,
		Options{},
		encodeTestTextRequest("hi"),
	)

	if string(fields["stream"]) != "true" {
		t.Fatalf("stream: %s", fields["stream"])
	}
	if string(fields["model"]) != `"test-model"` {
		t.Fatalf("model: %s", fields["model"])
	}
	if _, exists := fields["stream_options"]; exists {
		t.Fatalf("stream_options: %s", fields["stream_options"])
	}
}

func TestEncodeIncludeUsage(t *testing.T) {
	fields := encodeTestBody(
		t,
		Options{IncludeUsage: true},
		encodeTestTextRequest("hi"),
	)

	if string(fields["stream_options"]) != `{"include_usage":true}` {
		t.Fatalf("stream_options: %s", fields["stream_options"])
	}
}

func TestEncodeSingleTextBecomesString(t *testing.T) {
	fields := encodeTestBody(
		t,
		Options{},
		encodeTestTextRequest("hi"),
	)

	messages := encodeTestMessages(t, fields)
	if len(messages) != 1 {
		t.Fatalf("messages: %d", len(messages))
	}

	text, items := encodeTestContent(t, messages[0])
	if text != "hi" || items != nil {
		t.Fatalf("content: %q %+v", text, items)
	}
	if string(messages[0]["role"]) != `"user"` {
		t.Fatalf("role: %s", messages[0]["role"])
	}
}

func TestEncodeMultiplePartsBecomeArray(t *testing.T) {
	request := &llm.Request{
		Model: "test-model",
		Messages: []llm.Message{{
			Role: llm.RoleUser,
			Parts: []llm.Part{
				{Kind: llm.PartText, Text: "look"},
				{Kind: llm.PartJSON, JSON: json.RawMessage(`{"a":1}`)},
			},
		}},
	}

	fields := encodeTestBody(t, Options{}, request)
	messages := encodeTestMessages(t, fields)

	_, items := encodeTestContent(t, messages[0])
	if len(items) != 2 {
		t.Fatalf("items: %+v", items)
	}
	if items[0].Type != "text" || items[0].Text != "look" {
		t.Fatalf("items[0]: %+v", items[0])
	}
	// JSON 内容块以文本形式发送：wire 只有文本项。
	if items[1].Type != "text" || items[1].Text != `{"a":1}` {
		t.Fatalf("items[1]: %+v", items[1])
	}
}

func TestEncodeSystemMessageJoinsText(t *testing.T) {
	request := &llm.Request{
		Model: "test-model",
		Messages: []llm.Message{{
			Role: llm.RoleSystem,
			Parts: []llm.Part{
				{Kind: llm.PartText, Text: "be "},
				{Kind: llm.PartText, Text: "brief"},
			},
		}},
	}

	fields := encodeTestBody(t, Options{}, request)
	messages := encodeTestMessages(t, fields)

	text, _ := encodeTestContent(t, messages[0])
	if text != "be brief" {
		t.Fatalf("content: %q", text)
	}
}

func TestEncodeSystemMessageRejectsOtherContent(t *testing.T) {
	// IR 保证系统消息只含文本；这里模拟调用方绕过校验的情况。
	request := &llm.Request{
		Model: "test-model",
		Messages: []llm.Message{{
			Role: llm.RoleSystem,
			Parts: []llm.Part{encodeTestAudioPart(
				llm.Source{Type: llm.SourceData, Data: []byte("abc")},
				"audio/wav",
			)},
		}},
	}

	_, err := encodeTestClient(t, Options{}).buildBody(request)

	var failure *llm.Error
	if !errors.As(err, &failure) {
		t.Fatalf("error type: %v", err)
	}
	if failure.Kind != llm.ErrorInvalidRequest {
		t.Fatalf("kind: %q", failure.Kind)
	}
}

func TestEncodeImageSources(t *testing.T) {
	cases := []struct {
		name   string
		source llm.Source
		mime   string
		want   string
	}{
		{
			"inline bytes",
			llm.Source{Type: llm.SourceData, Data: []byte("abc")},
			"image/png",
			"data:image/png;base64,YWJj",
		},
		{
			"absolute url",
			llm.Source{
				Type: llm.SourceURL,
				URL:  "https://example.com/a.png",
			},
			"image/png",
			"https://example.com/a.png",
		},
	}

	for _, testCase := range cases {
		request := &llm.Request{
			Model: "test-model",
			Messages: []llm.Message{{
				Role: llm.RoleUser,
				Parts: []llm.Part{
					{Kind: llm.PartText, Text: "look"},
					encodeTestImagePart(testCase.source, testCase.mime),
				},
			}},
		}

		fields := encodeTestBody(t, Options{}, request)
		messages := encodeTestMessages(t, fields)

		_, items := encodeTestContent(t, messages[0])
		if len(items) != 2 {
			t.Fatalf("%s: items: %+v", testCase.name, items)
		}
		if items[1].Type != "image_url" ||
			items[1].ImageURL == nil ||
			items[1].ImageURL.URL != testCase.want {
			t.Fatalf("%s: item: %+v", testCase.name, items[1])
		}
	}
}

func TestEncodeAudioSources(t *testing.T) {
	cases := []struct {
		name   string
		mime   string
		want   string
		format string
	}{
		{"wav", "audio/wav", "YWJj", "wav"},
		{"wave alias", "audio/x-wav", "YWJj", "wav"},
		{"mp3", "audio/mpeg", "YWJj", "mp3"},
	}

	for _, testCase := range cases {
		request := &llm.Request{
			Model: "test-model",
			Messages: []llm.Message{{
				Role: llm.RoleUser,
				Parts: []llm.Part{
					{Kind: llm.PartText, Text: "listen"},
					encodeTestAudioPart(
						llm.Source{
							Type: llm.SourceData,
							Data: []byte("abc"),
						},
						testCase.mime,
					),
				},
			}},
		}

		fields := encodeTestBody(t, Options{}, request)
		messages := encodeTestMessages(t, fields)

		_, items := encodeTestContent(t, messages[0])
		if len(items) != 2 {
			t.Fatalf("%s: items: %+v", testCase.name, items)
		}

		audio := items[1].InputAudio
		if items[1].Type != "input_audio" || audio == nil ||
			audio.Data != testCase.want ||
			audio.Format != testCase.format {
			t.Fatalf("%s: item: %+v", testCase.name, items[1])
		}
	}
}

// encodeTestDroppedRequest 返回一条"文本 + 无法表达的内容"的用户消息。
func encodeTestDroppedRequest(part llm.Part) *llm.Request {
	return &llm.Request{
		Model: "test-model",
		Messages: []llm.Message{{
			Role:  llm.RoleUser,
			Parts: []llm.Part{{Kind: llm.PartText, Text: "keep"}, part},
		}},
	}
}

func TestEncodeDropsUnrepresentableContent(t *testing.T) {
	emptyData := llm.Source{Type: llm.SourceData, Data: []byte("abc")}

	cases := []struct {
		name string
		part llm.Part
	}{
		{
			"asset image",
			encodeTestImagePart(
				llm.Source{Type: llm.SourceAsset, AssetID: "asset-1"},
				"image/png",
			),
		},
		{
			"provider image",
			encodeTestImagePart(llm.Source{
				Type: llm.SourceProvider,
				Provider: &llm.ProviderFile{
					Adapter:    AdapterID,
					Connection: "main",
					ID:         "file-1",
				},
			}, "image/png"),
		},
		{
			"remote audio",
			encodeTestAudioPart(llm.Source{
				Type: llm.SourceURL,
				URL:  "https://example.com/a.wav",
			}, "audio/wav"),
		},
		{
			"unknown audio container",
			encodeTestAudioPart(emptyData, "audio/flac"),
		},
		{
			"video",
			llm.Part{Kind: llm.PartVideo, Video: &llm.Video{
				Source:   emptyData,
				MIMEType: "video/mp4",
			}},
		},
		{
			"document",
			llm.Part{Kind: llm.PartDocument, Document: &llm.Document{
				Source:   emptyData,
				MIMEType: "application/pdf",
			}},
		},
		{
			"file",
			llm.Part{Kind: llm.PartFile, File: &llm.File{
				Source:   emptyData,
				MIMEType: "application/octet-stream",
			}},
		},
		{
			"opaque",
			llm.Part{Kind: llm.PartOpaque},
		},
	}

	for _, testCase := range cases {
		request := encodeTestDroppedRequest(testCase.part)

		fields := encodeTestBody(t, Options{}, request)
		messages := encodeTestMessages(t, fields)

		_, items := encodeTestContent(t, messages[0])
		if len(items) != 1 || items[0].Text != "keep" {
			t.Fatalf("%s: items: %+v", testCase.name, items)
		}
	}
}

func TestEncodeDroppedContentIsLogged(t *testing.T) {
	var buffer bytes.Buffer

	request := encodeTestDroppedRequest(encodeTestImagePart(
		llm.Source{Type: llm.SourceAsset, AssetID: "asset-1"},
		"image/png",
	))

	encodeTestBody(t, Options{
		Logger: slog.New(slog.NewTextHandler(&buffer, nil)),
	}, request)

	if !strings.Contains(buffer.String(), "content dropped") {
		t.Fatalf("log: %q", buffer.String())
	}
	if !strings.Contains(buffer.String(), "messages[0].parts[1]") {
		t.Fatalf("log: %q", buffer.String())
	}
}

func TestEncodeKeepsMessageWhenEverythingIsDropped(t *testing.T) {
	// 内容被丢光也要保留这条消息：删掉整条消息会改变对话结构。
	request := &llm.Request{
		Model: "test-model",
		Messages: []llm.Message{{
			Role: llm.RoleUser,
			Parts: []llm.Part{encodeTestImagePart(
				llm.Source{Type: llm.SourceAsset, AssetID: "asset-1"},
				"image/png",
			)},
		}},
	}

	fields := encodeTestBody(t, Options{}, request)
	messages := encodeTestMessages(t, fields)

	if len(messages) != 1 {
		t.Fatalf("messages: %d", len(messages))
	}

	text, items := encodeTestContent(t, messages[0])
	if text != "" || items != nil {
		t.Fatalf("content: %q %+v", text, items)
	}
}

func TestEncodeAssistantMessage(t *testing.T) {
	request := &llm.Request{
		Model: "test-model",
		Messages: []llm.Message{{
			Role: llm.RoleAssistant,
			Parts: []llm.Part{
				{Kind: llm.PartText, Text: "call"},
				{Kind: llm.PartText, Text: "ing"},
				{Kind: llm.PartToolCall, ToolCall: &llm.ToolCall{
					ID:        "call_a",
					Name:      "weather",
					Arguments: `{"city":"Paris"}`,
				}},
			},
		}},
	}

	fields := encodeTestBody(t, Options{}, request)
	messages := encodeTestMessages(t, fields)

	text, _ := encodeTestContent(t, messages[0])
	if text != "calling" {
		t.Fatalf("content: %q", text)
	}

	var calls []wireToolCall
	if err := json.Unmarshal(messages[0]["tool_calls"], &calls); err != nil {
		t.Fatalf("decode tool calls: %v", err)
	}
	if len(calls) != 1 || calls[0].Type != "function" ||
		calls[0].ID != "call_a" ||
		calls[0].Function.Name != "weather" ||
		calls[0].Function.Arguments != `{"city":"Paris"}` {
		t.Fatalf("tool calls: %+v", calls)
	}

	if _, exists := messages[0]["reasoning_content"]; exists {
		t.Fatal("reasoning field must not be sent by default")
	}
}

func TestEncodeAssistantWithoutTextOmitsContent(t *testing.T) {
	// 只有工具调用的历史消息省略 content，避免被解读为空回答。
	request := &llm.Request{
		Model: "test-model",
		Messages: []llm.Message{{
			Role: llm.RoleAssistant,
			Parts: []llm.Part{
				{Kind: llm.PartToolCall, ToolCall: &llm.ToolCall{
					ID:        "call_a",
					Name:      "weather",
					Arguments: "{}",
				}},
			},
		}},
	}

	fields := encodeTestBody(t, Options{}, request)
	messages := encodeTestMessages(t, fields)

	if _, exists := messages[0]["content"]; exists {
		t.Fatalf("content: %s", messages[0]["content"])
	}
}

func TestEncodeAssistantReasoning(t *testing.T) {
	request := &llm.Request{
		Model: "test-model",
		Messages: []llm.Message{{
			Role: llm.RoleAssistant,
			Parts: []llm.Part{
				{Kind: llm.PartReasoning, Text: "think"},
				{Kind: llm.PartText, Text: "answer"},
			},
		}},
	}

	// 未声明支持：历史推理内容被丢弃，字段不出现。
	fields := encodeTestBody(t, Options{}, request)
	messages := encodeTestMessages(t, fields)

	if _, exists := messages[0]["reasoning_content"]; exists {
		t.Fatal("reasoning field must not be sent")
	}

	// 声明支持：字段名可自定义。
	fields = encodeTestBody(t, Options{
		SupportsAssistantReasoning: true,
		AssistantReasoningField:    "thinking",
	}, request)
	messages = encodeTestMessages(t, fields)

	if string(messages[0]["thinking"]) != `"think"` {
		t.Fatalf("reasoning: %s", messages[0]["thinking"])
	}
}

func TestEncodeAssistantReasoningIsAlwaysSentWhenSupported(t *testing.T) {
	// 声明支持时，历史消息没有推理内容也要发空字符串：
	// 省略字段与"这段推理为空"对部分端点不是一回事。
	request := &llm.Request{
		Model: "test-model",
		Messages: []llm.Message{{
			Role:  llm.RoleAssistant,
			Parts: []llm.Part{{Kind: llm.PartText, Text: "answer"}},
		}},
	}

	fields := encodeTestBody(t, Options{
		SupportsAssistantReasoning: true,
	}, request)

	messages := encodeTestMessages(t, fields)
	if string(messages[0]["reasoning_content"]) != `""` {
		t.Fatalf("reasoning: %s", messages[0]["reasoning_content"])
	}
}

func TestEncodeToolResultIsExpanded(t *testing.T) {
	request := &llm.Request{
		Model: "test-model",
		Messages: []llm.Message{{
			Role: llm.RoleAssistant,
			Parts: []llm.Part{
				{
					Kind:     llm.PartText,
					Text:     "x",
					ResultOf: &llm.ToolResultRef{CallID: "call_a"},
				},
				{
					Kind:     llm.PartText,
					Text:     "y",
					ResultOf: &llm.ToolResultRef{CallID: "call_b"},
				},
				{
					Kind:     llm.PartJSON,
					JSON:     json.RawMessage(`{"z":1}`),
					ResultOf: &llm.ToolResultRef{CallID: "call_a"},
				},
			},
		}},
	}

	fields := encodeTestBody(t, Options{}, request)
	messages := encodeTestMessages(t, fields)

	if len(messages) != 2 {
		t.Fatalf("messages: %d", len(messages))
	}

	// 顺序按调用首次出现确定；同一调用的结果合并成一条消息。
	for index, want := range []struct{ callID, content string }{
		{"call_a", `x{"z":1}`},
		{"call_b", "y"},
	} {
		text, _ := encodeTestContent(t, messages[index])
		if string(messages[index]["role"]) != `"tool"` ||
			string(messages[index]["tool_call_id"]) != `"`+want.callID+`"` ||
			text != want.content {
			t.Fatalf("message %d: %+v (%q)", index, messages[index], text)
		}
	}
}

func TestEncodeToolResultWithMediaKeepsArray(t *testing.T) {
	request := &llm.Request{
		Model: "test-model",
		Messages: []llm.Message{{
			Role: llm.RoleAssistant,
			Parts: []llm.Part{
				{
					Kind:     llm.PartText,
					Text:     "shot:",
					ResultOf: &llm.ToolResultRef{CallID: "call_a"},
				},
				{
					Kind: llm.PartImage,
					Image: &llm.Image{
						Source: llm.Source{
							Type: llm.SourceData,
							Data: []byte("abc"),
						},
						MIMEType: "image/png",
					},
					ResultOf: &llm.ToolResultRef{CallID: "call_a"},
				},
			},
		}},
	}

	fields := encodeTestBody(t, Options{}, request)
	messages := encodeTestMessages(t, fields)

	_, items := encodeTestContent(t, messages[0])
	if len(items) != 2 {
		t.Fatalf("items: %+v", items)
	}
	if items[0].Type != "text" || items[0].Text != "shot:" {
		t.Fatalf("items[0]: %+v", items[0])
	}
	if items[1].ImageURL == nil ||
		items[1].ImageURL.URL != "data:image/png;base64,YWJj" {
		t.Fatalf("items[1]: %+v", items[1])
	}
}

func TestEncodeToolResultVariants(t *testing.T) {
	cases := []struct {
		name    string
		part    llm.Part
		content string
	}{
		{
			"error",
			llm.Part{
				Kind:  llm.PartError,
				Error: &llm.ToolError{Code: "E1", Message: "boom"},
			},
			"E1: boom",
		},
		{
			"error without code",
			llm.Part{
				Kind:  llm.PartError,
				Error: &llm.ToolError{Message: "boom"},
			},
			"boom",
		},
		{
			"empty",
			llm.Part{Kind: llm.PartEmpty},
			"",
		},
		{
			"text",
			llm.Part{Kind: llm.PartText, Text: "done"},
			"done",
		},
	}

	for _, testCase := range cases {
		part := testCase.part
		part.ResultOf = &llm.ToolResultRef{CallID: "call_a"}

		request := &llm.Request{
			Model: "test-model",
			Messages: []llm.Message{{
				Role:  llm.RoleAssistant,
				Parts: []llm.Part{part},
			}},
		}

		fields := encodeTestBody(t, Options{}, request)
		messages := encodeTestMessages(t, fields)

		if len(messages) != 1 {
			t.Fatalf("%s: messages: %d", testCase.name, len(messages))
		}

		text, _ := encodeTestContent(t, messages[0])
		if text != testCase.content {
			t.Fatalf(
				"%s: content %q, want %q",
				testCase.name,
				text,
				testCase.content,
			)
		}
	}
}

func TestEncodeTools(t *testing.T) {
	request := encodeTestTextRequest("hi")
	request.Tools = []llm.ToolSchema{{
		Name:        "weather",
		Description: "get weather",
		Parameters:  json.RawMessage(`{"type":"object"}`),
		Strict:      encodeTestBool(true),
	}}

	fields := encodeTestBody(t, Options{}, request)

	var tools []wireTool
	if err := json.Unmarshal(fields["tools"], &tools); err != nil {
		t.Fatalf("decode tools: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("tools: %+v", tools)
	}
	if tools[0].Type != "function" ||
		tools[0].Function.Name != "weather" ||
		tools[0].Function.Description != "get weather" ||
		string(tools[0].Function.Parameters) != `{"type":"object"}` ||
		tools[0].Function.Strict == nil ||
		!*tools[0].Function.Strict {
		t.Fatalf("tool: %+v", tools[0])
	}
}

func TestEncodeConfigFields(t *testing.T) {
	request := encodeTestTextRequest("hi")
	request.Config = &llm.Config{
		Temperature:     encodeTestFloat(0.2),
		TopP:            encodeTestFloat(0.9),
		TopK:            encodeTestInt(40),
		MaxOutputTokens: encodeTestInt(256),
		Stop:            []string{"a", "b"},
	}

	fields := encodeTestBody(t, Options{}, request)

	if string(fields["temperature"]) != "0.2" {
		t.Fatalf("temperature: %s", fields["temperature"])
	}
	if string(fields["topP"]) != "0.9" {
		t.Fatalf("topP: %s", fields["topP"])
	}
	if string(fields["top_k"]) != "40" {
		t.Fatalf("top_k: %s", fields["top_k"])
	}
	if string(fields["max_tokens"]) != "256" {
		t.Fatalf("max_tokens: %s", fields["max_tokens"])
	}
	if _, exists := fields["max_completion_tokens"]; exists {
		t.Fatal("both output limits must not be sent")
	}
	if string(fields["stop"]) != `["a","b"]` {
		t.Fatalf("stop: %s", fields["stop"])
	}
}

func TestEncodeConfigOutputLimitField(t *testing.T) {
	request := encodeTestTextRequest("hi")
	request.Config = &llm.Config{MaxOutputTokens: encodeTestInt(256)}

	fields := encodeTestBody(
		t,
		Options{MaxTokensParam: MaxCompletionTokensField},
		request,
	)

	if string(fields["max_completion_tokens"]) != "256" {
		t.Fatalf("max_completion_tokens: %s", fields["max_completion_tokens"])
	}
	if _, exists := fields["max_tokens"]; exists {
		t.Fatalf("max_tokens: %s", fields["max_tokens"])
	}
}

func TestEncodeToolChoice(t *testing.T) {
	cases := []struct {
		name string
		// mode 与 name 用于构造 ToolChoice。
		choice   llm.ToolChoice
		want     string
		parallel string
	}{
		{
			"auto",
			llm.ToolChoice{Mode: llm.ToolChoiceAuto},
			`"auto"`,
			"",
		},
		{
			"none",
			llm.ToolChoice{Mode: llm.ToolChoiceNone},
			`"none"`,
			"",
		},
		{
			"required with parallel",
			llm.ToolChoice{
				Mode:     llm.ToolChoiceRequired,
				Parallel: encodeTestBool(true),
			},
			`"required"`,
			"true",
		},
		{
			"named",
			llm.ToolChoice{
				Mode: llm.ToolChoiceNamed,
				Name: "weather",
			},
			`{"type":"function","function":{"name":"weather"}}`,
			"",
		},
		{
			"parallel disabled",
			llm.ToolChoice{
				Mode:     llm.ToolChoiceAuto,
				Parallel: encodeTestBool(false),
			},
			`"auto"`,
			"false",
		},
	}

	for _, testCase := range cases {
		request := encodeTestTextRequest("hi")
		request.Config = &llm.Config{ToolChoice: &testCase.choice}

		fields := encodeTestBody(t, Options{}, request)

		if string(fields["tool_choice"]) != testCase.want {
			t.Errorf(
				"%s: tool_choice: %s",
				testCase.name,
				fields["tool_choice"],
			)
		}

		parallel, exists := fields["parallel_tool_calls"]
		if testCase.parallel == "" {
			if exists {
				t.Errorf(
					"%s: parallel_tool_calls: %s",
					testCase.name,
					parallel,
				)
			}
			continue
		}
		if string(parallel) != testCase.parallel {
			t.Errorf(
				"%s: parallel_tool_calls: %s",
				testCase.name,
				parallel,
			)
		}
	}
}

func TestEncodeResponseFormat(t *testing.T) {
	cases := []struct {
		name   string
		format llm.ResponseFormat
		want   string
	}{
		{
			"text",
			llm.ResponseFormat{Type: llm.ResponseFormatText},
			`{"type":"text"}`,
		},
		{
			"json object",
			llm.ResponseFormat{Type: llm.ResponseFormatJSONObject},
			`{"type":"json_object"}`,
		},
		{
			"json schema",
			llm.ResponseFormat{
				Type:        llm.ResponseFormatJSONSchema,
				Name:        "answer",
				Description: "the answer",
				Schema:      json.RawMessage(`{"type":"object"}`),
				Strict:      encodeTestBool(true),
			},
			`{"type":"json_schema","json_schema":{` +
				`"name":"answer","description":"the answer",` +
				`"schema":{"type":"object"},"strict":true}}`,
		},
	}

	for _, testCase := range cases {
		request := encodeTestTextRequest("hi")
		request.Config = &llm.Config{ResponseFormat: &testCase.format}

		fields := encodeTestBody(t, Options{}, request)

		if string(fields["response_format"]) != testCase.want {
			t.Errorf(
				"%s: response_format: %s",
				testCase.name,
				fields["response_format"],
			)
		}
	}
}

func TestEncodeReasoningEffort(t *testing.T) {
	cases := []struct {
		name   string
		value  llm.Reasoning
		want   string
		failed bool
	}{
		{"low", llm.Reasoning{Effort: llm.ReasoningLow}, "low", false},
		{"high", llm.Reasoning{Effort: llm.ReasoningHigh}, "high", false},
		{
			"minimal",
			llm.Reasoning{Effort: llm.ReasoningMinimal},
			"minimal",
			false,
		},
		{
			"explicitly disabled",
			llm.Reasoning{Enabled: encodeTestBool(false)},
			"none",
			false,
		},
		{"unset", llm.Reasoning{}, "", false},
		{
			"xhigh has no equivalent",
			llm.Reasoning{Effort: llm.ReasoningXHigh},
			"",
			true,
		},
		{
			"budget has no equivalent",
			llm.Reasoning{BudgetTokens: encodeTestInt(1024)},
			"",
			true,
		},
		{
			"summary has no equivalent",
			llm.Reasoning{
				Effort:  llm.ReasoningLow,
				Summary: encodeTestBool(true),
			},
			"",
			true,
		},
		{
			"enabled without effort",
			llm.Reasoning{Enabled: encodeTestBool(true)},
			"",
			true,
		},
	}

	for _, testCase := range cases {
		request := encodeTestTextRequest("hi")
		request.Config = &llm.Config{Reasoning: &testCase.value}

		fields, err := func() (map[string]json.RawMessage, error) {
			body, err := encodeTestClient(t, Options{}).buildBody(request)
			if err != nil {
				return nil, err
			}

			var decoded map[string]json.RawMessage
			if err := json.Unmarshal(body, &decoded); err != nil {
				return nil, err
			}

			return decoded, nil
		}()

		if testCase.failed {
			if err == nil {
				t.Errorf("%s: expected an error", testCase.name)
				continue
			}

			var failure *llm.Error
			if !errors.As(err, &failure) ||
				failure.Kind != llm.ErrorUnsupported {
				t.Errorf("%s: error %v", testCase.name, err)
			}
			continue
		}

		if err != nil {
			t.Errorf("%s: %v", testCase.name, err)
			continue
		}

		if testCase.want == "" {
			if _, exists := fields["reasoning_effort"]; exists {
				t.Errorf(
					"%s: reasoning_effort: %s",
					testCase.name,
					fields["reasoning_effort"],
				)
			}
			continue
		}

		if string(fields["reasoning_effort"]) != `"`+testCase.want+`"` {
			t.Errorf(
				"%s: reasoning_effort: %s",
				testCase.name,
				fields["reasoning_effort"],
			)
		}
	}
}

func TestEncodeExtraBodyIsMerged(t *testing.T) {
	fields := encodeTestBody(t, Options{
		ExtraBody: map[string]json.RawMessage{
			"seed":  json.RawMessage(`7`),
			"tools": json.RawMessage(`[]`),
		},
	}, encodeTestTextRequest("hi"))

	if string(fields["seed"]) != "7" {
		t.Fatalf("seed: %s", fields["seed"])
	}
	if string(fields["tools"]) != "[]" {
		t.Fatalf("tools: %s", fields["tools"])
	}
}

func TestEncodeExtraBodyConflictIsRejected(t *testing.T) {
	_, err := encodeTestClient(t, Options{
		ExtraBody: map[string]json.RawMessage{
			"model": json.RawMessage(`"other"`),
		},
	}).buildBody(encodeTestTextRequest("hi"))

	var failure *llm.Error
	if !errors.As(err, &failure) {
		t.Fatalf("error type: %v", err)
	}
	if failure.Kind != llm.ErrorInvalidRequest {
		t.Fatalf("kind: %q", failure.Kind)
	}
}

func TestEncodeMessageOrderAndCount(t *testing.T) {
	// 消息条数只有两种情况会变：工具结果被展开成多条。
	request := &llm.Request{
		Model: "test-model",
		Messages: []llm.Message{
			{
				Role:  llm.RoleSystem,
				Parts: []llm.Part{{Kind: llm.PartText, Text: "s"}},
			},
			{
				Role:  llm.RoleUser,
				Parts: []llm.Part{{Kind: llm.PartText, Text: "u"}},
			},
		},
	}

	fields := encodeTestBody(t, Options{}, request)
	messages := encodeTestMessages(t, fields)

	if len(messages) != 2 {
		t.Fatalf("messages: %d", len(messages))
	}
	if string(messages[0]["role"]) != `"system"` ||
		string(messages[1]["role"]) != `"user"` {
		t.Fatalf("roles: %s %s", messages[0]["role"], messages[1]["role"])
	}
}
