package llm

import (
	"errors"
	"fmt"
)

type Role string

const (
	RoleSystem Role = "system"

	// RoleUser 表示用户侧输入。
	RoleUser Role = "user"

	// RoleAssistant 表示 agent 侧内容
	RoleAssistant Role = "assistant"
)

// Message 保存一条消息的有序内容块。
//
// system 与 user 消息至少要有一个 Part；assistant 可以没有 Part。
// 带 ResultOf 的 Part 是工具结果，只能出现在 assistant 消息里，
// 并且一条消息不能同时包含结果与普通内容。
//
// Replay 保存消息级私有状态，它不代替各 Part 自身需要的 Replay。
//
// 调用方法期间，调用方必须保证没有其他 goroutine
// 并发修改 Message 或其引用的数据。
type Message struct {
	// Role 指定消息所属的对话侧。
	Role Role `json:"role"`

	// Parts 保存有序内容块，每个元素都是值，不存在 nil 元素。
	// 同一工具结果消息可以包含多个调用的结果。
	Parts []Part `json:"parts"`

	// Replay 保存消息级私有状态。
	Replay *Replay `json:"replay,omitempty"`
}

// Validate 检查 m 的角色、内容、局部关联及回放绑定。
// m 为 nil 时返回错误。本方法不修改 m，也不获取资源。
//
// Validate 检查消息内调用 ID 的唯一性，并要求 PartEmpty 独占所属结果组。
// 它不检查结果引用的调用是否存在，也不检查消息在请求历史中的顺序——
// 这两项由 Request.Validate 负责。
func (m *Message) Validate() error {
	if err := m.validateContent(); err != nil {
		return err
	}

	for index := range m.Parts {
		part := &m.Parts[index]

		if err := part.Validate(); err != nil {
			return fmt.Errorf("parts[%d]: %w", index, err)
		}
	}

	if m.Replay != nil {
		if err := m.Replay.Validate(); err != nil {
			return fmt.Errorf("replay: %w", err)
		}

		binding, err := m.Binding()
		if err != nil {
			return fmt.Errorf("replay binding: %w", err)
		}

		if m.Replay.Binding != binding {
			return errors.New(
				"replay.binding: does not match message content",
			)
		}
	}

	return nil
}

// validateContent 检查角色、公开内容与消息内关联，不检查任何 Replay。
// Binding 使用该方法，避免回放校验与摘要计算相互递归。
//
// 这里的规则有两条是给 Request.Validate 复用的前提：一条消息不能同时
// 包含工具结果与普通内容（IsToolFeedback 依赖它），同一调用的结果组里
// PartEmpty 必须独占。
func (m *Message) validateContent() error {
	if m == nil {
		return errors.New("message is nil")
	}

	switch m.Role {
	case RoleSystem, RoleUser, RoleAssistant:
	default:
		return fmt.Errorf("role: unknown role %q", m.Role)
	}

	if m.Role != RoleAssistant && len(m.Parts) == 0 {
		return errors.New("parts: required for system and user messages")
	}

	hasResult := false
	hasOrdinary := false

	callIDs := make(map[string]struct{})

	// 值表示该结果组是否已经包含 PartEmpty。
	resultGroups := make(map[string]bool)

	for index := range m.Parts {
		part := &m.Parts[index]

		if err := part.validateContent(); err != nil {
			return fmt.Errorf("parts[%d]: %w", index, err)
		}

		switch m.Role {
		case RoleSystem:
			if part.Kind != PartText {
				return fmt.Errorf(
					"parts[%d].kind: system messages only accept text",
					index,
				)
			}

		case RoleUser:
			switch part.Kind {
			case PartText, PartJSON,
				PartImage, PartAudio, PartVideo,
				PartDocument, PartFile:
			default:
				return fmt.Errorf(
					"parts[%d].kind: %q is not valid user content",
					index,
					part.Kind,
				)
			}
		}

		if part.ResultOf == nil {
			hasOrdinary = true
		} else {
			if m.Role != RoleAssistant {
				return fmt.Errorf(
					"parts[%d].resultOf: only assistant messages "+
						"may contain tool feedback",
					index,
				)
			}

			hasResult = true
			callID := part.ResultOf.CallID

			containsEmpty, exists := resultGroups[callID]
			if exists &&
				(containsEmpty || part.Kind == PartEmpty) {
				return fmt.Errorf(
					"parts[%d]: empty result for call %q "+
						"must be the only part in its group",
					index,
					callID,
				)
			}

			if !exists {
				resultGroups[callID] = part.Kind == PartEmpty
			}
		}

		if part.Kind == PartToolCall {
			callID := part.ToolCall.ID
			if _, exists := callIDs[callID]; exists {
				return fmt.Errorf(
					"parts[%d].toolCall.id: duplicate %q",
					index,
					callID,
				)
			}
			callIDs[callID] = struct{}{}
		}
	}

	if hasResult && hasOrdinary {
		return errors.New(
			"parts: tool feedback must not be mixed with ordinary content",
		)
	}

	return nil
}

// Binding 返回 m 的公开内容摘要，不包含任何 Replay。
// 摘要输入是 Role 与有序的 Part 摘要输入，域为 llm.message。
//
// 内嵌的是每个 Part 的摘要输入，而不是 Part 的序列化结果：这样 Part 上的
// Replay 不会污染消息摘要。nil Parts 与空 Parts 得到相同的摘要。
//
// Binding 只检查公开内容结构，因此计算成功不代表 Validate 成功。
// 本方法不修改 m。
func (m *Message) Binding() (string, error) {
	if err := m.validateContent(); err != nil {
		return "", err
	}

	parts := make([]any, len(m.Parts))
	for index := range m.Parts {
		parts[index] = partBindingValue(m.Parts[index])
	}

	value := map[string]any{
		"role":  string(m.Role),
		"parts": parts,
	}

	return digest("llm.message", value)
}

// Clone 返回 m 的深拷贝。
// 返回值不与 m 共享 Parts 的底层数组、Part 对象或回放数据。
// m 为 nil 时返回 nil。
//
// Clone 保留 nil 与空切片的区别，不校验或修复消息。
func (m *Message) Clone() *Message {
	if m == nil {
		return nil
	}

	out := *m

	if m.Parts != nil {
		out.Parts = make([]Part, len(m.Parts))

		for index := range m.Parts {
			out.Parts[index] = m.Parts[index].Clone()
		}
	}

	out.Replay = m.Replay.Clone()
	return &out
}

// IsToolResultMessage 报告 m 是否为工具结果消息。
//
// 判据只看第一个 Part：validateContent 保证一条消息不会同时包含结果与
// 普通内容，因此第一个 Part 带 ResultOf 就意味着整条消息都是结果。
//
// 本方法导出是因为适配器要按结果消息展开 wire 消息（例如 Chat Completions
// 把它展开成多条 role:"tool"），而"什么算结果消息"的判据必须只有一份。
// m 为 nil 时返回 false。
func (m *Message) IsToolResultMessage() bool {
	if m == nil {
		return false
	}

	return len(m.Parts) != 0 && m.Parts[0].ResultOf != nil
}
