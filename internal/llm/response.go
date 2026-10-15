package llm

import (
	"errors"
	"fmt"
	"strings"
)

// StopReason 表示一次已完整接收的生成结果的停止原因。
// 传输失败或协议解析失败应通过 error 返回。
type StopReason string

const (
	// StopEndTurn 表示模型结束本轮输出。
	StopEndTurn StopReason = "end_turn"

	// StopToolUse 表示模型请求应用处理工具调用。
	StopToolUse StopReason = "tool_use"

	// StopMaxTokens 表示生成达到输出 token 上限。
	StopMaxTokens StopReason = "max_tokens"

	// StopSequence 表示生成命中停止序列。
	StopSequence StopReason = "stop_sequence"

	// StopRefusal 表示模型拒绝请求。
	StopRefusal StopReason = "refusal"

	// StopPauseTurn 表示供应商暂停本轮，需要按其协议继续。
	StopPauseTurn StopReason = "pause_turn"

	// StopContextWindowExceeded 表示生成受上下文窗口上限限制。
	StopContextWindowExceeded StopReason = "context_window_exceeded"

	// StopOther 表示未归入已知类别的供应商停止原因。
	StopOther StopReason = "other"
)

// valid 报告 s 是否为已定义的停止原因。
func (s StopReason) valid() bool {
	switch s {
	case StopEndTurn,
		StopToolUse,
		StopMaxTokens,
		StopSequence,
		StopRefusal,
		StopPauseTurn,
		StopContextWindowExceeded,
		StopOther:
		return true

	default:
		return false
	}
}

// Response 保存一次完整接收的模型生成结果。
// 完整接收不保证回答未被截断、工具参数合法或工具可以执行。
// Response 的零值无效。
//
// Message 必须属于 assistant，且不得包含工具结果。
// 空 assistant 消息合法，但不能用于 StopToolUse。
//
// 调用方法期间，调用方必须保证没有其他 goroutine
// 并发修改 Response 或其引用的数据。
type Response struct {
	// Message 保存模型生成的有序内容。
	Message Message `json:"message"`

	// StopReason 保存归一化后的停止原因。
	StopReason StopReason `json:"stopReason"`

	// ProviderStopReason 保存供应商原始停止原因。
	// StopOther 要求该字段非空；其他停止原因也允许保留它。
	ProviderStopReason string `json:"providerStopReason,omitempty"`

	// Usage 保存归一化后的 token 统计。
	// nil 表示供应商未提供可用统计。
	Usage *Usage `json:"usage,omitempty"`

	// Meta 保存供应商响应元数据。
	Meta ResponseMeta `json:"meta"`
}

// ResponseMeta 保存供应商响应的辅助标识。
// 零值有效，表示这些信息未报告。
// 元数据不用于决定工具是否可以执行。
type ResponseMeta struct {
	// ResponseID 是供应商响应标识。
	ResponseID string `json:"responseId,omitempty"`

	// RequestID 是供应商或网关报告的请求标识。
	RequestID string `json:"requestId,omitempty"`

	// Model 是供应商报告的实际模型标识。
	// 它可以与请求中的模型别名不同。
	Model string `json:"model,omitempty"`
}

