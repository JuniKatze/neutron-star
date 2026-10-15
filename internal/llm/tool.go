package llm

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ToolSchema 声明一个由应用执行的 JSON 对象参数函数工具。
// ToolSchema 的零值无效。
// 具体名称格式及 Schema 支持范围由适配器检查。
type ToolSchema struct {
	// Name 是工具名称。
	Name string `json:"name"`

	// Description 是可选的工具说明。
	Description string `json:"description,omitempty"`

	// Parameters 保存参数 Schema 的原始 JSON 对象文档。
	// 必需的：没有参数的工具也要给一个空的对象 Schema。
	Parameters json.RawMessage `json:"parameters"`

	// Strict 控制是否请求模型严格遵守 Parameters。
	// nil 表示未指定。目标供应商是否支持该字段，由适配器检查。
	Strict *bool `json:"strict,omitempty"`
}

// Validate 检查 t 的名称及 Parameters 的 JSON 结构。
// t 为 nil 时返回错误。本方法不修改 t。
//
// Validate 不编译 Schema，不解析外部引用，不执行工具，
// 也不检查名称是否与其他工具声明重复。
func (t *ToolSchema) Validate() error {
	if t == nil {
		return errors.New("tool schema is nil")
	}
	if strings.TrimSpace(t.Name) == "" {
		return errors.New("name: required")
	}
	if strings.TrimSpace(t.Name) != t.Name {
		return errors.New(
			"name: must not have leading or trailing whitespace",
		)
	}
	if len(t.Parameters) == 0 {
		return errors.New("parameters: required")
	}
	if err := validateObject(t.Parameters); err != nil {
		return fmt.Errorf("parameters: %w", err)
	}

	return nil
}

// Clone 返回 t 的副本，并独立复制 Parameters 与 Strict
// Clone 保留原文及 nil 状态，不校验工具声明。
func (t ToolSchema) Clone() ToolSchema {
	out := t

	out.Strict = clonePtr(t.Strict)

	if t.Parameters != nil {
		out.Parameters = make(json.RawMessage, len(t.Parameters))
		copy(out.Parameters, t.Parameters)
	}

	return out
}
