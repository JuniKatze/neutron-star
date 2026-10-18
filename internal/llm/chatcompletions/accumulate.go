package chatcompletions

import (
	"errors"
	"fmt"
	"strings"

	"neutron-star/internal/llm"
)

// 本文件把 wire 增量装配为最终消息。
//
// 内容块位置按首次出现分配，之后不再重新编号：IR 要求流中的 PartIndex
// 与最终 Message.Parts 的位置一致。
//
// 归一化规则（其余不一致一律作为协议错误）：
//   - finish_reason 为 stop 但结果携带工具调用时归为 tool_use：IR 只允许
//     带调用的 tool_use / max_tokens / context_window_exceeded，
//     供应商原文保留在 ProviderStopReason 里；
//   - 未识别的 finish_reason 归为 other，并保留原文；
//   - usage 各字段按同名含义搬运，total 不参与一致性判断。
//
// 多个候选（choices）无法用单个消息表达，直接报错。

// partKey 标识一个最终内容位置的来源。
type partKey struct {
	kind  llm.PartKind
	index int
}

// partBuilder 保存一个最终内容位置的增量累计值。
// strings.Builder 使用后不得复制，因此构建器始终通过指针保存。
type partBuilder struct {
	kind llm.PartKind

	text strings.Builder

	callID       string
	callName     string
	arguments    strings.Builder
	sawArguments bool
}

// accumulator 保存一次流式接收的装配状态。
// 它不并发使用，开始接收后不得复制或复用到另一次接收。
type accumulator struct {
	reasoningField string

	builders  []*partBuilder
	positions map[partKey]int

	responseID   string
	model        string
	usage        *wireUsage
	finishReason string
	sawFinish    bool
}

// newAccumulator 返回空装配状态。
// reasoningField 为空表示端点不支持推理字段，收到的推理内容不装配。
func newAccumulator(reasoningField string) *accumulator {
	return &accumulator{
		reasoningField: reasoningField,
		positions:      make(map[partKey]int),
	}
}

// accept 接收一个流项，返回需要交付的增量。
// 统计项与保活项不产生增量。
func (a *accumulator) accept(
	chunk *wireChunk,
) ([]*llm.PartDelta, error) {
	if chunk.ID != "" {
		a.responseID = chunk.ID
	}
	if chunk.Model != "" {
		a.model = chunk.Model
	}
	if chunk.Usage != nil {
		a.usage = chunk.Usage
	}

	if len(chunk.Choices) == 0 {
		return nil, nil
	}
	if len(chunk.Choices) > 1 {
		return nil, unsupportedError(
			"multiple choices cannot be represented as one message",
		)
	}

	choice := &chunk.Choices[0]
	if choice.Index != 0 {
		return nil, unsupportedError(fmt.Sprintf(
			"choice index %d cannot be represented",
			choice.Index,
		))
	}

	if choice.FinishReason != nil {
		if a.sawFinish {
			return nil, errors.New("finish reason reported more than once")
		}

		a.finishReason = *choice.FinishReason
		a.sawFinish = true
	}

	if choice.Delta == nil {
		return nil, nil
	}

	return a.applyDelta(choice.Delta)
}

// applyDelta 把一个 wire 增量写入累计状态，并返回对应的 IR 增量。
// 空增量不下发：IR 拒绝空文本增量。
func (a *accumulator) applyDelta(
	delta *wireDelta,
) ([]*llm.PartDelta, error) {
	deltas := make([]*llm.PartDelta, 0, 1+len(delta.ToolCalls))

	if delta.Content != nil && *delta.Content != "" {
		deltas = append(deltas, a.appendText(
			llm.PartText,
			*delta.Content,
		))
	}

	// 拒绝内容成为独立的 refusal 内容块：IR 有对应的种类，
	// 不需要与普通文本混在一起。
	if delta.Refusal != nil && *delta.Refusal != "" {
		deltas = append(deltas, a.appendText(
			llm.PartRefusal,
			*delta.Refusal,
		))
	}

	if a.reasoningField != "" &&
		delta.ReasoningContent != nil &&
		*delta.ReasoningContent != "" {
		deltas = append(deltas, a.appendText(
			llm.PartReasoning,
			*delta.ReasoningContent,
		))
	}

	for index := range delta.ToolCalls {
		call, err := a.applyToolCall(delta.ToolCalls[index])
		if err != nil {
			return nil, err
		}
		if call != nil {
			deltas = append(deltas, call)
		}
	}

	return deltas, nil
}

// appendText 追加文本、推理或拒绝内容，并返回对应增量。
func (a *accumulator) appendText(
	kind llm.PartKind,
	text string,
) *llm.PartDelta {
	position := a.position(partKey{kind: kind})
	a.builders[position].text.WriteString(text)

	return &llm.PartDelta{
		PartIndex: position,
		Kind:      kind,
		Text:      text,
	}
}