// Validate 检查 r 的模型输出、停止原因和 token 统计。
// r 为 nil 时返回错误。本方法不修改 r。
//
// 工具调用与停止原因必须自洽：携带调用的结果只允许 tool_use、
// max_tokens 与 context_window_exceeded。供应商报告"正常停止却
// 带调用"时，适配器必须归一为上述组合，否则整条结果会被拒绝。
//
// Validate 不检查请求历史冲突、工具是否已声明、ToolChoice、
// 函数参数对象、参数 Schema 或执行权限。
// 工具参数原文即使不是合法 JSON，也可以作为生成结果保存。
func (r *Response) Validate() error {
	if r == nil {
		return errors.New("response is nil")
	}

	if r.Message.Role != RoleAssistant {
		return errors.New(
			"message.role: model output must use assistant role",
		)
	}

	if err := r.Message.Validate(); err != nil {
		return fmt.Errorf("message: %w", err)
	}

	callCount := 0

	for index := range r.Message.Parts {
		part := &r.Message.Parts[index]

		if part.ResultOf != nil {
			return fmt.Errorf(
				"message.parts[%d].resultOf: "+
					"model output must not contain tool results",
				index,
			)
		}

		switch part.Kind {
		case PartError, PartEmpty:
			return fmt.Errorf(
				"message.parts[%d].kind: "+
					"%q is not valid model output",
				index,
				part.Kind,
			)

		case PartToolCall:
			callCount++
		}
	}

	if !r.StopReason.valid() {
		return fmt.Errorf(
			"stopReason: unknown stop reason %q",
			r.StopReason,
		)
	}

	if r.StopReason == StopOther &&
		strings.TrimSpace(r.ProviderStopReason) == "" {
		return errors.New(
			"providerStopReason: required for other stop reason",
		)
	}

	if r.StopReason == StopToolUse && callCount == 0 {
		return errors.New(
			"stopReason: tool_use requires at least one tool call",
		)
	}

	if callCount != 0 {
		switch r.StopReason {
		case StopToolUse, StopMaxTokens, StopContextWindowExceeded:
			// 截断结果允许保存调用内容，但不因此允许执行。
		default:
			return fmt.Errorf(
				"stopReason: %q must not contain tool calls",
				r.StopReason,
			)
		}
	}

	if err := r.Usage.Validate(); err != nil {
		return fmt.Errorf("usage: %w", err)
	}

	return nil
}

// Clone 返回 r 的深拷贝。
// 返回值不与 r 共享消息内容、回放状态或 Usage 指针。
// r 为 nil 时返回 nil。Clone 不校验或修复结果。
func (r *Response) Clone() *Response {
	if r == nil {
		return nil
	}

	out := *r
	out.Message = *r.Message.Clone()
	out.Usage = r.Usage.Clone()

	return &out
}

// ValidateFor 检查 r 是否为 req 的合法结构性完成结果。
// 它先校验请求及 r，再检查 r 的新工具调用 ID 是否与请求历史冲突。
// 本方法不修改任何输入。
//
// ValidateFor 不检查工具是否已声明、ToolChoice、
// 参数对象、参数 Schema、执行权限或响应模型名是否与请求相同。
// 这些检查不应阻止保存模型实际返回的内容，执行决策由 agent 负责。
//
// 新结果中的工具调用尚未执行，不要求已经存在工具结果。
// 因此，本方法不将新结果拼接到请求后调用 Request.Validate。
func (r *Response) ValidateFor(req *Request) error {
	if err := req.Validate(); err != nil {
		return fmt.Errorf("request: %w", err)
	}
	if err := r.Validate(); err != nil {
		return fmt.Errorf("response: %w", err)
	}

	historicalCalls := make(map[string]struct{})

	for messageIndex := range req.Messages {
		message := &req.Messages[messageIndex]

		for index := range message.Parts {
			part := &message.Parts[index]

			if part.Kind == PartToolCall {
				historicalCalls[part.ToolCall.ID] = struct{}{}
			}
		}
	}

	for index := range r.Message.Parts {
		part := &r.Message.Parts[index]

		if part.Kind != PartToolCall {
			continue
		}

		if _, exists := historicalCalls[part.ToolCall.ID]; exists {
			return fmt.Errorf(
				"response.message.parts[%d].toolCall.id: "+
					"conflicts with historical call %q",
				index,
				part.ToolCall.ID,
			)
		}
	}

	return nil
}

