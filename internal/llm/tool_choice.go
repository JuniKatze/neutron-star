package llm

import (
	"errors"
	"fmt"
	"strings"
)

// ToolChoiceMode 表示模型选择工具的策略。
type ToolChoiceMode string

const (
	// ToolChoiceAuto 允许模型自行决定是否调用工具。
	ToolChoiceAuto ToolChoiceMode = "auto"

	// ToolChoiceNone 禁止模型调用工具。
	ToolChoiceNone ToolChoiceMode = "none"

	// ToolChoiceRequired 要求模型调用工具。
	ToolChoiceRequired ToolChoiceMode = "required"

	// ToolChoiceNamed 要求模型选择指定工具。
	ToolChoiceNamed ToolChoiceMode = "named"
)

// ToolChoice 控制模型是否以及如何选择工具。
// nil 表示未指定。非 nil ToolChoice 必须提供合法 Mode；
// ToolChoice 的零值无效。
type ToolChoice struct {
	// Mode 指定工具选择策略。
	Mode ToolChoiceMode `json:"mode"`

	// Name 是 named 模式下指定的工具名称。
	// 其他模式必须保持为空。
	Name string `json:"name,omitempty"`

	// Parallel 控制是否允许模型并行提出工具调用。
	// nil 表示未指定。它不决定应用如何调度工具执行，
	// 且在 Mode 为 none 时没有意义。
	Parallel *bool `json:"parallel,omitempty"`
}

// Validate 检查 c 的模式及工具名称组合。
// c 为 nil 时返回 nil。本方法不修改 c。
//
// Validate 检查 Mode 与 Parallel 的相容性（none 不允许 Parallel=true），
// 但不检查 Name 是否存在于请求的工具声明中，也不检查目标供应商
// 是否支持该模式或 Parallel。
func (c *ToolChoice) Validate() error {
	if c == nil {
		return nil
	}

	switch c.Mode {
	case ToolChoiceNone:
		if c.Name != "" {
			return errors.New("name: only valid in named mode")
		}
		if c.Parallel != nil && *c.Parallel {
			return errors.New(
				"parallel: not allowed when tool choice is none",
			)
		}

	case ToolChoiceAuto, ToolChoiceRequired:
		if c.Name != "" {
			return errors.New("name: only valid in named mode")
		}

	case ToolChoiceNamed:
		if strings.TrimSpace(c.Name) == "" {
			return errors.New("name: required in named mode")
		}

		if strings.TrimSpace(c.Name) != c.Name {
			return errors.New(
				"name: must not have leading or trailing whitespace",
			)
		}

	default:
		return fmt.Errorf("mode: unknown tool choice mode %q", c.Mode)
	}

	return nil
}

// Clone 返回 c 的独立副本，并复制 Parallel 指针。
// c 为 nil 时返回 nil。Clone 不校验配置。
func (c *ToolChoice) Clone() *ToolChoice {
	if c == nil {
		return nil
	}

	out := *c
	out.Parallel = clonePtr(c.Parallel)
	return &out
}
