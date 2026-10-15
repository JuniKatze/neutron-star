package llm

import (
	"errors"
	"fmt"
	"strings"
)

// Request 描述一次供应商无关的模型生成请求。
// Request 的零值无效。
//
// Messages 是用于本次生成的上下文，不是完整运行时间线。
// 所有需要结果的历史调用必须已经得到唯一的终态结果。
//
// 调用方法期间，调用方必须保证没有其他 goroutine
// 并发修改 Request 或其引用的数据。
type Request struct {
	// Model 是目标模型标识。
	Model string `json:"model"`

	// Messages 保存有序模型上下文，至少包含一条消息。
	// system 消息只能构成连续前缀。
	Messages []Message `json:"messages"`

	// Tools 声明本次生成可以使用的函数工具。
	// 历史调用不要求仍存在于当前 Tools。
	Tools []ToolSchema `json:"tools,omitempty"`

	// Config 控制本次生成的行为。
	Config *Config `json:"config,omitempty"`
}

// Validate 检查 r 的结构、配置、工具声明及消息历史。
// r 为 nil 时返回错误。本方法不修改 r，也不执行工具或获取资源。
//
// 调用 ID 在请求内必须唯一。工具结果必须引用此前的调用，
// 同一调用的全部结果内容必须位于一条结果消息中。
// 在所有待结果调用闭合前，不允许出现下一条普通消息。
//
// Validate 不强制第一条消息的角色，也不强制最后一条消息的角色：
// 各协议对首条消息的要求不同（有的要求 user 开头），由适配器检查。
func (r *Request) Validate() error {
	if r == nil {
		return errors.New("request is nil")
	}
	if strings.TrimSpace(r.Model) == "" {
		return errors.New("model: required")
	}
	if len(r.Messages) == 0 {
		return errors.New("messages: required")
	}

	if err := r.Config.Validate(); err != nil {
		return fmt.Errorf("config: %w", err)
	}

	toolNames := make(map[string]struct{}, len(r.Tools))

	for index := range r.Tools {
		tool := &r.Tools[index]

		if err := tool.Validate(); err != nil {
			return fmt.Errorf("tools[%d]: %w", index, err)
		}
		if _, exists := toolNames[tool.Name]; exists {
			return fmt.Errorf(
				"tools[%d].name: duplicate %q",
				index,
				tool.Name,
			)
		}

		toolNames[tool.Name] = struct{}{}
	}

	if r.Config != nil && r.Config.ToolChoice != nil {
		choice := r.Config.ToolChoice

		switch choice.Mode {
		case ToolChoiceRequired:
			if len(r.Tools) == 0 {
				return errors.New(
					"config.toolChoice: required mode needs at least one tool",
				)
			}

		case ToolChoiceNamed:
			if _, exists := toolNames[choice.Name]; !exists {
				return fmt.Errorf(
					"config.toolChoice.name: unknown tool %q",
					choice.Name,
				)
			}
		}
	}

	// 值为 true 表示对应调用已经得到完整结果。
	calls := make(map[string]bool)
	callOrder := make([]string, 0)
	openCalls := 0
	seenNonSystem := false

	for messageIndex := range r.Messages {
		message := &r.Messages[messageIndex]

		if err := message.Validate(); err != nil {
			return fmt.Errorf(
				"messages[%d]: %w",
				messageIndex,
				err,
			)
		}

		if message.Role == RoleSystem {
			if seenNonSystem {
				return fmt.Errorf(
					"messages[%d].role: system messages "+
						"must form a leading prefix",
					messageIndex,
				)
			}
		} else {
			seenNonSystem = true
		}

		if message.IsToolResultMessage() {
			// 同一消息内多个 Part 可属于同一调用，
			// 只在第一次遇到该调用时关闭它。
			seenResults := make(map[string]struct{})

			for partIndex := range message.Parts {
				part := &message.Parts[partIndex]
				callID := part.ResultOf.CallID

				if _, exists := seenResults[callID]; exists {
					continue
				}

				closed, exists := calls[callID]
				if !exists {
					return fmt.Errorf(
						"messages[%d].parts[%d].resultOf.callId: "+
							"unknown or not earlier call %q",
						messageIndex,
						partIndex,
						callID,
					)
				}
				if closed {
					return fmt.Errorf(
						"messages[%d].parts[%d].resultOf.callId: "+
							"call %q already has a result",
						messageIndex,
						partIndex,
						callID,
					)
				}

				seenResults[callID] = struct{}{}
				calls[callID] = true
				openCalls--
			}

			continue
		}

		if openCalls != 0 {
			return fmt.Errorf(
				"messages[%d]: tool call %q must have a result "+
					"before an ordinary message",
				messageIndex,
				firstOpenRequestCall(calls, callOrder),
			)
		}

		for partIndex := range message.Parts {
			part := &message.Parts[partIndex]

			if part.Kind != PartToolCall {
				continue
			}

			callID := part.ToolCall.ID
			if _, exists := calls[callID]; exists {
				return fmt.Errorf(
					"messages[%d].parts[%d].toolCall.id: duplicate %q",
					messageIndex,
					partIndex,
					callID,
				)
			}

			calls[callID] = false
			callOrder = append(callOrder, callID)
			openCalls++
		}
	}

	if openCalls != 0 {
		return fmt.Errorf(
			"messages: tool call %q has no terminal result",
			firstOpenRequestCall(calls, callOrder),
		)
	}

	return nil
}

// Clone 返回 r 的深拷贝。
// 返回值不与 r 共享消息内容、工具参数文档或配置中的可变内存。
// r 为 nil 时返回 nil。
//
// Clone 保留 nil 与空集合的区别，不校验或修复请求。
// 它不提供对并发修改对象的原子快照。
func (r *Request) Clone() *Request {
	if r == nil {
		return nil
	}

	out := *r
	out.Config = r.Config.Clone()

	if r.Messages != nil {
		out.Messages = make([]Message, len(r.Messages))

		for index := range r.Messages {
			out.Messages[index] = *r.Messages[index].Clone()
		}
	}

	if r.Tools != nil {
		out.Tools = make([]ToolSchema, len(r.Tools))

		for index := range r.Tools {
			out.Tools[index] = r.Tools[index].Clone()
		}
	}

	return &out
}

// firstOpenRequestCall 按调用出现顺序返回首个尚未闭合的调用。
// 如果没有开放调用，则返回空字符串。
func firstOpenRequestCall(
	calls map[string]bool,
	order []string,
) string {
	for _, callID := range order {
		if !calls[callID] {
			return callID
		}
	}

	return ""
}
