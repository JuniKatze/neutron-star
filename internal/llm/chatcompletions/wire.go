package chatcompletions

import "encoding/json"

// 本文件保存 Chat Completions 的 wire 类型。
//
// 接收时宽容：未知字段、缺失的可选字段与 null 都按未提供处理。
// 发送时保守：字段只在投影显式决定时才出现，不用零值猜测意图。

// requestBody 是一次 Chat Completions 请求体。
//
// Messages 保存逐条编码后的对象：assistant 推理字段的 wire 名由选项决定，
// 无法用固定的结构体标签表达。
type requestBody struct {
	Model    string            `json:"model"`
	Messages []json.RawMessage `json:"messages"`
	Stream   bool              `json:"stream"`

	StreamOptions *wireStreamOptions `json:"stream_options,omitempty"`

	Temperature *float64 `json:"temperature,omitempty"`
	TopP        *float64 `json:"topP,omitempty"`
	TopK        *int     `json:"top_k,omitempty"`
	Stop        []string `json:"stop,omitempty"`

	// 输出上限的字段名由 Options.MaxTokensParam 选择，两者互斥。
	MaxTokens           *int `json:"max_tokens,omitempty"`
	MaxCompletionTokens *int `json:"max_completion_tokens,omitempty"`

	Tools             []wireTool          `json:"tools,omitempty"`
	ToolChoice        json.RawMessage     `json:"tool_choice,omitempty"`
	ParallelToolCalls *bool               `json:"parallel_tool_calls,omitempty"`
	ResponseFormat    *wireResponseFormat `json:"response_format,omitempty"`
	ReasoningEffort   string              `json:"reasoning_effort,omitempty"`
}

// wireStreamOptions 请求服务在流结束前返回 usage。
type wireStreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

// wireMessage 是一条已投影的请求消息。
// assistant 推理字段由调用方在编码后注入，因此不在本结构上。
type wireMessage struct {
	Role string `json:"role"`

	// Content 是字符串或 []wireContentPart。
	// 为 nil 时整个 content 键不出现。
	Content any `json:"content,omitempty"`

	ToolCalls  []wireToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	Name       string         `json:"name,omitempty"`
}

// wireContentPart 是内容数组的一项。
type wireContentPart struct {
	Type       string          `json:"type"`
	Text       string          `json:"text,omitempty"`
	ImageURL   *wireImageURL   `json:"image_url,omitempty"`
	InputAudio *wireInputAudio `json:"input_audio,omitempty"`
}

// wireImageURL 是图片引用；URL 可以是 HTTP(S) 地址或 data URI。
type wireImageURL struct {
	URL string `json:"url"`
}

// wireInputAudio 是内联音频，格式使用供应商的容器枚举。
type wireInputAudio struct {
	Data   string `json:"data"`
	Format string `json:"format"`
}

// wireTool 声明一个函数工具。
type wireTool struct {
	Type     string           `json:"type"`
	Function wireToolFunction `json:"function"`
}

// wireToolFunction 是一个函数工具的定义。
type wireToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
	Strict      *bool           `json:"strict,omitempty"`
}

// wireNamedToolChoice 是 named 模式的工具选择。
type wireNamedToolChoice struct {
	Type     string           `json:"type"`
	Function wireToolCallName `json:"function"`
}

// wireToolCallName 是 named 模式指定的工具名。
type wireToolCallName struct {
	Name string `json:"name"`
}

// wireResponseFormat 描述生成结果的格式约束。
type wireResponseFormat struct {
	Type       string          `json:"type"`
	JSONSchema *wireJSONSchema `json:"json_schema,omitempty"`
}

// wireJSONSchema 是 json_schema 模式的载荷。
type wireJSONSchema struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Schema      json.RawMessage `json:"schema"`
	Strict      *bool           `json:"strict,omitempty"`
}

// wireToolCall 是完整的（非增量）工具调用。
type wireToolCall struct {
	Type     string               `json:"type"`
	ID       string               `json:"id"`
	Function wireToolCallFunction `json:"function"`
}

// wireToolCallFunction 是完整工具调用的函数载荷。
type wireToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// wireChunk 是流式响应中的一项。
// 选择项为空时仍可能携带 usage，这是包含统计的结束项。
type wireChunk struct {
	ID      string       `json:"id"`
	Model   string       `json:"model"`
	Choices []wireChoice `json:"choices"`
	Usage   *wireUsage   `json:"usage"`
	Error   *wireError   `json:"error"`
}

// wireChoice 是一个候选结果。
type wireChoice struct {
	Index        int        `json:"index"`
	Delta        *wireDelta `json:"delta"`
	FinishReason *string    `json:"finish_reason"`
}

// wireDelta 是一个候选结果的增量。
// Content 与 Refusal 使用指针，以区分 null 与缺失。
type wireDelta struct {
	Role             string              `json:"role"`
	Content          *string             `json:"content"`
	Refusal          *string             `json:"refusal"`
	ReasoningContent *string             `json:"reasoning_content"`
	ToolCalls        []wireToolCallDelta `json:"tool_calls"`
}

// wireToolCallDelta 是一个工具调用的增量。
// Index 是供应商侧的工具调用序号，不是 IR 的内容块位置。
type wireToolCallDelta struct {
	Index    int                    `json:"index"`
	ID       string                 `json:"id"`
	Type     string                 `json:"type"`
	Function *wireToolCallFuncDelta `json:"function"`
}

// wireToolCallFuncDelta 是工具调用增量的函数载荷。
// 名称一般一次给全，参数字符串按片段追加。
type wireToolCallFuncDelta struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// wireUsage 是供应商原始 token 统计。
type wireUsage struct {
	PromptTokens     *int64 `json:"prompt_tokens"`
	CompletionTokens *int64 `json:"completion_tokens"`
	TotalTokens      *int64 `json:"total_tokens"`

	PromptTokensDetails     *wirePromptTokenDetails     `json:"prompt_tokens_details"`
	CompletionTokensDetails *wireCompletionTokenDetails `json:"completion_tokens_details"`
}

// wirePromptTokenDetails 是输入侧的明细统计。
type wirePromptTokenDetails struct {
	CachedTokens *int64 `json:"cached_tokens"`
}

// wireCompletionTokenDetails 是输出侧的明细统计。
type wireCompletionTokenDetails struct {
	ReasoningTokens *int64 `json:"reasoning_tokens"`
}

// wireError 是供应商报告的错误。
// Code 在部分实现中是字符串，在另一些实现中是数字，因此保留原文。
type wireError struct {
	Message string          `json:"message"`
	Type    string          `json:"type"`
	Code    json.RawMessage `json:"code"`
	Param   string          `json:"param"`
}

// wireErrorEnvelope 是错误响应体。
type wireErrorEnvelope struct {
	Error *wireError `json:"error"`
}