// Usage 保存归一化后的 token 统计。
// nil 字段表示未报告，指向零的字段表示明确报告为零。
// nil Usage 和 Usage 的零值均有效。
//
// 输入总量包含缓存输入，输出总量包含推理输出。
// 各供应商原始统计必须由适配器转换到该口径。
//
// 调用方法期间，调用方必须保证没有其他 goroutine
// 并发修改 Usage 或其指针字段。
type Usage struct {
	// InputTokens 是总输入 token 数。
	InputTokens *int64 `json:"inputTokens,omitempty"`

	// OutputTokens 是总输出 token 数。
	OutputTokens *int64 `json:"outputTokens,omitempty"`

	// CacheReadTokens 是缓存读取输入的明细。
	CacheReadTokens *int64 `json:"cacheReadTokens,omitempty"`

	// CacheWriteTokens 是缓存写入输入的明细。
	CacheWriteTokens *int64 `json:"cacheWriteTokens,omitempty"`

	// ReasoningTokens 是推理输出的明细。
	ReasoningTokens *int64 `json:"reasoningTokens,omitempty"`

	// TotalTokens 是输入与输出 token 总量。
	TotalTokens *int64 `json:"totalTokens,omitempty"`
}

// Validate 检查 u 的非负计数及已知字段间的关系。
// u 为 nil 时返回 nil。本方法不修改 u，不推算或补填字段。
//
// 缓存明细不得超过已知输入，推理明细不得超过已知输出。
// 总量不得小于已知输入或输出。输入、输出和总量均已报告时，
// 总量必须等于输入与输出之和。
//
// Validate 不假定缓存读取与缓存写入明细互斥，
// 因此不检查二者之和是否超过输入。
func (u *Usage) Validate() error {
	if u == nil {
		return nil
	}

	fields := []struct {
		name  string
		value *int64
	}{
		{"inputTokens", u.InputTokens},
		{"outputTokens", u.OutputTokens},
		{"cacheReadTokens", u.CacheReadTokens},
		{"cacheWriteTokens", u.CacheWriteTokens},
		{"reasoningTokens", u.ReasoningTokens},
		{"totalTokens", u.TotalTokens},
	}

	for _, field := range fields {
		if field.value != nil && *field.value < 0 {
			return fmt.Errorf("%s: must not be negative", field.name)
		}
	}

	subsets := []struct {
		name      string
		value     *int64
		totalName string
		total     *int64
	}{
		{
			"cacheReadTokens", u.CacheReadTokens,
			"inputTokens", u.InputTokens,
		},
		{
			"cacheWriteTokens", u.CacheWriteTokens,
			"inputTokens", u.InputTokens,
		},
		{
			"reasoningTokens", u.ReasoningTokens,
			"outputTokens", u.OutputTokens,
		},
		{
			"inputTokens", u.InputTokens,
			"totalTokens", u.TotalTokens,
		},
		{
			"outputTokens", u.OutputTokens,
			"totalTokens", u.TotalTokens,
		},
	}

	for _, subset := range subsets {
		if subset.value != nil &&
			subset.total != nil &&
			*subset.value > *subset.total {
			return fmt.Errorf(
				"%s: must not exceed %s",
				subset.name,
				subset.totalName,
			)
		}
	}

	if u.InputTokens != nil &&
		u.OutputTokens != nil &&
		u.TotalTokens != nil {
		// 前面的检查保证 total >= input >= 0。
		// 用减法检查等式，避免 input + output 发生整数溢出。
		if *u.OutputTokens != *u.TotalTokens-*u.InputTokens {
			return errors.New(
				"totalTokens: must equal inputTokens plus outputTokens",
			)
		}
	}

	return nil
}

// Clone 返回 u 的深拷贝，不共享任何 token 计数指针。
// u 为 nil 时返回 nil。Clone 保留未报告字段及原始数值，不校验统计。
func (u *Usage) Clone() *Usage {
	if u == nil {
		return nil
	}

	out := *u
	out.InputTokens = clonePtr(u.InputTokens)
	out.OutputTokens = clonePtr(u.OutputTokens)
	out.CacheReadTokens = clonePtr(u.CacheReadTokens)
	out.CacheWriteTokens = clonePtr(u.CacheWriteTokens)
	out.ReasoningTokens = clonePtr(u.ReasoningTokens)
	out.TotalTokens = clonePtr(u.TotalTokens)

	return &out
}
