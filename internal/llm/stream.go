package llm

import (
	"errors"
	"fmt"
	"iter"
	"strings"
)

// Stream 是适配器内部产生的原始事件序列。
// 生产者必须同步调用 yield，并在 yield 返回 false 后停止。
// 事件和错误不能同时非 nil，也不能同时为 nil。
type Stream iter.Seq2[*StreamEvent, error]

// ValidatedStream 是经 GuardStream 保护的单次消费事件序列。
// 它输出零个或多个 delta，随后输出一个 final 或一个 error。
//
// ValidatedStream 是契约标记，不能仅通过类型转换获得保护。
// 实现必须实际调用 GuardStream。
type ValidatedStream iter.Seq2[*StreamEvent, error]

// StreamEventKind 表示流式事件类型。
type StreamEventKind string

const (
	// StreamDelta 表示供预览使用的内容增量。
	StreamDelta StreamEventKind = "delta"

	// StreamFinal 表示完整接收并通过流校验的生成结果。
	StreamFinal StreamEventKind = "final"
)

// StreamEvent 保存一项流式输出。
// Kind 决定 Delta 或 Final 中唯一有效的载荷。
// StreamEvent 的零值无效。
//
// 调用方法期间，调用方不得并发修改事件或其引用的数据。
type StreamEvent struct {
	// Kind 指定事件类型。
	Kind StreamEventKind `json:"kind"`

	// Delta 保存 delta 事件载荷。
	Delta *PartDelta `json:"delta,omitempty"`

	// Final 保存 final 事件载荷。
	Final *Response `json:"final,omitempty"`
}

// PartDelta 描述最终 Message.Parts 中某个位置的临时增量。
// PartDelta 不参与消息持久化，也不表示可执行工具调用。
// PartDelta 的零值无效。
type PartDelta struct {
	// PartIndex 是该内容块在最终 Message.Parts 中的位置。
	// 它不得为负数，也不得随生成过程重新编号。
	PartIndex int `json:"partIndex"`

	// Kind 是该位置的内容类型，流中不得改变。
	// 当前只支持 text、reasoning、refusal 和 tool_call 增量。
	Kind PartKind `json:"kind"`

	// Text 是 text、reasoning 或 refusal 的追加内容。
	Text string `json:"text,omitempty"`

	// ToolCall 是 tool_call 的增量载荷。
	ToolCall *ToolCallDelta `json:"toolCall,omitempty"`
}

// ToolCallDelta 描述工具调用的临时增量。
// 至少一个字段必须非空。
type ToolCallDelta struct {
	// ID 是完整调用标识，不采用追加语义。
	// 重复提供时必须保持一致。
	ID string `json:"id,omitempty"`

	// Name 是完整工具名称，不采用追加语义。
	// 重复提供时必须保持一致。
	Name string `json:"name,omitempty"`

	// ArgumentsDelta 是追加到原始参数字符串的内容。
	// 它不要求独立构成合法 JSON。
	ArgumentsDelta string `json:"argumentsDelta,omitempty"`
}

// Validate 检查 e 的类型及互斥载荷。
// e 为 nil 时返回错误。本方法不修改 e。
// 跨事件顺序和 final 一致性由 GuardStream 检查。
func (e *StreamEvent) Validate() error {
	if e == nil {
		return errors.New("stream event is nil")
	}

	switch e.Kind {
	case StreamDelta:
		if e.Delta == nil || e.Final != nil {
			return errors.New("delta event has incompatible fields")
		}
		return e.Delta.Validate()

	case StreamFinal:
		if e.Final == nil || e.Delta != nil {
			return errors.New("final event has incompatible fields")
		}
		return e.Final.Validate()

	default:
		return fmt.Errorf("kind: unknown stream event kind %q", e.Kind)
	}
}

// Clone 返回 e 的深拷贝。
// e 为 nil 时返回 nil。Clone 不校验或修复事件。
func (e *StreamEvent) Clone() *StreamEvent {
	if e == nil {
		return nil
	}

	out := *e
	out.Delta = e.Delta.Clone()
	out.Final = e.Final.Clone()

	return &out
}

// Validate 检查 d 的位置、类型及增量字段。
// d 为 nil 时返回错误。本方法不修改 d。
//
// Validate 不要求 ArgumentsDelta 是完整 JSON，
// 也不检查该位置是否已经在流中出现。
func (d *PartDelta) Validate() error {
	if d == nil {
		return errors.New("part delta is nil")
	}
	if d.PartIndex < 0 {
		return errors.New("partIndex: must not be negative")
	}

	switch d.Kind {
	case PartText, PartReasoning, PartRefusal:
		if d.Text == "" {
			return errors.New("text: delta must not be empty")
		}
		if d.ToolCall != nil {
			return errors.New(
				"toolCall: incompatible with text, reasoning " +
					"or refusal delta",
			)
		}

	case PartToolCall:
		if d.Text != "" {
			return errors.New("text: incompatible with tool call delta")
		}
		if err := d.ToolCall.Validate(); err != nil {
			return fmt.Errorf("toolCall: %w", err)
		}

	default:
		return fmt.Errorf("kind: %q cannot be streamed", d.Kind)
	}

	return nil
}

// Clone 返回 d 的独立副本，并复制 ToolCall 载荷。
// d 为 nil 时返回 nil。Clone 不校验增量。
func (d *PartDelta) Clone() *PartDelta {
	if d == nil {
		return nil
	}

	out := *d

	if d.ToolCall != nil {
		call := *d.ToolCall
		out.ToolCall = &call
	}

	return &out
}

// Validate 检查 d 是否包含有效增量字段。
// d 为 nil 时返回错误。本方法不修改 d，也不解析参数字符串。
func (d *ToolCallDelta) Validate() error {
	if d == nil {
		return errors.New("tool call delta is nil")
	}
	if d.ID == "" && d.Name == "" && d.ArgumentsDelta == "" {
		return errors.New("tool call delta must contain at least one field")
	}
	if d.ID != "" && strings.TrimSpace(d.ID) == "" {
		return errors.New("id: must not be blank when set")
	}
	if d.Name != "" && strings.TrimSpace(d.Name) == "" {
		return errors.New("name: must not be blank when set")
	}

	return nil
}

// NewDelta 返回校验通过的 delta 事件及其独立载荷副本。
// delta 不合法时返回 nil 和错误。
func NewDelta(delta PartDelta) (*StreamEvent, error) {
	event := &StreamEvent{
		Kind:  StreamDelta,
		Delta: delta.Clone(),
	}

	if err := event.Validate(); err != nil {
		return nil, err
	}

	return event, nil
}

// NewFinal 返回校验通过的 final 事件及响应的深拷贝。
// response 为 nil 或不合法时返回 nil 和错误。
//
// NewFinal 不检查请求历史冲突；适配器必须另行调用
// Response.ValidateFor。原始流中的该事件仍需经过 GuardStream。
func NewFinal(response *Response) (*StreamEvent, error) {
	event := &StreamEvent{
		Kind:  StreamFinal,
		Final: response.Clone(),
	}

	if err := event.Validate(); err != nil {
		return nil, err
	}

	return event, nil
}
