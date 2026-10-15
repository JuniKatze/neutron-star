package llm

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

// streamState 保存一次流消费的校验状态。
// 零值可直接使用。它不支持并发操作，
// 开始处理事件后不得复制或复用到另一次消费。
//
// final 仅表示已经收到候选结果；只有上游正常返回后，
// 调用方才可以通过 finish 取得成功结果。
type streamState struct {
	final    *Response
	terminal error

	parts map[int]*streamPartState
}

// streamPartState 保存一个最终内容位置的增量累计值。
// strings.Builder 使用后不得复制，因此该状态通过指针保存。
type streamPartState struct {
	kind PartKind

	text strings.Builder

	callID   string
	callName string

	arguments    strings.Builder
	sawArguments bool
}

// step 处理一项原始输出，并把待交付的 delta 交给 deliver。
//
// 它是流生命周期的唯一实现：GuardStream 与 Consume 都通过它执行同样的
// 检查顺序——已终态、上下文失败、载荷合法性、增量一致性。两处各写一遍
// 检查顺序，改动时就会悄悄漂移。
//
// ctx 为 nil 表示调用方已经完成取消检查：Consume 消费的流已经过
// GuardStream，取消在更早的阶段处理。
//
// deliver 返回 false 表示下游要求停止；返回值报告生产者是否可以继续。
func (s *streamState) step(
	ctx context.Context,
	event *StreamEvent,
	err error,
	deliver func(*StreamEvent) bool,
) bool {
	if s.terminal != nil {
		return false
	}

	if ctx != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			s.terminal = contextStreamError(contextErr)
			return false
		}
	}

	delta, keepGoing := s.accept(event, err)
	if !keepGoing {
		return false
	}

	if delta == nil {
		// 候选 final 已暂存，继续确认上游正常结束。
		return true
	}

	return deliver(delta)
}

// accept 接收一项原始输出。
// delta 返回独立事件副本；final 暂存并返回 nil。
// 布尔返回值表示生产者是否可以继续输出。
func (s *streamState) accept(
	event *StreamEvent,
	err error,
) (*StreamEvent, bool) {
	fail := func(cause error) (*StreamEvent, bool) {
		s.terminal = cause
		return nil, false
	}

	if s.terminal != nil {
		return nil, false
	}

	if s.final != nil {
		return fail(protocolError(
			"stream emitted an item after final",
			err,
		))
	}

	if err != nil {
		if event != nil {
			return fail(protocolError(
				"stream emitted an event and an error together",
				err,
			))
		}

		return fail(contextStreamError(err))
	}

	if event == nil {
		return fail(protocolError(
			"stream emitted (nil, nil)",
			nil,
		))
	}

	// 冻结当前事件，避免上游后续修改污染已接收内容。
	snapshot := event.Clone()

	if err := snapshot.Validate(); err != nil {
		return fail(protocolError(
			"stream emitted an invalid event",
			err,
		))
	}

	if snapshot.Kind == StreamDelta {
		if err := s.applyDelta(snapshot.Delta); err != nil {
			return fail(protocolError(
				"stream delta sequence is inconsistent",
				err,
			))
		}

		return snapshot, true
	}

	if err := s.verifyFinal(&snapshot.Final.Message); err != nil {
		return fail(protocolError(
			"stream final differs from accumulated deltas",
			err,
		))
	}

	s.final = snapshot.Final
	return nil, true
}

// applyDelta 将已校验的增量写入对应位置的累计状态。
func (s *streamState) applyDelta(delta *PartDelta) error {
	if s.parts == nil {
		s.parts = make(map[int]*streamPartState)
	}

	part := s.parts[delta.PartIndex]

	if part == nil {
		part = &streamPartState{kind: delta.Kind}
		s.parts[delta.PartIndex] = part
	} else if part.kind != delta.Kind {
		return fmt.Errorf(
			"part %d changed kind from %q to %q",
			delta.PartIndex,
			part.kind,
			delta.Kind,
		)
	}

	switch delta.Kind {
	case PartText, PartReasoning, PartRefusal:
		part.text.WriteString(delta.Text)

	case PartToolCall:
		call := delta.ToolCall

		if call.ID != "" {
			if part.callID != "" && part.callID != call.ID {
				return fmt.Errorf(
					"part %d has conflicting tool call IDs",
					delta.PartIndex,
				)
			}
			part.callID = call.ID
		}

		if call.Name != "" {
			if part.callName != "" && part.callName != call.Name {
				return fmt.Errorf(
					"part %d has conflicting tool call names",
					delta.PartIndex,
				)
			}
			part.callName = call.Name
		}

		if call.ArgumentsDelta != "" {
			part.arguments.WriteString(call.ArgumentsDelta)
			part.sawArguments = true
		}
	}

	return nil
}

// verifyFinal 检查已经公开预览的字段与最终消息完全一致。
// 未通过增量提供的字段及内容块可以只出现在最终消息中。
// message 必须已经通过 Validate。
func (s *streamState) verifyFinal(message *Message) error {
	indices := make([]int, 0, len(s.parts))
	for index := range s.parts {
		indices = append(indices, index)
	}

	// 固定校验顺序，避免多个错误位置产生不稳定的首个错误。
	slices.Sort(indices)

	for _, index := range indices {
		state := s.parts[index]

		if index >= len(message.Parts) {
			return fmt.Errorf(
				"delta part %d is absent from final message",
				index,
			)
		}

		part := &message.Parts[index]

		if part.Kind != state.kind {
			return fmt.Errorf(
				"part %d kind differs from streamed kind",
				index,
			)
		}

		switch state.kind {
		case PartText, PartReasoning, PartRefusal:
			if part.Text != state.text.String() {
				return fmt.Errorf(
					"part %d text differs from accumulated deltas",
					index,
				)
			}

		case PartToolCall:
			call := part.ToolCall

			if state.callID != "" && call.ID != state.callID {
				return fmt.Errorf(
					"part %d final tool call ID differs",
					index,
				)
			}

			if state.callName != "" && call.Name != state.callName {
				return fmt.Errorf(
					"part %d final tool call name differs",
					index,
				)
			}

			if state.sawArguments &&
				call.Arguments != state.arguments.String() {
				return fmt.Errorf(
					"part %d final tool arguments differ",
					index,
				)
			}
		}
	}

	return nil
}

// finish 在生产者正常返回后取得最终结果。
// 任何已记录错误优先于候选 final；没有 final 的结束属于协议错误。
func (s *streamState) finish() (*Response, error) {
	if s.terminal != nil {
		return nil, s.terminal
	}

	if s.final == nil {
		s.terminal = protocolError(
			"stream ended without a final response",
			nil,
		)
		return nil, s.terminal
	}

	return s.final, nil
}