// applyToolCall 追加一个工具调用增量。
//
// 只携带序号、没有任何载荷的增量既不分配位置，也不下发。
// ID 与名称按完整值处理：重复提供必须一致，否则报错而不是拼接。
func (a *accumulator) applyToolCall(
	call wireToolCallDelta,
) (*llm.PartDelta, error) {
	if call.Type != "" && call.Type != "function" {
		return nil, unsupportedError(fmt.Sprintf(
			"tool call type %q cannot be represented", call.Type,
		))
	}

	var function *wireToolCallFuncDelta
	if call.Function != nil {
		function = call.Function
	}

	hasPayload := call.ID != "" ||
		(function != nil && (function.Name != "" ||
			function.Arguments != ""))
	if !hasPayload {
		return nil, nil
	}

	position := a.position(partKey{
		kind:  llm.PartToolCall,
		index: call.Index,
	})
	builder := a.builders[position]

	projected := &llm.ToolCallDelta{}

	if call.ID != "" {
		if builder.callID != "" && builder.callID != call.ID {
			return nil, fmt.Errorf(
				"tool call %d: id conflicts with an earlier id",
				call.Index,
			)
		}

		builder.callID = call.ID
		projected.ID = call.ID
	}

	if function != nil && function.Name != "" {
		if builder.callName != "" && builder.callName != function.Name {
			return nil, fmt.Errorf(
				"tool call %d: function name conflicts with an "+
					"earlier name",
				call.Index,
			)
		}

		builder.callName = function.Name
		projected.Name = function.Name
	}

	if function != nil && function.Arguments != "" {
		builder.arguments.WriteString(function.Arguments)
		builder.sawArguments = true
		projected.ArgumentsDelta = function.Arguments
	}

	if projected.ID == "" &&
		projected.Name == "" &&
		projected.ArgumentsDelta == "" {
		return nil, nil
	}

	return &llm.PartDelta{
		PartIndex: position,
		Kind:      llm.PartToolCall,
		ToolCall:  projected,
	}, nil
}

// position 返回内容块位置，首次出现时按顺序分配。
func (a *accumulator) position(key partKey) int {
	if position, exists := a.positions[key]; exists {
		return position
	}

	a.builders = append(a.builders, &partBuilder{kind: key.kind})
	position := len(a.builders) - 1
	a.positions[key] = position

	return position
}

// part 返回该位置装配出的内容块。
func (b *partBuilder) part() (llm.Part, error) {
	switch b.kind {
	case llm.PartText, llm.PartReasoning, llm.PartRefusal:
		return llm.Part{Kind: b.kind, Text: b.text.String()}, nil

	case llm.PartToolCall:
		return llm.Part{
			Kind: llm.PartToolCall,
			ToolCall: &llm.ToolCall{
				ID:        b.callID,
				Name:      b.callName,
				Arguments: b.arguments.String(),
			},
		}, nil
	}

	return llm.Part{}, fmt.Errorf("unknown part kind %q", b.kind)
}

// response 在流正常结束后装配最终结果。
// requestID 来自响应头，由调用方提供。
func (a *accumulator) response(requestID string) (*llm.Response, error) {
	if !a.sawFinish {
		return nil, protocolError(
			"stream ended without a finish reason",
			nil,
		)
	}

	parts := make([]llm.Part, 0, len(a.builders))
	hasCalls := false

	for index := range a.builders {
		part, err := a.builders[index].part()
		if err != nil {
			return nil, protocolError(fmt.Sprintf(
				"stream part %d cannot be assembled",
				index,
			), err)
		}

		if part.Kind == llm.PartToolCall {
			hasCalls = true
		}

		parts = append(parts, part)
	}

	stopReason, err := stopReason(a.finishReason, hasCalls)
	if err != nil {
		return nil, err
	}

	response := &llm.Response{
		Message: llm.Message{
			Role:  llm.RoleAssistant,
			Parts: parts,
		},
		StopReason:         stopReason,
		ProviderStopReason: a.finishReason,
		Usage:              normalizeUsage(a.usage),
		Meta: llm.ResponseMeta{
			ResponseID: a.responseID,
			RequestID:  requestID,
			Model:      a.model,
		},
	}

	if err := response.Validate(); err != nil {
		return nil, protocolError("assembled response is invalid", err)
	}

	return response, nil
}

// stopReason 把 finish_reason 归一化为 IR 停止原因。
func stopReason(
	finishReason string,
	hasCalls bool,
) (llm.StopReason, error) {
	switch finishReason {
	case "stop":
		if hasCalls {
			// 结果携带调用时，IR 只接受 tool_use 一类的停止原因；
			// 供应商原文保留在 ProviderStopReason 里。
			return llm.StopToolUse, nil
		}

		return llm.StopEndTurn, nil

	case "tool_calls", "function_call":
		if !hasCalls {
			return "", protocolError(
				"finish reason reports tool calls but none were received",
				nil,
			)
		}

		return llm.StopToolUse, nil

	case "length":
		return llm.StopMaxTokens, nil

	case "content_filter":
		return llm.StopRefusal, nil

	case "":
		return "", protocolError("empty finish reason", nil)
	}

	return llm.StopOther, nil
}

// normalizeUsage 把供应商统计换算为 IR 口径。
//
// 各字段按同名含义搬运；total 原样保留，不参与一致性判断
// （不同供应商对 total 的口径不一致）。
func normalizeUsage(raw *wireUsage) *llm.Usage {
	if raw == nil {
		return nil
	}

	usage := &llm.Usage{
		InputTokens:  raw.PromptTokens,
		OutputTokens: raw.CompletionTokens,
		TotalTokens:  raw.TotalTokens,
	}

	if details := raw.PromptTokensDetails; details != nil {
		usage.CacheReadTokens = details.CachedTokens
	}
	if details := raw.CompletionTokensDetails; details != nil {
		usage.ReasoningTokens = details.ReasoningTokens
	}

	return usage
}

// classifyStreamError 保留已经分类的错误，其余装配错误按协议错误包装。
func classifyStreamError(err error) error {
	var classified *llm.Error
	if errors.As(err, &classified) {
		return err
	}

	return protocolError("stream chunk is inconsistent", err)
}
